package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"controlplane/internal/config"
	configmodel "controlplane/internal/model/config"
	"controlplane/web/template/pages"
)

const configsPageSize = 20

type environmentOption struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ConfigStore is what config/secret CRUD handlers need. Satisfied by *config.ConfigService.
type ConfigStore interface {
	ListAllConfigEntries(ctx context.Context, filter configmodel.ListConfigEntriesFilter) ([]configmodel.ConfigEntry, error)
	GetConfigByID(ctx context.Context, id uuid.UUID) (*configmodel.ConfigEntry, error)
	UpsertConfig(ctx context.Context, service, environment, key, value string, opts config.UpsertOptions) (*configmodel.ConfigEntry, error)
	UpdateConfigEntry(ctx context.Context, id uuid.UUID, in config.UpdateConfigEntryInput) (*configmodel.ConfigEntry, error)
	DeleteConfig(ctx context.Context, configID string) (bool, error)
	GetConfigHistory(ctx context.Context, configID string) ([]configmodel.ConfigEntryVersion, error)
	RollbackConfig(ctx context.Context, configID string, version int, changedBy string) (*configmodel.ConfigEntry, error)
	DecryptConfigValueOrOriginal(entry *configmodel.ConfigEntry) string
}

type ConfigHandler struct {
	configs  ConfigStore
	envs     EnvironmentStore
	apps     ApplicationStore
	activity ActivityRecorder
}

func NewConfigHandler(configs ConfigStore, envs EnvironmentStore, apps ApplicationStore, activity ActivityRecorder) *ConfigHandler {
	return &ConfigHandler{configs: configs, envs: envs, apps: apps, activity: activity}
}

// environmentsByApplicationJSON builds the JSON blob the config form's
// cascading application->environment <select> reads client-side, mirroring
// web_ui/views.py's _environments_by_application().
func (h *ConfigHandler) environmentsByApplicationJSON(ctx context.Context) (string, error) {
	envs, err := h.envs.ListAllEnvironments(ctx, configmodel.ListEnvironmentsFilter{})
	if err != nil {
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

func (h *ConfigHandler) List(c echo.Context) error {
	appIDFilter := c.QueryParam("application_id")
	envIDFilter := c.QueryParam("environment_id")
	secretFilter := c.QueryParam("secret")
	q := strings.TrimSpace(c.QueryParam("q"))

	filter := configmodel.ListConfigEntriesFilter{Query: q}
	if appIDFilter != "" {
		if id, err := uuid.Parse(appIDFilter); err == nil {
			filter.ApplicationID = &id
		}
	}
	if envIDFilter != "" {
		if id, err := uuid.Parse(envIDFilter); err == nil {
			filter.EnvironmentID = &id
		}
	}
	switch secretFilter {
	case "secret":
		v := true
		filter.IsSecret = &v
	case "config":
		v := false
		filter.IsSecret = &v
	}

	entries, err := h.configs.ListAllConfigEntries(c.Request().Context(), filter)
	if err != nil {
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
			displayValue = h.configs.DecryptConfigValueOrOriginal(&entry)
		}
		g.Entries = append(g.Entries, pages.ConfigGroupEntry{
			ID: entry.ID.String(), EnvironmentName: entry.Environment.Name, DisplayValue: displayValue,
		})
	}

	groups := make([]pages.ConfigGroup, 0, len(order))
	for _, k := range order {
		groups = append(groups, *groupsByKey[k])
	}

	apps, envJSON, err := h.loadConfigFormApps(c.Request().Context())
	if err != nil {
		return err
	}

	page := Paginate(groups, configsPageSize, PageParam(c))

	extra := url.Values{}
	if appIDFilter != "" {
		extra.Set("application_id", appIDFilter)
	}
	if envIDFilter != "" {
		extra.Set("environment_id", envIDFilter)
	}
	if secretFilter != "" {
		extra.Set("secret", secretFilter)
	}
	if q != "" {
		extra.Set("q", q)
	}

	return pages.ConfigsList(flashes(c), navUser(c), pages.ConfigsListData{
		Groups: page.Items, Applications: apps, EnvironmentsByAppJSON: envJSON,
		CurrentAppID: appIDFilter, CurrentEnvID: envIDFilter, CurrentQ: q, CurrentSecret: secretFilter,
		Page: page.Number, NumPages: page.NumPages, HasPrev: page.HasPrevious, HasNext: page.HasNext,
		PrevNum: page.PreviousNumber, NextNum: page.NextNumber,
		Window: page.PageRange(), ExtraQuery: extra.Encode(),
	}).Render(c.Request().Context(), c.Response())
}

func (h *ConfigHandler) loadConfigFormApps(ctx context.Context) ([]configmodel.Application, string, error) {
	apps, err := listApplications(ctx, h.apps)
	if err != nil {
		return nil, "", err
	}
	envJSON, err := h.environmentsByApplicationJSON(ctx)
	if err != nil {
		return nil, "", err
	}
	return apps, envJSON, nil
}

// upsertConfigFromForm parses & applies application/environment/key/value/type/
// is_secret/submit_action fields, mirroring _upsert_config_from_form /
// _upsert_config_for_all_environments.
func (h *ConfigHandler) upsertConfigFromForm(c echo.Context, historyAction string) error {
	appID, err := uuid.Parse(c.FormValue("application_id"))
	if err != nil {
		return errors.New("application is required")
	}
	app, err := h.apps.GetApplicationByID(c.Request().Context(), appID)
	if err != nil {
		return err
	}
	if app == nil {
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
		envs, err := h.envs.ListEnvironmentsByApplicationID(c.Request().Context(), appID)
		if err != nil {
			return err
		}
		if len(envs) == 0 {
			return errors.New("this application has no environments yet")
		}
		for _, env := range envs {
			if _, err := h.configs.UpsertConfig(c.Request().Context(), app.Name, env.Name, key, value, opts); err != nil {
				return err
			}
		}
		return nil
	}

	envID, err := uuid.Parse(c.FormValue("environment_id"))
	if err != nil {
		return errors.New("environment is required")
	}
	env, err := h.envs.GetEnvironmentByID(c.Request().Context(), envID)
	if err != nil {
		return err
	}
	if env == nil || env.ApplicationID != appID {
		return errors.New("environment not found for the selected application")
	}

	_, err = h.configs.UpsertConfig(c.Request().Context(), app.Name, env.Name, key, value, opts)
	return err
}

func (h *ConfigHandler) Create(c echo.Context) error {
	apps, envJSON, err := h.loadConfigFormApps(c.Request().Context())
	if err != nil {
		return err
	}

	if c.Request().Method == http.MethodGet {
		return pages.ConfigForm(flashes(c), navUser(c), pages.ConfigFormData{
			CSRFToken: csrfToken(c), Applications: apps, EnvironmentsByAppJSON: envJSON,
			Action: "/configs/create/", Title: "New Config", ShowSubmitAction: true, Type: configmodel.TypeString,
		}).Render(c.Request().Context(), c.Response())
	}

	if err := h.upsertConfigFromForm(c, configmodel.ActionCreate); err != nil {
		return pages.ConfigForm(flashes(c), navUser(c), rePopulateConfigForm(c, apps, envJSON, "/configs/create/", "New Config", true, err.Error())).
			Render(c.Request().Context(), c.Response())
	}

	AddFlash(c, "success", "Config saved.")
	return c.Redirect(http.StatusFound, "/configs/")
}

func rePopulateConfigForm(c echo.Context, apps []configmodel.Application, envJSON, action, title string, showSubmitAction bool, errMsg string) pages.ConfigFormData {
	return pages.ConfigFormData{
		CSRFToken: csrfToken(c), Applications: apps, EnvironmentsByAppJSON: envJSON,
		ApplicationID: c.FormValue("application_id"), EnvironmentID: c.FormValue("environment_id"),
		Key: c.FormValue("key"), Value: c.FormValue("value"), Type: c.FormValue("type"),
		IsSecret: c.FormValue("is_secret") != "", Action: action, Title: title,
		ShowSubmitAction: showSubmitAction, Error: errMsg,
	}
}

func (h *ConfigHandler) Clone(c echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound)
	}
	entry, err := h.configs.GetConfigByID(c.Request().Context(), id)
	if err != nil {
		return err
	}
	if entry == nil {
		return echo.NewHTTPError(http.StatusNotFound)
	}
	apps, envJSON, err := h.loadConfigFormApps(c.Request().Context())
	if err != nil {
		return err
	}
	action := "/configs/" + entry.ID.String() + "/clone/"

	if c.Request().Method == http.MethodGet {
		value := ""
		if !entry.IsSecret {
			value = h.configs.DecryptConfigValueOrOriginal(entry)
		}
		return pages.ConfigForm(flashes(c), navUser(c), pages.ConfigFormData{
			CSRFToken: csrfToken(c), Applications: apps, EnvironmentsByAppJSON: envJSON,
			ApplicationID: entry.ApplicationID.String(), Key: entry.Key, Value: value,
			Type: entry.Type, IsSecret: entry.IsSecret, Action: action, Title: "Clone Config", ShowSubmitAction: true,
		}).Render(c.Request().Context(), c.Response())
	}

	if err := h.upsertConfigFromForm(c, configmodel.ActionCreate); err != nil {
		return pages.ConfigForm(flashes(c), navUser(c), rePopulateConfigForm(c, apps, envJSON, action, "Clone Config", true, err.Error())).
			Render(c.Request().Context(), c.Response())
	}

	AddFlash(c, "success", "Config cloned.")
	return c.Redirect(http.StatusFound, "/configs/")
}

// Edit mutates the ConfigEntry directly (via ConfigStore.UpdateConfigEntry)
// rather than going through ConfigStore.UpsertConfig, mirroring
// web_ui/views.py's config_edit.
func (h *ConfigHandler) Edit(c echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound)
	}
	entry, err := h.configs.GetConfigByID(c.Request().Context(), id)
	if err != nil {
		return err
	}
	if entry == nil {
		return echo.NewHTTPError(http.StatusNotFound)
	}
	apps, envJSON, err := h.loadConfigFormApps(c.Request().Context())
	if err != nil {
		return err
	}
	action := "/configs/" + entry.ID.String() + "/edit/"

	if c.Request().Method == http.MethodGet {
		value := ""
		if !entry.IsSecret {
			value = h.configs.DecryptConfigValueOrOriginal(entry)
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
		return h.rePopulateConfigEditForm(c, apps, envJSON, action, "application is required")
	}
	envID, err := uuid.Parse(c.FormValue("environment_id"))
	if err != nil {
		return h.rePopulateConfigEditForm(c, apps, envJSON, action, "environment is required")
	}
	env, err := h.envs.GetEnvironmentByID(c.Request().Context(), envID)
	if err != nil {
		return err
	}
	if env == nil || env.ApplicationID != appID {
		return h.rePopulateConfigEditForm(c, apps, envJSON, action, "environment not found for the selected application")
	}
	app, err := h.apps.GetApplicationByID(c.Request().Context(), appID)
	if err != nil {
		return err
	}
	if app == nil {
		return h.rePopulateConfigEditForm(c, apps, envJSON, action, "application not found")
	}

	key := strings.TrimSpace(c.FormValue("key"))
	if key == "" {
		return h.rePopulateConfigEditForm(c, apps, envJSON, action, "key is required")
	}

	changedBy := ""
	if user := CurrentUser(c); user != nil {
		changedBy = user.Email
	}

	if _, err := h.configs.UpdateConfigEntry(c.Request().Context(), id, config.UpdateConfigEntryInput{
		ApplicationID: appID, EnvironmentID: envID, Key: key,
		Value: c.FormValue("value"), ConfigType: c.FormValue("type"),
		IsSecret: c.FormValue("is_secret") != "", ChangedBy: changedBy,
	}); err != nil {
		return err
	}

	AddFlash(c, "success", "Config updated.")
	return c.Redirect(http.StatusFound, "/configs/")
}

func (h *ConfigHandler) rePopulateConfigEditForm(c echo.Context, apps []configmodel.Application, envJSON, action, errMsg string) error {
	data := rePopulateConfigForm(c, apps, envJSON, action, "Edit Config", false, errMsg)
	data.IsEdit = true
	return pages.ConfigForm(flashes(c), navUser(c), data).Render(c.Request().Context(), c.Response())
}

func (h *ConfigHandler) Delete(c echo.Context) error {
	id := c.Param("id")
	parsedID, parseErr := uuid.Parse(id)
	if parseErr != nil {
		return echo.NewHTTPError(http.StatusNotFound)
	}
	entry, err := h.configs.GetConfigByID(c.Request().Context(), parsedID)
	if err != nil {
		return err
	}
	if entry == nil {
		return echo.NewHTTPError(http.StatusNotFound)
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

	ok, err := h.configs.DeleteConfig(c.Request().Context(), id)
	if err != nil {
		return err
	}
	if !ok {
		return echo.NewHTTPError(http.StatusNotFound)
	}
	h.activity.LogDelete(requestContext(c), "config", id, entry.Key, nil)
	AddFlash(c, "success", "Config deleted.")
	return c.Redirect(http.StatusFound, "/configs/")
}

func (h *ConfigHandler) History(c echo.Context) error {
	id := c.Param("id")
	parsedID, parseErr := uuid.Parse(id)
	if parseErr != nil {
		return echo.NewHTTPError(http.StatusNotFound)
	}
	entry, err := h.configs.GetConfigByID(c.Request().Context(), parsedID)
	if err != nil {
		return err
	}
	if entry == nil {
		return echo.NewHTTPError(http.StatusNotFound)
	}
	versions, err := h.configs.GetConfigHistory(c.Request().Context(), id)
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

func (h *ConfigHandler) Rollback(c echo.Context) error {
	id := c.Param("id")
	version, err := strconv.Atoi(c.Param("version"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound)
	}

	changedBy := ""
	if user := CurrentUser(c); user != nil {
		changedBy = user.Email
	}

	updated, err := h.configs.RollbackConfig(c.Request().Context(), id, version, changedBy)
	if err != nil {
		return err
	}
	if updated == nil {
		return echo.NewHTTPError(http.StatusNotFound)
	}
	h.activity.LogUpdate(requestContext(c), "config", id, updated.Key, map[string]any{"rolled_back_to_version": version})
	AddFlash(c, "success", "Rolled back to version "+strconv.Itoa(version)+".")
	return c.Redirect(http.StatusFound, "/configs/"+id+"/history/")
}

var _ ConfigStore = (*config.ConfigService)(nil)
