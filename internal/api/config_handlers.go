package api

import (
	"log/slog"
	"net/http"

	"github.com/labstack/echo/v4"
)

// RegisterConfigRoutes mounts the read-only S2S endpoints service clients
// poll for their own configs/secrets and feature flags, mirroring the
// listing routes in config_management/api.py. Everything else
// (create/update/delete/rollback of configs and flags) is web-UI-only now.
func RegisterConfigRoutes(g *echo.Group, deps *Deps) {
	g.GET("/configs/list", listConfigsForClientHandler(deps), APIKeyAuth(deps))
	g.GET("/feature-flags", listFeatureFlagsHandler(deps), APIKeyAuth(deps))
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

type featureFlagResponse struct {
	ID           string `json:"id"`
	Service      string `json:"service"`
	Environment  string `json:"environment"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	IsEnabled    bool   `json:"is_enabled"`
	CreatedCount int    `json:"created_count"`
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
