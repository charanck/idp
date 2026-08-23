package webui

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"

	"controlplane/internal/config"
	"controlplane/views/pages"
)

const flagsPageSize = 20

func flagsListHandler(deps *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
		appIDFilter := c.QueryParam("application_id")
		envIDFilter := c.QueryParam("environment_id")
		statusFilter := c.QueryParam("status")

		query := deps.DB.WithContext(c.Request().Context()).Preload("Application").Preload("Environment").
			Joins("JOIN applications ON applications.id = feature_flags.application_id").
			Where("feature_flags.deleted_at IS NULL").
			Order("applications.name, feature_flags.name, feature_flags.description")
		if appIDFilter != "" {
			if _, err := uuid.Parse(appIDFilter); err == nil {
				query = query.Where("feature_flags.application_id = ?", appIDFilter)
			}
		}
		if envIDFilter != "" {
			if _, err := uuid.Parse(envIDFilter); err == nil {
				query = query.Where("feature_flags.environment_id = ?", envIDFilter)
			}
		}
		switch statusFilter {
		case "enabled":
			query = query.Where("feature_flags.is_enabled = true")
		case "disabled":
			query = query.Where("feature_flags.is_enabled = false")
		}

		var flags []config.FeatureFlag
		if err := query.Find(&flags).Error; err != nil {
			return err
		}

		type groupKey struct {
			appID string
			name  string
			desc  string
		}
		groupsByKey := map[groupKey]*pages.FlagGroup{}
		var order []groupKey

		for _, flag := range flags {
			desc := ""
			if flag.Description != nil {
				desc = *flag.Description
			}
			gk := groupKey{appID: flag.ApplicationID.String(), name: flag.Name, desc: desc}
			g, ok := groupsByKey[gk]
			if !ok {
				g = &pages.FlagGroup{
					ApplicationID: flag.ApplicationID.String(), ApplicationName: flag.Application.Name,
					Name: flag.Name, Description: desc,
				}
				groupsByKey[gk] = g
				order = append(order, gk)
			}
			g.Entries = append(g.Entries, pages.FlagGroupEntry{
				ID: flag.ID.String(), EnvironmentName: flag.Environment.Name, IsEnabled: flag.IsEnabled,
			})
		}

		groups := make([]pages.FlagGroup, 0, len(order))
		for _, k := range order {
			groups = append(groups, *groupsByKey[k])
		}

		apps, envJSON, err := loadConfigFormApps(c.Request().Context(), deps)
		if err != nil {
			return err
		}

		page := Paginate(groups, flagsPageSize, PageParam(c))

		extra := url.Values{}
		if appIDFilter != "" {
			extra.Set("application_id", appIDFilter)
		}
		if envIDFilter != "" {
			extra.Set("environment_id", envIDFilter)
		}
		if statusFilter != "" {
			extra.Set("status", statusFilter)
		}

		return pages.FlagsList(flashes(c), navUser(c), pages.FlagsListData{
			Groups: page.Items, CSRFToken: csrfToken(c), Applications: apps, EnvironmentsByAppJSON: envJSON,
			CurrentAppID: appIDFilter, CurrentEnvID: envIDFilter, CurrentStatus: statusFilter,
			Page: page.Number, NumPages: page.NumPages,
			HasPrev: page.HasPrevious, HasNext: page.HasNext,
			PrevNum: page.PreviousNumber, NextNum: page.NextNumber,
			Window: page.PageRange(), ExtraQuery: extra.Encode(),
		}).Render(c.Request().Context(), c.Response())
	}
}

func flagCreateHandler(deps *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
		apps, err := listApplications(c.Request().Context(), deps)
		if err != nil {
			return err
		}

		if c.Request().Method == http.MethodGet {
			return pages.FlagForm(flashes(c), navUser(c), pages.FlagFormData{
				CSRFToken: csrfToken(c), Applications: apps,
			}).Render(c.Request().Context(), c.Response())
		}

		reRender := func(errMsg string) error {
			return pages.FlagForm(flashes(c), navUser(c), pages.FlagFormData{
				CSRFToken: csrfToken(c), Applications: apps,
				ApplicationID: c.FormValue("application_id"), Name: c.FormValue("name"),
				Description: c.FormValue("description"), IsEnabled: c.FormValue("is_enabled") != "",
				Error: errMsg,
			}).Render(c.Request().Context(), c.Response())
		}

		appID, err := uuid.Parse(c.FormValue("application_id"))
		if err != nil {
			return reRender("Application is required.")
		}
		var app config.Application
		if err := deps.DB.WithContext(c.Request().Context()).First(&app, "id = ?", appID).Error; err != nil {
			return reRender("Application not found.")
		}

		name := strings.TrimSpace(c.FormValue("name"))
		if name == "" {
			return reRender("Name is required.")
		}

		opts := config.CreateFlagOptions{
			Description:           c.FormValue("description"),
			IsEnabled:             c.FormValue("is_enabled") != "",
			CreateAllEnvironments: true,
		}

		flags, err := deps.FlagService.CreateFlag(c.Request().Context(), app.Name, name, opts)
		if err != nil {
			switch {
			case errors.Is(err, config.ErrApplicationNotFound):
				return reRender("Application does not exist.")
			case errors.Is(err, config.ErrEnvironmentRequired):
				return reRender("Environment is required.")
			case errors.Is(err, config.ErrNoEnvironmentsFound):
				return reRender("No environments found for the selected application.")
			default:
				return err
			}
		}

		for _, flag := range flags {
			deps.Activity.LogCreate(c.Request().Context(), "feature_flag", flag.ID.String(), flag.Name, requestContext(c), nil)
		}
		AddFlash(c, "success", "Feature flag saved.")
		return c.Redirect(http.StatusFound, "/flags/")
	}
}

// flagToggleHandler mirrors web_ui/views.py's flag_toggle: a direct
// fetch-by-ID/flip/save, not FeatureFlagService.ToggleFlag (which looks up
// by service/environment/name strings, not by primary key).
func flagToggleHandler(deps *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			return echo.NewHTTPError(http.StatusNotFound)
		}
		var flag config.FeatureFlag
		if err := deps.DB.WithContext(c.Request().Context()).Preload("Application").Preload("Environment").First(&flag, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return echo.NewHTTPError(http.StatusNotFound)
			}
			return err
		}

		flag.IsEnabled = !flag.IsEnabled
		if err := deps.DB.WithContext(c.Request().Context()).Save(&flag).Error; err != nil {
			return err
		}
		if err := deps.FlagService.InvalidateScopeCache(c.Request().Context(), flag.Application.Name, flag.Environment.Name); err != nil {
			return err
		}

		deps.Activity.LogToggle(c.Request().Context(), "feature_flag", flag.ID.String(), flag.Name, requestContext(c), map[string]any{"is_enabled": flag.IsEnabled})
		AddFlash(c, "success", "Feature flag toggled.")
		return c.Redirect(http.StatusFound, "/flags/")
	}
}

// flagDeleteHandler soft-deletes (sets deleted_at), mirroring
// web_ui/views.py's flag_delete.
func flagDeleteHandler(deps *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			return echo.NewHTTPError(http.StatusNotFound)
		}
		var flag config.FeatureFlag
		if err := deps.DB.WithContext(c.Request().Context()).Preload("Application").Preload("Environment").First(&flag, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return echo.NewHTTPError(http.StatusNotFound)
			}
			return err
		}

		if c.Request().Method == http.MethodGet {
			return pages.ConfirmDelete(flashes(c), navUser(c), "flags", pages.ConfirmDeleteData{
				CSRFToken:  csrfToken(c),
				Title:      "Delete Feature Flag",
				Message:    "Are you sure you want to delete \"" + flag.Application.Name + "/" + flag.Environment.Name + "/" + flag.Name + "\"?",
				Action:     "/flags/" + flag.ID.String() + "/delete/",
				CancelHref: "/flags/",
			}).Render(c.Request().Context(), c.Response())
		}

		now := time.Now()
		flag.DeletedAt = &now
		if err := deps.DB.WithContext(c.Request().Context()).Save(&flag).Error; err != nil {
			return err
		}
		if err := deps.FlagService.InvalidateScopeCache(c.Request().Context(), flag.Application.Name, flag.Environment.Name); err != nil {
			return err
		}

		deps.Activity.LogDelete(c.Request().Context(), "feature_flag", flag.ID.String(), flag.Name, requestContext(c), nil)
		AddFlash(c, "success", "Feature flag deleted.")
		return c.Redirect(http.StatusFound, "/flags/")
	}
}
