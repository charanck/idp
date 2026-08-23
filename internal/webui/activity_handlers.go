package webui

import (
	"net/url"

	"github.com/labstack/echo/v4"

	"controlplane/internal/config"
	"controlplane/views/pages"
)

const activityPageSize = 50

func activityLogHandler(deps *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
		resourceFilter := c.QueryParam("resource")
		typeFilter := c.QueryParam("type")
		userFilter := c.QueryParam("user")

		query := deps.DB.WithContext(c.Request().Context()).Model(&config.Activity{}).Order("timestamp DESC")
		if resourceFilter != "" {
			query = query.Where("resource = ?", resourceFilter)
		}
		if typeFilter != "" {
			query = query.Where("type = ?", typeFilter)
		}
		if userFilter != "" {
			query = query.Where("user_email ILIKE ?", "%"+userFilter+"%")
		}

		var activities []config.Activity
		if err := query.Find(&activities).Error; err != nil {
			return err
		}

		rows := make([]pages.ActivityRow, 0, len(activities))
		for _, a := range activities {
			resourceName := ""
			if a.ResourceName != nil {
				resourceName = *a.ResourceName
			}
			userEmail := ""
			if a.UserEmail != nil {
				userEmail = *a.UserEmail
			}
			ip := ""
			if a.IPAddress != nil {
				ip = *a.IPAddress
			}
			rows = append(rows, pages.ActivityRow{
				Type: a.Type, Resource: a.Resource, ResourceName: resourceName,
				UserEmail: userEmail, IPAddress: ip,
				Timestamp: a.Timestamp.Format("2006-01-02 15:04:05"),
			})
		}

		var resourceTypes []string
		if err := deps.DB.WithContext(c.Request().Context()).Model(&config.Activity{}).Order("resource").Distinct("resource").Pluck("resource", &resourceTypes).Error; err != nil {
			return err
		}
		var actionTypes []string
		if err := deps.DB.WithContext(c.Request().Context()).Model(&config.Activity{}).Order("type").Distinct("type").Pluck("type", &actionTypes).Error; err != nil {
			return err
		}

		page := Paginate(rows, activityPageSize, PageParam(c))

		extra := url.Values{}
		if resourceFilter != "" {
			extra.Set("resource", resourceFilter)
		}
		if typeFilter != "" {
			extra.Set("type", typeFilter)
		}
		if userFilter != "" {
			extra.Set("user", userFilter)
		}

		return pages.ActivityLog(flashes(c), navUser(c), pages.ActivityLogData{
			Activities: page.Items, ResourceTypes: resourceTypes, ActionTypes: actionTypes,
			CurrentResource: resourceFilter, CurrentType: typeFilter, CurrentUser: userFilter,
			Page: page.Number, NumPages: page.NumPages, HasPrev: page.HasPrevious, HasNext: page.HasNext,
			PrevNum: page.PreviousNumber, NextNum: page.NextNumber, Window: page.SlidingWindow(2),
			ExtraQuery: extra.Encode(),
		}).Render(c.Request().Context(), c.Response())
	}
}
