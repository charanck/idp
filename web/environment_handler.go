package web

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"controlplane/internal/config"
	configmodel "controlplane/internal/model/config"
	"controlplane/web/template/pages"
)

const environmentsPageSize = 20

// EnvironmentStore is what environment CRUD handlers need. Satisfied by *config.ConfigService.
type EnvironmentStore interface {
	ListAllEnvironments(ctx context.Context, filter configmodel.ListEnvironmentsFilter) ([]configmodel.Environment, error)
	ListEnvironmentsByApplicationID(ctx context.Context, applicationID uuid.UUID) ([]configmodel.Environment, error)
	GetEnvironmentByID(ctx context.Context, id uuid.UUID) (*configmodel.Environment, error)
	GetEnvironmentWithApplicationByID(ctx context.Context, id uuid.UUID) (*configmodel.Environment, error)
	CreateEnvironment(ctx context.Context, applicationID uuid.UUID, name string) (*configmodel.Environment, error)
	UpdateEnvironment(ctx context.Context, id, applicationID uuid.UUID, name string) (*configmodel.Environment, error)
	DeleteEnvironment(ctx context.Context, id uuid.UUID) (*configmodel.Environment, error)
}

type EnvironmentHandler struct {
	envs     EnvironmentStore
	apps     ApplicationStore
	activity ActivityRecorder
}

func NewEnvironmentHandler(envs EnvironmentStore, apps ApplicationStore, activity ActivityRecorder) *EnvironmentHandler {
	return &EnvironmentHandler{envs: envs, apps: apps, activity: activity}
}

func (h *EnvironmentHandler) List(c echo.Context) error {
	appIDFilter := c.QueryParam("application_id")
	q := strings.TrimSpace(c.QueryParam("q"))

	allowedIDs, _ := AllowedApplicationIDs(c)
	filter := configmodel.ListEnvironmentsFilter{Query: q, ApplicationIDs: allowedIDs}
	if appIDFilter != "" {
		if id, err := uuid.Parse(appIDFilter); err == nil {
			filter.ApplicationID = &id
		}
	}

	envs, err := h.envs.ListAllEnvironments(c.Request().Context(), filter)
	if err != nil {
		return err
	}

	groupsByApp := map[uuid.UUID]*pages.EnvironmentGroup{}
	var order []uuid.UUID
	for _, env := range envs {
		g, ok := groupsByApp[env.ApplicationID]
		if !ok {
			g = &pages.EnvironmentGroup{Application: env.Application}
			groupsByApp[env.ApplicationID] = g
			order = append(order, env.ApplicationID)
		}
		g.Environments = append(g.Environments, env)
	}
	sort.Slice(order, func(i, j int) bool {
		return groupsByApp[order[i]].Application.Name < groupsByApp[order[j]].Application.Name
	})

	groups := make([]pages.EnvironmentGroup, 0, len(order))
	for _, id := range order {
		groups = append(groups, *groupsByApp[id])
	}

	apps, err := listApplications(c.Request().Context(), h.apps, allowedIDs)
	if err != nil {
		return err
	}

	page := Paginate(groups, environmentsPageSize, PageParam(c))

	extra := url.Values{}
	if appIDFilter != "" {
		extra.Set("application_id", appIDFilter)
	}
	if q != "" {
		extra.Set("q", q)
	}

	return pages.EnvironmentsList(flashes(c), navUser(c), pages.EnvironmentsListData{
		Applications: apps, CurrentAppID: appIDFilter, CurrentQ: q, ExtraQuery: extra.Encode(),
		Groups: page.Items, Page: page.Number, NumPages: page.NumPages,
		HasPrev: page.HasPrevious, HasNext: page.HasNext,
		PrevNum: page.PreviousNumber, NextNum: page.NextNumber,
		Window: page.PageRange(),
	}).Render(c.Request().Context(), c.Response())
}

func (h *EnvironmentHandler) Create(c echo.Context) error {
	allowedIDs, _ := AllowedApplicationIDs(c)
	apps, err := listApplications(c.Request().Context(), h.apps, allowedIDs)
	if err != nil {
		return err
	}

	if c.Request().Method == http.MethodGet {
		return pages.EnvironmentForm(flashes(c), navUser(c), pages.EnvironmentFormData{
			CSRFToken: csrfToken(c), Applications: apps, Action: "/environments/create/",
		}).Render(c.Request().Context(), c.Response())
	}

	applicationID := c.FormValue("application_id")
	name := strings.TrimSpace(c.FormValue("name"))
	appID, parseErr := uuid.Parse(applicationID)

	if parseErr != nil || name == "" {
		return pages.EnvironmentForm(flashes(c), navUser(c), pages.EnvironmentFormData{
			CSRFToken: csrfToken(c), Applications: apps, Action: "/environments/create/",
			ApplicationID: applicationID, Name: name, Error: "Application and name are required.",
		}).Render(c.Request().Context(), c.Response())
	}
	if !ApplicationAllowed(c, appID) {
		return echo.NewHTTPError(http.StatusNotFound)
	}

	env, err := h.envs.CreateEnvironment(c.Request().Context(), appID, name)
	if err != nil {
		if errors.Is(err, config.ErrAlreadyExists) {
			return pages.EnvironmentForm(flashes(c), navUser(c), pages.EnvironmentFormData{
				CSRFToken: csrfToken(c), Applications: apps, Action: "/environments/create/",
				ApplicationID: applicationID, Name: name, Error: "This application already has an environment with this name.",
			}).Render(c.Request().Context(), c.Response())
		}
		return err
	}

	h.activity.LogCreate(requestContext(c), "environment", env.ID.String(), env.Name, nil)
	AddFlash(c, "success", "Environment created.")
	return c.Redirect(http.StatusFound, "/environments/")
}

func (h *EnvironmentHandler) Edit(c echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound)
	}
	env, err := h.envs.GetEnvironmentByID(c.Request().Context(), id)
	if err != nil {
		return err
	}
	if env == nil || !ApplicationAllowed(c, env.ApplicationID) {
		return echo.NewHTTPError(http.StatusNotFound)
	}
	allowedIDs, _ := AllowedApplicationIDs(c)
	apps, err := listApplications(c.Request().Context(), h.apps, allowedIDs)
	if err != nil {
		return err
	}

	action := "/environments/" + env.ID.String() + "/edit/"
	if c.Request().Method == http.MethodGet {
		return pages.EnvironmentForm(flashes(c), navUser(c), pages.EnvironmentFormData{
			CSRFToken: csrfToken(c), Applications: apps, Action: action, IsEdit: true,
			ApplicationID: env.ApplicationID.String(), Name: env.Name,
		}).Render(c.Request().Context(), c.Response())
	}

	applicationID := c.FormValue("application_id")
	name := strings.TrimSpace(c.FormValue("name"))
	appID, parseErr := uuid.Parse(applicationID)
	if parseErr != nil || name == "" {
		return pages.EnvironmentForm(flashes(c), navUser(c), pages.EnvironmentFormData{
			CSRFToken: csrfToken(c), Applications: apps, Action: action, IsEdit: true,
			ApplicationID: applicationID, Name: name, Error: "Application and name are required.",
		}).Render(c.Request().Context(), c.Response())
	}

	if !ApplicationAllowed(c, appID) {
		return echo.NewHTTPError(http.StatusNotFound)
	}

	updated, err := h.envs.UpdateEnvironment(c.Request().Context(), id, appID, name)
	if err != nil {
		return err
	}
	h.activity.LogUpdate(requestContext(c), "environment", updated.ID.String(), updated.Name, nil)
	AddFlash(c, "success", "Environment updated.")
	return c.Redirect(http.StatusFound, "/environments/")
}

func (h *EnvironmentHandler) Delete(c echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound)
	}
	env, err := h.envs.GetEnvironmentWithApplicationByID(c.Request().Context(), id)
	if err != nil {
		return err
	}
	if env == nil || !ApplicationAllowed(c, env.ApplicationID) {
		return echo.NewHTTPError(http.StatusNotFound)
	}

	if c.Request().Method == http.MethodGet {
		return pages.ConfirmDelete(flashes(c), navUser(c), "environments", pages.ConfirmDeleteData{
			CSRFToken:  csrfToken(c),
			Title:      "Delete Environment",
			Message:    "Are you sure you want to delete \"" + env.Application.Name + "/" + env.Name + "\"? This will delete all of its configs, secrets and feature flags.",
			Action:     "/environments/" + env.ID.String() + "/delete/",
			CancelHref: "/environments/",
		}).Render(c.Request().Context(), c.Response())
	}

	if _, err := h.envs.DeleteEnvironment(c.Request().Context(), id); err != nil {
		return err
	}
	h.activity.LogDelete(requestContext(c), "environment", env.ID.String(), env.Name, nil)
	AddFlash(c, "success", "Environment deleted.")
	return c.Redirect(http.StatusFound, "/environments/")
}

var _ EnvironmentStore = (*config.ConfigService)(nil)
