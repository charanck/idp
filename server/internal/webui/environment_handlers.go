package webui

import (
	"errors"
	"net/http"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"

	"controlplane/internal/config"
	"controlplane/views/pages"
)

const environmentsPageSize = 20

func environmentsListHandler(deps *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
		var envs []config.Environment
		if err := deps.DB.Preload("Application").Order("name").Find(&envs).Error; err != nil {
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

		page := Paginate(groups, environmentsPageSize, PageParam(c))
		return pages.EnvironmentsList(flashes(c), navUser(c), pages.EnvironmentsListData{
			Groups: page.Items, Page: page.Number, NumPages: page.NumPages,
			HasPrev: page.HasPrevious, HasNext: page.HasNext,
			PrevNum: page.PreviousNumber, NextNum: page.NextNumber,
			Window: page.PageRange(),
		}).Render(c.Request().Context(), c.Response())
	}
}

func listApplications(deps *Deps) ([]config.Application, error) {
	var apps []config.Application
	err := deps.DB.Order("name").Find(&apps).Error
	return apps, err
}

func environmentCreateHandler(deps *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
		apps, err := listApplications(deps)
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

		var existing config.Environment
		err = deps.DB.Where("application_id = ? AND name = ?", appID, name).First(&existing).Error
		if err == nil {
			return pages.EnvironmentForm(flashes(c), navUser(c), pages.EnvironmentFormData{
				CSRFToken: csrfToken(c), Applications: apps, Action: "/environments/create/",
				ApplicationID: applicationID, Name: name, Error: "This application already has an environment with this name.",
			}).Render(c.Request().Context(), c.Response())
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		env := config.Environment{ApplicationID: appID, Name: name}
		if err := deps.DB.Create(&env).Error; err != nil {
			return err
		}
		deps.Activity.LogCreate("environment", env.ID.String(), env.Name, requestContext(c), nil)
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
		var env config.Environment
		if err := deps.DB.First(&env, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return echo.NewHTTPError(http.StatusNotFound)
			}
			return err
		}
		apps, err := listApplications(deps)
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

		env.ApplicationID = appID
		env.Name = name
		if err := deps.DB.Save(&env).Error; err != nil {
			return err
		}
		deps.Activity.LogUpdate("environment", env.ID.String(), env.Name, requestContext(c), nil)
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
		var env config.Environment
		if err := deps.DB.Preload("Application").First(&env, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return echo.NewHTTPError(http.StatusNotFound)
			}
			return err
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

		if err := deps.DB.Delete(&env).Error; err != nil {
			return err
		}
		deps.Activity.LogDelete("environment", env.ID.String(), env.Name, requestContext(c), nil)
		AddFlash(c, "success", "Environment deleted.")
		return c.Redirect(http.StatusFound, "/environments/")
	}
}
