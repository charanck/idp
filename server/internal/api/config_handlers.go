package api

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/labstack/echo/v4"

	"controlplane/internal/config"
)

// RegisterConfigRoutes mounts /configs/... and /feature-flags/... under the
// given group, mirroring config_management/api.py.
func RegisterConfigRoutes(g *echo.Group, deps *Deps) {
	g.POST("/configs/upsert", upsertConfigHandler(deps), JWTAuth(deps))
	g.GET("/configs/:config_id/history", getConfigHistoryHandler(deps), JWTAuth(deps))
	g.POST("/configs/:config_id/rollback", rollbackConfigHandler(deps), JWTAuth(deps))
	g.GET("/configs/list", listConfigsForClientHandler(deps), APIKeyAuth(deps))

	g.POST("/feature-flags", createFeatureFlagHandler(deps), JWTAuth(deps))
	g.GET("/feature-flags", listFeatureFlagsHandler(deps), JWTOrAPIKeyAuth(deps))
	g.POST("/feature-flags/:name/toggle", toggleFeatureFlagHandler(deps), JWTAuth(deps))
}

type configUpsertRequest struct {
	Service     string `json:"service"`
	Environment string `json:"environment"`
	Key         string `json:"key"`
	Value       string `json:"value"`
	IsSecret    bool   `json:"is_secret"`
	Type        string `json:"type"`
}

type configResponse struct {
	ID          string `json:"id"`
	Service     string `json:"service"`
	Environment string `json:"environment"`
	Key         string `json:"key"`
	Value       string `json:"value"`
	IsSecret    bool   `json:"is_secret"`
	Type        string `json:"type"`
}

func upsertConfigHandler(deps *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
		var req configUpsertRequest
		if err := c.Bind(&req); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
		}
		if req.Service == "" || req.Environment == "" || req.Key == "" {
			return echo.NewHTTPError(http.StatusUnprocessableEntity, "service, environment and key are required")
		}
		configType := req.Type
		if configType == "" {
			configType = config.TypeString
		}

		existedBefore, err := deps.ConfigService.GetConfig(req.Service, req.Environment, req.Key)
		if err != nil {
			slog.Error("unexpected error checking existing config", "service", req.Service, "environment", req.Environment, "key", req.Key, "err", err)
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to save configuration")
		}

		entry, err := deps.ConfigService.UpsertConfig(c.Request().Context(), req.Service, req.Environment, req.Key, req.Value, config.UpsertOptions{
			IsSecret:   req.IsSecret,
			ConfigType: configType,
			ChangedBy:  requestContext(c).UserEmail,
		})
		if err != nil {
			slog.Error("unexpected error upserting config", "service", req.Service, "environment", req.Environment, "key", req.Key, "err", err)
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to save configuration")
		}

		configName := req.Service + "/" + req.Environment + "/" + entry.Key
		details := map[string]any{"is_secret": entry.IsSecret, "type": entry.Type}
		if existedBefore != nil {
			deps.Activity.LogUpdate("config", entry.ID.String(), configName, requestContext(c), details)
		} else {
			deps.Activity.LogCreate("config", entry.ID.String(), configName, requestContext(c), details)
		}

		return c.JSON(http.StatusOK, configResponse{
			ID:          entry.ID.String(),
			Service:     req.Service,
			Environment: req.Environment,
			Key:         entry.Key,
			Value:       "***ENCRYPTED***",
			IsSecret:    entry.IsSecret,
			Type:        entry.Type,
		})
	}
}

type configVersionResponse struct {
	Version   int     `json:"version"`
	Action    string  `json:"action"`
	Value     *string `json:"value"`
	IsSecret  bool    `json:"is_secret"`
	Type      string  `json:"type"`
	ChangedBy *string `json:"changed_by"`
	CreatedAt string  `json:"created_at"`
}

func getConfigHistoryHandler(deps *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
		configID := c.Param("config_id")
		versions, err := deps.ConfigService.GetConfigHistory(configID)
		if err != nil {
			return err
		}

		out := make([]configVersionResponse, 0, len(versions))
		for _, v := range versions {
			var value *string
			if !v.IsSecret {
				decrypted, err := deps.ConfigService.DecryptVersionValue(&v)
				if err != nil {
					return err
				}
				value = &decrypted
			}
			out = append(out, configVersionResponse{
				Version:   v.Version,
				Action:    v.Action,
				Value:     value,
				IsSecret:  v.IsSecret,
				Type:      v.Type,
				ChangedBy: v.ChangedBy,
				CreatedAt: v.CreatedAt.Format("2006-01-02T15:04:05.999999-07:00"),
			})
		}
		return c.JSON(http.StatusOK, out)
	}
}

type configRollbackRequest struct {
	Version int `json:"version"`
}

func rollbackConfigHandler(deps *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
		configID := c.Param("config_id")
		var req configRollbackRequest
		if err := c.Bind(&req); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
		}
		if req.Version <= 0 {
			return echo.NewHTTPError(http.StatusUnprocessableEntity, "version must be greater than 0")
		}

		entry, err := deps.ConfigService.RollbackConfig(c.Request().Context(), configID, req.Version, requestContext(c).UserEmail)
		if err != nil {
			slog.Error("unexpected error rolling back config", "config_id", configID, "version", req.Version, "err", err)
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to roll back configuration")
		}
		if entry == nil {
			return echo.NewHTTPError(http.StatusNotFound, "Config or version not found")
		}

		configName := entry.Application.Name + "/" + entry.Environment.Name + "/" + entry.Key
		deps.Activity.LogUpdate("config", entry.ID.String(), configName, requestContext(c), map[string]any{
			"is_secret":              entry.IsSecret,
			"rolled_back_to_version": req.Version,
		})

		return c.JSON(http.StatusOK, configResponse{
			ID:          entry.ID.String(),
			Service:     entry.Application.Name,
			Environment: entry.Environment.Name,
			Key:         entry.Key,
			Value:       "***ENCRYPTED***",
			IsSecret:    entry.IsSecret,
			Type:        entry.Type,
		})
	}
}

func listConfigsForClientHandler(deps *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
		service := c.QueryParam("service")
		environment := c.QueryParam("environment")
		client := ServiceClientFromContext(c)

		configs, err := deps.ConfigService.ListConfigsForClient(c.Request().Context(), service, environment, client.EncryptionKey)
		if err != nil {
			slog.Error("unexpected error listing configs for client", "client", client.Name, "service", service, "environment", environment, "err", err)
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to list configurations")
		}

		out := make([]configResponse, 0, len(configs))
		for _, cfg := range configs {
			out = append(out, configResponse{
				ID:          cfg.ID,
				Service:     cfg.Service,
				Environment: cfg.Environment,
				Key:         cfg.Key,
				Value:       cfg.Value,
				IsSecret:    cfg.IsSecret,
				Type:        cfg.Type,
			})
		}
		return c.JSON(http.StatusOK, out)
	}
}

type featureFlagCreateRequest struct {
	Service     string `json:"service"`
	Environment string `json:"environment"`
	Name        string `json:"name"`
	Description string `json:"description"`
	IsEnabled   bool   `json:"is_enabled"`
}

type featureFlagResponse struct {
	ID           string `json:"id"`
	Service      string `json:"service"`
	Environment  string `json:"environment"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	IsEnabled    bool   `json:"is_enabled"`
	CreatedCount int    `json:"created_count"`
}

func createFeatureFlagHandler(deps *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
		var req featureFlagCreateRequest
		if err := c.Bind(&req); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
		}
		if req.Service == "" {
			return echo.NewHTTPError(http.StatusUnprocessableEntity, "service is required")
		}
		if len(req.Name) == 0 || len(req.Name) > 120 {
			return echo.NewHTTPError(http.StatusUnprocessableEntity, "name must be between 1 and 120 characters")
		}

		flags, err := deps.FlagService.CreateFlag(c.Request().Context(), req.Service, req.Name, config.CreateFlagOptions{
			Description:           req.Description,
			IsEnabled:             req.IsEnabled,
			Environment:           req.Environment,
			CreateAllEnvironments: true,
		})
		if err != nil {
			if errors.Is(err, config.ErrApplicationNotFound) || errors.Is(err, config.ErrEnvironmentRequired) || errors.Is(err, config.ErrNoEnvironmentsFound) {
				return echo.NewHTTPError(http.StatusConflict, err.Error())
			}
			slog.Error("unexpected error creating feature flag", "service", req.Service, "name", req.Name, "err", err)
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to create feature flag")
		}

		flag := flags[0]
		description := ""
		if flag.Description != nil {
			description = *flag.Description
		}
		deps.Activity.LogCreate("flag", flag.ID.String(), flag.Application.Name+"/"+flag.Name, requestContext(c), map[string]any{
			"is_enabled":            flag.IsEnabled,
			"environments_affected": len(flags),
		})
		return c.JSON(http.StatusOK, featureFlagResponse{
			ID:           flag.ID.String(),
			Service:      flag.Application.Name,
			Environment:  flag.Environment.Name,
			Name:         flag.Name,
			Description:  description,
			IsEnabled:    flag.IsEnabled,
			CreatedCount: len(flags),
		})
	}
}

func listFeatureFlagsHandler(deps *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
		service := c.QueryParam("service")
		environment := c.QueryParam("environment")

		flags, err := deps.FlagService.ListFlags(c.Request().Context(), service, environment)
		if err != nil {
			slog.Error("unexpected error listing feature flags", "service", service, "environment", environment, "err", err)
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to list feature flags")
		}

		out := make([]featureFlagResponse, 0, len(flags))
		for _, flag := range flags {
			description := ""
			if flag.Description != nil {
				description = *flag.Description
			}
			out = append(out, featureFlagResponse{
				ID:           flag.ID.String(),
				Service:      flag.Application.Name,
				Environment:  flag.Environment.Name,
				Name:         flag.Name,
				Description:  description,
				IsEnabled:    flag.IsEnabled,
				CreatedCount: 1,
			})
		}
		return c.JSON(http.StatusOK, out)
	}
}

func toggleFeatureFlagHandler(deps *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
		name := c.Param("name")
		service := c.QueryParam("service")
		environment := c.QueryParam("environment")

		flag, err := deps.FlagService.ToggleFlag(c.Request().Context(), service, environment, name)
		if err != nil {
			slog.Error("unexpected error toggling feature flag", "service", service, "environment", environment, "name", name, "err", err)
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to toggle feature flag")
		}
		if flag == nil {
			return echo.NewHTTPError(http.StatusNotFound, "Feature flag not found")
		}

		description := ""
		if flag.Description != nil {
			description = *flag.Description
		}
		deps.Activity.LogToggle("flag", flag.ID.String(), flag.Application.Name+"/"+flag.Environment.Name+"/"+flag.Name, requestContext(c), map[string]any{
			"is_enabled": flag.IsEnabled,
		})
		return c.JSON(http.StatusOK, featureFlagResponse{
			ID:           flag.ID.String(),
			Service:      flag.Application.Name,
			Environment:  flag.Environment.Name,
			Name:         flag.Name,
			Description:  description,
			IsEnabled:    flag.IsEnabled,
			CreatedCount: 1,
		})
	}
}
