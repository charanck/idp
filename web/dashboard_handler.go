package web

import (
	"context"

	"github.com/labstack/echo/v4"

	"controlplane/internal/config"
	"controlplane/internal/dashboard"
	"controlplane/web/template/pages"
)

// DashboardReader is what the dashboard page needs. Satisfied by *dashboard.Service.
type DashboardReader interface {
	GetCounts(ctx context.Context) (dashboard.Counts, error)
	RecentConfigs(ctx context.Context, limit int) ([]config.ConfigEntry, error)
}

type DashboardHandler struct {
	reader DashboardReader
}

func NewDashboardHandler(reader DashboardReader) *DashboardHandler {
	return &DashboardHandler{reader: reader}
}

func (h *DashboardHandler) Show(c echo.Context) error {
	ctx := c.Request().Context()

	counts, err := h.reader.GetCounts(ctx)
	if err != nil {
		return err
	}
	recentConfigs, err := h.reader.RecentConfigs(ctx, 5)
	if err != nil {
		return err
	}

	data := pages.DashboardData{
		ApplicationCount: counts.ApplicationCount,
		EnvironmentCount: counts.EnvironmentCount,
		ConfigCount:      counts.ConfigCount,
		SecretCount:      counts.SecretCount,
		FlagCount:        counts.FlagCount,
		ClientCount:      counts.ClientCount,
		RecentConfigs:    recentConfigs,
	}

	return pages.Dashboard(flashes(c), navUser(c), data).Render(c.Request().Context(), c.Response())
}

var _ DashboardReader = (*dashboard.Service)(nil)
