package webui

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
	"controlplane/views/pages"
)

const environmentsPageSize = 20

func environmentsListHandler(deps *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
		appIDFilter := c.QueryParam("application_id")
		q := strings.TrimSpace(c.QueryParam("q"))

		filter := config.ListEnvironmentsFilter{Query: q}
		if appIDFilter != "" {
			if id, err := uuid.Parse(appIDFilter); err == nil {
				filter.ApplicationID = &id
			}
		}

		envs, err := deps.ConfigService.ListAllEnvironments(c.Request().Context(), filter)
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

		apps, err := listApplications(c.Request().Context(), deps)
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
}

func listApplications(ctx context.Context, deps *Deps) ([]config.Application, error) {
	return deps.ConfigService.ListAllApplications(ctx, "")
}

func environmentCreateHandler(deps *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
		apps, err := listApplications(c.Request().Context(), deps)
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

		env, err := deps.ConfigService.CreateEnvironment(c.Request().Context(), appID, name)
		if err != nil {
			if errors.Is(err, config.ErrAlreadyExists) {
				return pages.EnvironmentForm(flashes(c), navUser(c), pages.EnvironmentFormData{
					CSRFToken: csrfToken(c), Applications: apps, Action: "/environments/create/",
					ApplicationID: applicationID, Name: name, Error: "This application already has an environment with this name.",
				}).Render(c.Request().Context(), c.Response())
			}
			return err
		}
		deps.Activity.LogCreate(requestContext(c), "environment", env.ID.String(), env.Name, nil)
		AddFlash(c, "success", "Environment created.")
		return c.Redirect(http.StatusFound, "/environments/")
	}
}

func environmentEditHandler(deps *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			return echo.NewHTTPError(http.StatusNotFound)
		}
		env, err := deps.ConfigService.GetEnvironmentByID(c.Request().Context(), id)
		if err != nil {
			return err
		}
		if env == nil {
			return echo.NewHTTPError(http.StatusNotFound)
		}
		apps, err := listApplications(c.Request().Context(), deps)
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

		updated, err := deps.ConfigService.UpdateEnvironment(c.Request().Context(), id, appID, name)
		if err != nil {
			return err
		}
		deps.Activity.LogUpdate(requestContext(c), "environment", updated.ID.String(), updated.Name, nil)
		AddFlash(c, "success", "Environment updated.")
		return c.Redirect(http.StatusFound, "/environments/")
	}
}

func environmentDeleteHandler(deps *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			return echo.NewHTTPError(http.StatusNotFound)
		}
		env, err := deps.ConfigService.GetEnvironmentWithApplicationByID(c.Request().Context(), id)
		if err != nil {
			return err
		}
		if env == nil {
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

		if _, err := deps.ConfigService.DeleteEnvironment(c.Request().Context(), id); err != nil {
			return err
		}
		deps.Activity.LogDelete(requestContext(c), "environment", env.ID.String(), env.Name, nil)
		AddFlash(c, "success", "Environment deleted.")
		return c.Redirect(http.StatusFound, "/environments/")
	}
}
