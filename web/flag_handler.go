package web

import (
	"context"
	"encoding/json"
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

const flagsPageSize = 20

// FlagStore is what feature-flag CRUD handlers need. Satisfied by *config.FeatureFlagService.
type FlagStore interface {
	ListAllFlags(ctx context.Context, filter configmodel.ListFlagsFilter) ([]configmodel.FeatureFlag, error)
	CreateFlag(ctx context.Context, service, name string, opts config.CreateFlagOptions) ([]configmodel.FeatureFlag, error)
	ToggleFlagByID(ctx context.Context, id uuid.UUID) (*configmodel.FeatureFlag, error)
	GetFlagByID(ctx context.Context, id uuid.UUID) (*configmodel.FeatureFlag, error)
	SoftDeleteFlagByID(ctx context.Context, id uuid.UUID) (*configmodel.FeatureFlag, error)
}

type FlagHandler struct {
	flags    FlagStore
	apps     ApplicationStore
	envs     EnvironmentStore
	activity ActivityRecorder
}

func NewFlagHandler(flags FlagStore, apps ApplicationStore, envs EnvironmentStore, activity ActivityRecorder) *FlagHandler {
	return &FlagHandler{flags: flags, apps: apps, envs: envs, activity: activity}
}

func (h *FlagHandler) loadFlagFormApps(c echo.Context) ([]configmodel.Application, string, error) {
	ctx := c.Request().Context()
	allowedIDs, _ := AllowedApplicationIDs(c)
	apps, err := listApplications(ctx, h.apps, allowedIDs)
	if err != nil {
		return nil, "", err
	}
	envs, err := h.envs.ListAllEnvironments(ctx, configmodel.ListEnvironmentsFilter{})
	if err != nil {
		return nil, "", err
	}
	byApp := map[string][]environmentOption{}
	for _, env := range envs {
		key := env.ApplicationID.String()
		byApp[key] = append(byApp[key], environmentOption{ID: env.ID.String(), Name: env.Name})
	}
	b, err := json.Marshal(byApp)
	if err != nil {
		return nil, "", err
	}
	return apps, string(b), nil
}

func (h *FlagHandler) List(c echo.Context) error {
	appIDFilter := c.QueryParam("application_id")
	envIDFilter := c.QueryParam("environment_id")
	statusFilter := c.QueryParam("status")

	allowedIDs, _ := AllowedApplicationIDs(c)
	filter := configmodel.ListFlagsFilter{ApplicationIDs: allowedIDs}
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
	switch statusFilter {
	case "enabled":
		enabled := true
		filter.IsEnabled = &enabled
	case "disabled":
		disabled := false
		filter.IsEnabled = &disabled
	}

	flags, err := h.flags.ListAllFlags(c.Request().Context(), filter)
	if err != nil {
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

	apps, envJSON, err := h.loadFlagFormApps(c)
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

func (h *FlagHandler) Create(c echo.Context) error {
	allowedIDs, _ := AllowedApplicationIDs(c)
	apps, err := listApplications(c.Request().Context(), h.apps, allowedIDs)
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
	app, err := h.apps.GetApplicationByID(c.Request().Context(), appID)
	if err != nil {
		return err
	}
	if app == nil || !ApplicationAllowed(c, app.ID) {
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

	flags, err := h.flags.CreateFlag(c.Request().Context(), app.Name, name, opts)
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
		h.activity.LogCreate(requestContext(c), "feature_flag", flag.ID.String(), flag.Name, nil)
	}
	AddFlash(c, "success", "Feature flag saved.")
	return c.Redirect(http.StatusFound, "/flags/")
}

// Toggle flips a flag's enabled state by primary key, mirroring
// web_ui/views.py's flag_toggle (not FeatureFlagService.ToggleFlag, which
// looks up by service/environment/name strings instead).
func (h *FlagHandler) Toggle(c echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound)
	}
	existing, err := h.flags.GetFlagByID(c.Request().Context(), id)
	if err != nil {
		return err
	}
	if existing == nil || !ApplicationAllowed(c, existing.ApplicationID) {
		return echo.NewHTTPError(http.StatusNotFound)
	}
	flag, err := h.flags.ToggleFlagByID(c.Request().Context(), id)
	if err != nil {
		return err
	}
	if flag == nil {
		return echo.NewHTTPError(http.StatusNotFound)
	}

	h.activity.LogToggle(requestContext(c), "feature_flag", flag.ID.String(), flag.Name, map[string]any{"is_enabled": flag.IsEnabled})
	AddFlash(c, "success", "Feature flag toggled.")
	return c.Redirect(http.StatusFound, "/flags/")
}

// Delete soft-deletes (sets deleted_at), mirroring web_ui/views.py's flag_delete.
func (h *FlagHandler) Delete(c echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound)
	}
	flag, err := h.flags.GetFlagByID(c.Request().Context(), id)
	if err != nil {
		return err
	}
	if flag == nil || !ApplicationAllowed(c, flag.ApplicationID) {
		return echo.NewHTTPError(http.StatusNotFound)
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

	if _, err := h.flags.SoftDeleteFlagByID(c.Request().Context(), id); err != nil {
		return err
	}
	h.activity.LogDelete(requestContext(c), "feature_flag", flag.ID.String(), flag.Name, nil)
	AddFlash(c, "success", "Feature flag deleted.")
	return c.Redirect(http.StatusFound, "/flags/")
}

var _ FlagStore = (*config.FeatureFlagService)(nil)
