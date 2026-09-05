package http

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/labstack/echo/v4"

	"controlplane/internal/config"
	configmodel "controlplane/internal/model/config"
)

// FeatureFlagLister is the narrow slice of *config.FeatureFlagService that
// FeatureFlagHandler needs.
type FeatureFlagLister interface {
	ListFlags(ctx context.Context, service, environment string) ([]configmodel.FeatureFlag, error)
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

// FeatureFlagHandler serves the read-only S2S feature-flag listing endpoint.
type FeatureFlagHandler struct {
	flags   FeatureFlagLister
	apps    ApplicationFinder
	clients ClientApplicationScoper
}

func NewFeatureFlagHandler(flags FeatureFlagLister, apps ApplicationFinder, clients ClientApplicationScoper) *FeatureFlagHandler {
	return &FeatureFlagHandler{flags: flags, apps: apps, clients: clients}
}

// List serves GET /feature-flags.
func (h *FeatureFlagHandler) List(c echo.Context) error {
	service := c.QueryParam("service")
	environment := c.QueryParam("environment")
	client := ServiceClientFromContext(c)

	if err := checkApplicationScope(c.Request().Context(), h.apps, h.clients, client.ID, service); err != nil {
		return err
	}

	flags, err := h.flags.ListFlags(c.Request().Context(), service, environment)
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

var _ FeatureFlagLister = (*config.FeatureFlagService)(nil)
