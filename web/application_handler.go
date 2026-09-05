package web

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"controlplane/internal/config"
	configmodel "controlplane/internal/model/config"
	"controlplane/web/template/pages"
)

const applicationsPageSize = 20

// ApplicationStore is what application CRUD handlers need. Satisfied by *config.ConfigService.
type ApplicationStore interface {
	ListAllApplications(ctx context.Context, q string, allowedIDs []uuid.UUID) ([]configmodel.Application, error)
	GetApplicationByID(ctx context.Context, id uuid.UUID) (*configmodel.Application, error)
	CreateApplication(ctx context.Context, name string) (*configmodel.Application, error)
	UpdateApplication(ctx context.Context, id uuid.UUID, name string) (*configmodel.Application, error)
	DeleteApplication(ctx context.Context, id uuid.UUID) (*configmodel.Application, error)
}

type ApplicationHandler struct {
	apps     ApplicationStore
	activity ActivityRecorder
}

func NewApplicationHandler(apps ApplicationStore, activity ActivityRecorder) *ApplicationHandler {
	return &ApplicationHandler{apps: apps, activity: activity}
}

func (h *ApplicationHandler) List(c echo.Context) error {
	q := strings.TrimSpace(c.QueryParam("q"))
	allowedIDs, _ := AllowedApplicationIDs(c)

	apps, err := h.apps.ListAllApplications(c.Request().Context(), q, allowedIDs)
	if err != nil {
		return err
	}

	page := Paginate(apps, applicationsPageSize, PageParam(c))

	extra := url.Values{}
	if q != "" {
		extra.Set("q", q)
	}

	return pages.ApplicationsList(flashes(c), navUser(c), pages.ApplicationsListData{
		Apps: page.Items, CurrentQ: q, Page: page.Number, NumPages: page.NumPages,
		HasPrev: page.HasPrevious, HasNext: page.HasNext,
		PrevNum: page.PreviousNumber, NextNum: page.NextNumber,
		Window: page.PageRange(), ExtraQuery: extra.Encode(),
	}).Render(c.Request().Context(), c.Response())
}

func (h *ApplicationHandler) Create(c echo.Context) error {
	if c.Request().Method == http.MethodGet {
		return pages.ApplicationForm(flashes(c), navUser(c), pages.ApplicationFormData{
			CSRFToken: csrfToken(c), Action: "/applications/create/",
		}).Render(c.Request().Context(), c.Response())
	}

	name := strings.TrimSpace(c.FormValue("name"))
	if name == "" {
		return pages.ApplicationForm(flashes(c), navUser(c), pages.ApplicationFormData{
			CSRFToken: csrfToken(c), Action: "/applications/create/", Error: "Name is required.",
		}).Render(c.Request().Context(), c.Response())
	}

	app, err := h.apps.CreateApplication(c.Request().Context(), name)
	if err != nil {
		if errors.Is(err, config.ErrAlreadyExists) {
			return pages.ApplicationForm(flashes(c), navUser(c), pages.ApplicationFormData{
				CSRFToken: csrfToken(c), Action: "/applications/create/", Name: name,
				Error: "An application with this name already exists.",
			}).Render(c.Request().Context(), c.Response())
		}
		return err
	}

	h.activity.LogCreate(requestContext(c), "application", app.ID.String(), app.Name, nil)
	AddFlash(c, "success", "Application created.")
	return c.Redirect(http.StatusFound, "/applications/")
}

func (h *ApplicationHandler) Edit(c echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound)
	}
	app, err := h.apps.GetApplicationByID(c.Request().Context(), id)
	if err != nil {
		return err
	}
	if app == nil {
		return echo.NewHTTPError(http.StatusNotFound)
	}

	if c.Request().Method == http.MethodGet {
		return pages.ApplicationForm(flashes(c), navUser(c), pages.ApplicationFormData{
			CSRFToken: csrfToken(c), Action: "/applications/" + app.ID.String() + "/edit/",
			Name: app.Name, IsEdit: true,
		}).Render(c.Request().Context(), c.Response())
	}

	name := strings.TrimSpace(c.FormValue("name"))
	if name == "" {
		return pages.ApplicationForm(flashes(c), navUser(c), pages.ApplicationFormData{
			CSRFToken: csrfToken(c), Action: "/applications/" + app.ID.String() + "/edit/",
			IsEdit: true, Error: "Name is required.",
		}).Render(c.Request().Context(), c.Response())
	}

	updated, err := h.apps.UpdateApplication(c.Request().Context(), id, name)
	if err != nil {
		return err
	}
	h.activity.LogUpdate(requestContext(c), "application", updated.ID.String(), updated.Name, nil)
	AddFlash(c, "success", "Application updated.")
	return c.Redirect(http.StatusFound, "/applications/")
}

func (h *ApplicationHandler) Delete(c echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound)
	}
	app, err := h.apps.GetApplicationByID(c.Request().Context(), id)
	if err != nil {
		return err
	}
	if app == nil {
		return echo.NewHTTPError(http.StatusNotFound)
	}

	if c.Request().Method == http.MethodGet {
		return pages.ConfirmDelete(flashes(c), navUser(c), "applications", pages.ConfirmDeleteData{
			CSRFToken:  csrfToken(c),
			Title:      "Delete Application",
			Message:    "Are you sure you want to delete \"" + app.Name + "\"? This will delete all of its environments, configs, secrets and feature flags.",
			Action:     "/applications/" + app.ID.String() + "/delete/",
			CancelHref: "/applications/",
		}).Render(c.Request().Context(), c.Response())
	}

	if _, err := h.apps.DeleteApplication(c.Request().Context(), id); err != nil {
		return err
	}
	h.activity.LogDelete(requestContext(c), "application", app.ID.String(), app.Name, nil)
	AddFlash(c, "success", "Application deleted.")
	return c.Redirect(http.StatusFound, "/applications/")
}

// listApplications is a small shared helper for handlers that just need the
// allowed application list for a <select> (environment/config/flag forms).
func listApplications(ctx context.Context, apps ApplicationStore, allowedIDs []uuid.UUID) ([]configmodel.Application, error) {
	return apps.ListAllApplications(ctx, "", allowedIDs)
}

var _ ApplicationStore = (*config.ConfigService)(nil)
