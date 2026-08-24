package webui

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"controlplane/internal/config"
	"controlplane/views/pages"
)

const applicationsPageSize = 20

func applicationsListHandler(deps *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
		q := strings.TrimSpace(c.QueryParam("q"))

		apps, err := deps.ConfigService.ListAllApplications(c.Request().Context(), q)
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
}

func applicationCreateHandler(deps *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
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

		app, err := deps.ConfigService.CreateApplication(c.Request().Context(), name)
		if err != nil {
			if errors.Is(err, config.ErrAlreadyExists) {
				return pages.ApplicationForm(flashes(c), navUser(c), pages.ApplicationFormData{
					CSRFToken: csrfToken(c), Action: "/applications/create/", Name: name,
					Error: "An application with this name already exists.",
				}).Render(c.Request().Context(), c.Response())
			}
			return err
		}

		deps.Activity.LogCreate(requestContext(c), "application", app.ID.String(), app.Name, nil)
		AddFlash(c, "success", "Application created.")
		return c.Redirect(http.StatusFound, "/applications/")
	}
}

func applicationEditHandler(deps *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			return echo.NewHTTPError(http.StatusNotFound)
		}
		app, err := deps.ConfigService.GetApplicationByID(c.Request().Context(), id)
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

		updated, err := deps.ConfigService.UpdateApplication(c.Request().Context(), id, name)
		if err != nil {
			return err
		}
		deps.Activity.LogUpdate(requestContext(c), "application", updated.ID.String(), updated.Name, nil)
		AddFlash(c, "success", "Application updated.")
		return c.Redirect(http.StatusFound, "/applications/")
	}
}

func applicationDeleteHandler(deps *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			return echo.NewHTTPError(http.StatusNotFound)
		}
		app, err := deps.ConfigService.GetApplicationByID(c.Request().Context(), id)
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

		if _, err := deps.ConfigService.DeleteApplication(c.Request().Context(), id); err != nil {
			return err
		}
		deps.Activity.LogDelete(requestContext(c), "application", app.ID.String(), app.Name, nil)
		AddFlash(c, "success", "Application deleted.")
		return c.Redirect(http.StatusFound, "/applications/")
	}
}
