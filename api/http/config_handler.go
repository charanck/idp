package http

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/labstack/echo/v4"

	"controlplane/internal/config"
)

// ConfigLister is the narrow slice of *config.ConfigService that
// ConfigHandler needs.
type ConfigLister interface {
	ListConfigsForClient(ctx context.Context, service, environment, clientEncryptionKey string) ([]config.ClientConfig, error)
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

// ConfigHandler serves the read-only S2S config/secrets listing endpoint.
type ConfigHandler struct {
	configs ConfigLister
}

func NewConfigHandler(configs ConfigLister) *ConfigHandler {
	return &ConfigHandler{configs: configs}
}

// List serves GET /configs/list.
func (h *ConfigHandler) List(c echo.Context) error {
	service := c.QueryParam("service")
	environment := c.QueryParam("environment")
	client := ServiceClientFromContext(c)

	configs, err := h.configs.ListConfigsForClient(c.Request().Context(), service, environment, client.EncryptionKey)
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

var _ ConfigLister = (*config.ConfigService)(nil)
