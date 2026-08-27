package web

import (
	"context"
	"net/url"

	"github.com/labstack/echo/v4"

	"controlplane/internal/activity"
	"controlplane/internal/config"
	"controlplane/web/template/pages"
)

const activityPageSize = 50

// ActivityReader is the read side of the audit log, used by the activity log
// page. Satisfied by *activity.Logger.
type ActivityReader interface {
	List(ctx context.Context, filter activity.ListFilter) ([]config.Activity, error)
	DistinctResources(ctx context.Context) ([]string, error)
	DistinctTypes(ctx context.Context) ([]string, error)
}

type ActivityHandler struct {
	reader ActivityReader
}

func NewActivityHandler(reader ActivityReader) *ActivityHandler {
	return &ActivityHandler{reader: reader}
}

func (h *ActivityHandler) List(c echo.Context) error {
	resourceFilter := c.QueryParam("resource")
	typeFilter := c.QueryParam("type")
	userFilter := c.QueryParam("user")

	activities, err := h.reader.List(c.Request().Context(), activity.ListFilter{
		Resource: resourceFilter, Type: typeFilter, UserLike: userFilter,
	})
	if err != nil {
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

	resourceTypes, err := h.reader.DistinctResources(c.Request().Context())
	if err != nil {
		return err
	}
	actionTypes, err := h.reader.DistinctTypes(c.Request().Context())
	if err != nil {
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

var _ ActivityReader = (*activity.Logger)(nil)
