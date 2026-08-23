package webui

import (
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"

	"controlplane/internal/config"
	"controlplane/views/pages"
)

func applicationsListHandler(deps *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
		var apps []config.Application
		if err := deps.DB.Order("name").Find(&apps).Error; err != nil {
			return err
		}
		return pages.ApplicationsList(flashes(c), navUser(c), apps).Render(c.Request().Context(), c.Response())
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

		var existing config.Application
		err := deps.DB.Where("name = ?", name).First(&existing).Error
		if err == nil {
			return pages.ApplicationForm(flashes(c), navUser(c), pages.ApplicationFormData{
				CSRFToken: csrfToken(c), Action: "/applications/create/", Name: name,
				Error: "An application with this name already exists.",
			}).Render(c.Request().Context(), c.Response())
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		app := config.Application{Name: name}
		if err := deps.DB.Create(&app).Error; err != nil {
			return err
		}
		deps.Activity.LogCreate("application", app.ID.String(), app.Name, requestContext(c), nil)
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
		var app config.Application
		if err := deps.DB.First(&app, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return echo.NewHTTPError(http.StatusNotFound)
			}
			return err
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

		app.Name = name
		if err := deps.DB.Save(&app).Error; err != nil {
			return err
		}
		deps.Activity.LogUpdate("application", app.ID.String(), app.Name, requestContext(c), nil)
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
		var app config.Application
		if err := deps.DB.First(&app, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return echo.NewHTTPError(http.StatusNotFound)
			}
			return err
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

		if err := deps.DB.Delete(&app).Error; err != nil {
			return err
		}
		deps.Activity.LogDelete("application", app.ID.String(), app.Name, requestContext(c), nil)
		AddFlash(c, "success", "Application deleted.")
		return c.Redirect(http.StatusFound, "/applications/")
	}
}
