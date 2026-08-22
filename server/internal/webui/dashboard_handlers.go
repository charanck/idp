package webui

import (
	"github.com/labstack/echo/v4"

	"controlplane/internal/auth"
	"controlplane/internal/config"
	"controlplane/views/pages"
)

func dashboardHandler(deps *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
		var data pages.DashboardData

		if err := deps.DB.Model(&config.Application{}).Count(&data.ApplicationCount).Error; err != nil {
			return err
		}
		if err := deps.DB.Model(&config.Environment{}).Count(&data.EnvironmentCount).Error; err != nil {
			return err
		}
		if err := deps.DB.Model(&config.ConfigEntry{}).Where("is_secret = ?", false).Count(&data.ConfigCount).Error; err != nil {
			return err
		}
		if err := deps.DB.Model(&config.ConfigEntry{}).Where("is_secret = ?", true).Count(&data.SecretCount).Error; err != nil {
			return err
		}
		if err := deps.DB.Model(&config.FeatureFlag{}).Where("deleted_at IS NULL").Count(&data.FlagCount).Error; err != nil {
			return err
		}
		if err := deps.DB.Model(&auth.ServiceClient{}).Count(&data.ClientCount).Error; err != nil {
			return err
		}
		if err := deps.DB.Preload("Application").Preload("Environment").
			Order("updated_at DESC").Limit(5).Find(&data.RecentConfigs).Error; err != nil {
			return err
		}

		return pages.Dashboard(flashes(c), navUser(c), data).Render(c.Request().Context(), c.Response())
	}
}
