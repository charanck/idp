package webui

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"

	"controlplane/internal/config"
	"controlplane/views/pages"
)

const configsPageSize = 20

type environmentOption struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// environmentsByApplicationJSON builds the JSON blob the config form's
// cascading application->environment <select> reads client-side, mirroring
// web_ui/views.py's _environments_by_application().
func environmentsByApplicationJSON(deps *Deps) (string, error) {
	var envs []config.Environment
	if err := deps.DB.Order("name").Find(&envs).Error; err != nil {
		return "", err
	}
	byApp := map[string][]environmentOption{}
	for _, env := range envs {
		key := env.ApplicationID.String()
		byApp[key] = append(byApp[key], environmentOption{ID: env.ID.String(), Name: env.Name})
	}
	b, err := json.Marshal(byApp)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func configsListHandler(deps *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
		var entries []config.ConfigEntry
		if err := deps.DB.Preload("Application").Preload("Environment").
			Joins("JOIN applications ON applications.id = config_entries.application_id").
			Order("applications.name, config_entries.key, config_entries.is_secret, config_entries.type").
			Find(&entries).Error; err != nil {
			return err
		}

		type groupKey struct {
			appID    string
			key      string
			isSecret bool
			cfgType  string
		}
		groupsByKey := map[groupKey]*pages.ConfigGroup{}
		var order []groupKey

		for _, entry := range entries {
			gk := groupKey{appID: entry.ApplicationID.String(), key: entry.Key, isSecret: entry.IsSecret, cfgType: entry.Type}
			g, ok := groupsByKey[gk]
			if !ok {
				g = &pages.ConfigGroup{
					ApplicationID: entry.ApplicationID.String(), ApplicationName: entry.Application.Name,
					Key: entry.Key, IsSecret: entry.IsSecret, Type: entry.Type,
				}
				groupsByKey[gk] = g
				order = append(order, gk)
			}

			displayValue := ""
			if !entry.IsSecret {
				displayValue = deps.ConfigService.DecryptConfigValueOrOriginal(&entry)
			}
			g.Entries = append(g.Entries, pages.ConfigGroupEntry{
				ID: entry.ID.String(), EnvironmentName: entry.Environment.Name, DisplayValue: displayValue,
			})
		}

		groups := make([]pages.ConfigGroup, 0, len(order))
		for _, k := range order {
			groups = append(groups, *groupsByKey[k])
		}

		page := Paginate(groups, configsPageSize, PageParam(c))
		return pages.ConfigsList(flashes(c), navUser(c), pages.ConfigsListData{
			Groups: page.Items, Page: page.Number, NumPages: page.NumPages,
			HasPrev: page.HasPrevious, HasNext: page.HasNext,
			PrevNum: page.PreviousNumber, NextNum: page.NextNumber,
			Window: page.PageRange(),
		}).Render(c.Request().Context(), c.Response())
	}
}

func loadConfigFormApps(deps *Deps) ([]config.Application, string, error) {
	apps, err := listApplications(deps)
	if err != nil {
		return nil, "", err
	}
	envJSON, err := environmentsByApplicationJSON(deps)
	if err != nil {
		return nil, "", err
	}
	return apps, envJSON, nil
}

// upsertFromForm parses & applies application/environment/key/value/type/
// is_secret/submit_action fields, mirroring _upsert_config_from_form /
// _upsert_config_for_all_environments.
func upsertConfigFromForm(deps *Deps, c echo.Context, historyAction string) error {
	appID, err := uuid.Parse(c.FormValue("application_id"))
	if err != nil {
		return errors.New("application is required")
	}
	var app config.Application
	if err := deps.DB.First(&app, "id = ?", appID).Error; err != nil {
		return errors.New("application not found")
	}

	key := strings.TrimSpace(c.FormValue("key"))
	if key == "" {
		return errors.New("key is required")
	}
	value := c.FormValue("value")
	cfgType := c.FormValue("type")
	isSecret := c.FormValue("is_secret") != ""
	changedBy := ""
	if user := CurrentUser(c); user != nil {
		changedBy = user.Email
	}

	opts := config.UpsertOptions{IsSecret: isSecret, ConfigType: cfgType, ChangedBy: changedBy, HistoryAction: historyAction}

	if c.FormValue("submit_action") == "create_all_env" {
		var envs []config.Environment
		if err := deps.DB.Where("application_id = ?", appID).Find(&envs).Error; err != nil {
			return err
		}
		if len(envs) == 0 {
			return errors.New("this application has no environments yet")
		}
		for _, env := range envs {
			if _, err := deps.ConfigService.UpsertConfig(c.Request().Context(), app.Name, env.Name, key, value, opts); err != nil {
				return err
			}
		}
		return nil
	}

	envID, err := uuid.Parse(c.FormValue("environment_id"))
	if err != nil {
		return errors.New("environment is required")
	}
	var env config.Environment
	if err := deps.DB.First(&env, "id = ? AND application_id = ?", envID, appID).Error; err != nil {
		return errors.New("environment not found for the selected application")
	}

	_, err = deps.ConfigService.UpsertConfig(c.Request().Context(), app.Name, env.Name, key, value, opts)
	return err
}

func configCreateHandler(deps *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
		apps, envJSON, err := loadConfigFormApps(deps)
		if err != nil {
			return err
		}

		if c.Request().Method == http.MethodGet {
			return pages.ConfigForm(flashes(c), navUser(c), pages.ConfigFormData{
				CSRFToken: csrfToken(c), Applications: apps, EnvironmentsByAppJSON: envJSON,
				Action: "/configs/create/", Title: "New Config", ShowSubmitAction: true, Type: config.TypeString,
			}).Render(c.Request().Context(), c.Response())
		}

		if err := upsertConfigFromForm(deps, c, config.ActionCreate); err != nil {
			return pages.ConfigForm(flashes(c), navUser(c), rePopulateConfigForm(c, apps, envJSON, "/configs/create/", "New Config", true, err.Error())).
				Render(c.Request().Context(), c.Response())
		}

		AddFlash(c, "success", "Config saved.")
		return c.Redirect(http.StatusFound, "/configs/")
	}
}

func rePopulateConfigForm(c echo.Context, apps []config.Application, envJSON, action, title string, showSubmitAction bool, errMsg string) pages.ConfigFormData {
	return pages.ConfigFormData{
		CSRFToken: csrfToken(c), Applications: apps, EnvironmentsByAppJSON: envJSON,
		ApplicationID: c.FormValue("application_id"), EnvironmentID: c.FormValue("environment_id"),
		Key: c.FormValue("key"), Value: c.FormValue("value"), Type: c.FormValue("type"),
		IsSecret: c.FormValue("is_secret") != "", Action: action, Title: title,
		ShowSubmitAction: showSubmitAction, Error: errMsg,
	}
}

func configCloneHandler(deps *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			return echo.NewHTTPError(http.StatusNotFound)
		}
		var entry config.ConfigEntry
		if err := deps.DB.Preload("Application").First(&entry, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return echo.NewHTTPError(http.StatusNotFound)
			}
			return err
		}

		apps, envJSON, err := loadConfigFormApps(deps)
		if err != nil {
			return err
		}
		action := "/configs/" + entry.ID.String() + "/clone/"

		if c.Request().Method == http.MethodGet {
			value := ""
			if !entry.IsSecret {
				value = deps.ConfigService.DecryptConfigValueOrOriginal(&entry)
			}
			return pages.ConfigForm(flashes(c), navUser(c), pages.ConfigFormData{
				CSRFToken: csrfToken(c), Applications: apps, EnvironmentsByAppJSON: envJSON,
				ApplicationID: entry.ApplicationID.String(), Key: entry.Key, Value: value,
				Type: entry.Type, IsSecret: entry.IsSecret, Action: action, Title: "Clone Config", ShowSubmitAction: true,
			}).Render(c.Request().Context(), c.Response())
		}

		if err := upsertConfigFromForm(deps, c, config.ActionCreate); err != nil {
			return pages.ConfigForm(flashes(c), navUser(c), rePopulateConfigForm(c, apps, envJSON, action, "Clone Config", true, err.Error())).
				Render(c.Request().Context(), c.Response())
		}

		AddFlash(c, "success", "Config cloned.")
		return c.Redirect(http.StatusFound, "/configs/")
	}
}

// configEditHandler mutates the ConfigEntry directly rather than going
// through ConfigService.UpsertConfig, mirroring web_ui/views.py's config_edit.
func configEditHandler(deps *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			return echo.NewHTTPError(http.StatusNotFound)
		}
		var entry config.ConfigEntry
		if err := deps.DB.First(&entry, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return echo.NewHTTPError(http.StatusNotFound)
			}
			return err
		}

		apps, envJSON, err := loadConfigFormApps(deps)
		if err != nil {
			return err
		}
		action := "/configs/" + entry.ID.String() + "/edit/"

		if c.Request().Method == http.MethodGet {
			value := ""
			if !entry.IsSecret {
				value = deps.ConfigService.DecryptConfigValueOrOriginal(&entry)
			}
			return pages.ConfigForm(flashes(c), navUser(c), pages.ConfigFormData{
				CSRFToken: csrfToken(c), Applications: apps, EnvironmentsByAppJSON: envJSON,
				ApplicationID: entry.ApplicationID.String(), EnvironmentID: entry.EnvironmentID.String(),
				Key: entry.Key, Value: value, Type: entry.Type, IsSecret: entry.IsSecret,
				Action: action, Title: "Edit Config", IsEdit: true,
			}).Render(c.Request().Context(), c.Response())
		}

		appID, err := uuid.Parse(c.FormValue("application_id"))
		if err != nil {
			return rePopulateConfigEditForm(c, apps, envJSON, action, "application is required")
		}
		envID, err := uuid.Parse(c.FormValue("environment_id"))
		if err != nil {
			return rePopulateConfigEditForm(c, apps, envJSON, action, "environment is required")
		}
		var env config.Environment
		if err := deps.DB.First(&env, "id = ? AND application_id = ?", envID, appID).Error; err != nil {
			return rePopulateConfigEditForm(c, apps, envJSON, action, "environment not found for the selected application")
		}
		var app config.Application
		if err := deps.DB.First(&app, "id = ?", appID).Error; err != nil {
			return rePopulateConfigEditForm(c, apps, envJSON, action, "application not found")
		}

		key := strings.TrimSpace(c.FormValue("key"))
		if key == "" {
			return rePopulateConfigEditForm(c, apps, envJSON, action, "key is required")
		}

		encryptedValue, err := deps.Encryption.EncryptForStorage(c.FormValue("value"))
		if err != nil {
			return err
		}

		entry.ApplicationID = appID
		entry.EnvironmentID = envID
		entry.Key = key
		entry.Type = c.FormValue("type")
		entry.IsSecret = c.FormValue("is_secret") != ""
		entry.Value = encryptedValue
		if err := deps.DB.Save(&entry).Error; err != nil {
			return err
		}

		changedBy := ""
		if user := CurrentUser(c); user != nil {
			changedBy = user.Email
		}
		if _, err := deps.ConfigService.RecordConfigVersion(&entry, config.ActionUpdate, changedBy); err != nil {
			return err
		}
		if err := deps.ConfigService.InvalidateScopeCache(c.Request().Context(), app.Name, env.Name); err != nil {
			return err
		}

		AddFlash(c, "success", "Config updated.")
		return c.Redirect(http.StatusFound, "/configs/")
	}
}

func rePopulateConfigEditForm(c echo.Context, apps []config.Application, envJSON, action, errMsg string) error {
	data := rePopulateConfigForm(c, apps, envJSON, action, "Edit Config", false, errMsg)
	data.IsEdit = true
	return pages.ConfigForm(flashes(c), navUser(c), data).Render(c.Request().Context(), c.Response())
}

func configDeleteHandler(deps *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
		id := c.Param("id")
		parsedID, parseErr := uuid.Parse(id)
		if parseErr != nil {
			return echo.NewHTTPError(http.StatusNotFound)
		}
		var entry config.ConfigEntry
		if err := deps.DB.Preload("Application").Preload("Environment").First(&entry, "id = ?", parsedID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return echo.NewHTTPError(http.StatusNotFound)
			}
			return err
		}

		if c.Request().Method == http.MethodGet {
			return pages.ConfirmDelete(flashes(c), navUser(c), "configs", pages.ConfirmDeleteData{
				CSRFToken:  csrfToken(c),
				Title:      "Delete Config",
				Message:    "Are you sure you want to delete \"" + entry.Application.Name + "/" + entry.Environment.Name + "/" + entry.Key + "\"? Its version history will be deleted too.",
				Action:     "/configs/" + id + "/delete/",
				CancelHref: "/configs/",
			}).Render(c.Request().Context(), c.Response())
		}

		ok, err := deps.ConfigService.DeleteConfig(c.Request().Context(), id)
		if err != nil {
			return err
		}
		if !ok {
			return echo.NewHTTPError(http.StatusNotFound)
		}
		deps.Activity.LogDelete("config", id, entry.Key, requestContext(c), nil)
		AddFlash(c, "success", "Config deleted.")
		return c.Redirect(http.StatusFound, "/configs/")
	}
}

func configHistoryHandler(deps *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
		id := c.Param("id")
		parsedID, parseErr := uuid.Parse(id)
		if parseErr != nil {
			return echo.NewHTTPError(http.StatusNotFound)
		}
		var entry config.ConfigEntry
		if err := deps.DB.Preload("Application").Preload("Environment").First(&entry, "id = ?", parsedID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return echo.NewHTTPError(http.StatusNotFound)
			}
			return err
		}

		versions, err := deps.ConfigService.GetConfigHistory(id)
		if err != nil {
			return err
		}

		viewVersions := make([]pages.ConfigHistoryVersion, 0, len(versions))
		for _, v := range versions {
			changedBy := "-"
			if v.ChangedBy != nil && *v.ChangedBy != "" {
				changedBy = *v.ChangedBy
			}
			viewVersions = append(viewVersions, pages.ConfigHistoryVersion{
				Version: strconv.Itoa(v.Version), Action: v.Action, ChangedBy: changedBy,
				CreatedAt: v.CreatedAt.Format("2006-01-02 15:04:05 MST"),
			})
		}

		return pages.ConfigHistory(flashes(c), navUser(c), pages.ConfigHistoryData{
			ConfigID: id, Application: entry.Application.Name, Environment: entry.Environment.Name, Key: entry.Key,
			Versions: viewVersions, CSRFToken: csrfToken(c),
		}).Render(c.Request().Context(), c.Response())
	}
}

func configRollbackHandler(deps *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
		id := c.Param("id")
		version, err := strconv.Atoi(c.Param("version"))
		if err != nil {
			return echo.NewHTTPError(http.StatusNotFound)
		}

		changedBy := ""
		if user := CurrentUser(c); user != nil {
			changedBy = user.Email
		}

		updated, err := deps.ConfigService.RollbackConfig(c.Request().Context(), id, version, changedBy)
		if err != nil {
			return err
		}
		if updated == nil {
			return echo.NewHTTPError(http.StatusNotFound)
		}

		deps.Activity.LogUpdate("config", id, updated.Key, requestContext(c), map[string]any{"rolled_back_to_version": version})
		AddFlash(c, "success", "Rolled back to version "+strconv.Itoa(version)+".")
		return c.Redirect(http.StatusFound, "/configs/"+id+"/history/")
	}
}
