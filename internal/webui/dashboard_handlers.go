package webui

import (
	"github.com/labstack/echo/v4"

	"controlplane/views/pages"
)

func dashboardHandler(deps *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx := c.Request().Context()

		counts, err := deps.Dashboard.GetCounts(ctx)
		if err != nil {
			return err
		}
		recentConfigs, err := deps.Dashboard.RecentConfigs(ctx, 5)
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
}
