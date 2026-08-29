package web

import (
	"context"
	"encoding/json"

	"github.com/labstack/echo/v4"

	"controlplane/internal/analytics"
	"controlplane/internal/dashboard"
	analyticsmodel "controlplane/internal/model/analytics"
	configmodel "controlplane/internal/model/config"
	"controlplane/web/template/pages"
)

// DashboardReader is what the dashboard page needs. Satisfied by *dashboard.Service.
type DashboardReader interface {
	GetCounts(ctx context.Context) (dashboard.Counts, error)
	RecentConfigs(ctx context.Context, limit int) ([]configmodel.ConfigEntry, error)
}

// AnalyticsReader is what the dashboard page's trend charts need. Satisfied
// by *analytics.Service.
type AnalyticsReader interface {
	RecentSnapshots(ctx context.Context) ([]analyticsmodel.Snapshot, error)
}

type DashboardHandler struct {
	reader    DashboardReader
	analytics AnalyticsReader
}

func NewDashboardHandler(reader DashboardReader, analyticsReader AnalyticsReader) *DashboardHandler {
	return &DashboardHandler{reader: reader, analytics: analyticsReader}
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
	snapshots, err := h.analytics.RecentSnapshots(ctx)
	if err != nil {
		return err
	}
	analyticsJSON, err := analyticsChartJSON(snapshots)
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
		AnalyticsJSON:    analyticsJSON,
	}

	return pages.Dashboard(flashes(c), navUser(c), data).Render(c.Request().Context(), c.Response())
}

// analyticsChartData is the shape fed to the dashboard page's Chart.js
// charts: one label per hourly snapshot, plus one value slice per series.
type analyticsChartData struct {
	Labels              []string `json:"labels"`
	ActivityCreate      []int64  `json:"activityCreate"`
	ActivityUpdate      []int64  `json:"activityUpdate"`
	ActivityDelete      []int64  `json:"activityDelete"`
	ActivityLogin       []int64  `json:"activityLogin"`
	ActivityLoginFailed []int64  `json:"activityLoginFailed"`
	ConfigCount         []int64  `json:"configCount"`
	SecretCount         []int64  `json:"secretCount"`
	FlagCount           []int64  `json:"flagCount"`
	NotificationSent    []int64  `json:"notificationSent"`
	NotificationFailed  []int64  `json:"notificationFailed"`
	S2SRequests         []int64  `json:"s2sRequests"`
}

// analyticsChartJSON shapes snapshots (oldest first, per AnalyticsReader's
// contract) into analyticsChartData and marshals it, ready to embed via
// templ.JSONScript on the dashboard page.
func analyticsChartJSON(snapshots []analyticsmodel.Snapshot) (string, error) {
	data := analyticsChartData{
		Labels:              make([]string, 0, len(snapshots)),
		ActivityCreate:      make([]int64, 0, len(snapshots)),
		ActivityUpdate:      make([]int64, 0, len(snapshots)),
		ActivityDelete:      make([]int64, 0, len(snapshots)),
		ActivityLogin:       make([]int64, 0, len(snapshots)),
		ActivityLoginFailed: make([]int64, 0, len(snapshots)),
		ConfigCount:         make([]int64, 0, len(snapshots)),
		SecretCount:         make([]int64, 0, len(snapshots)),
		FlagCount:           make([]int64, 0, len(snapshots)),
		NotificationSent:    make([]int64, 0, len(snapshots)),
		NotificationFailed:  make([]int64, 0, len(snapshots)),
		S2SRequests:         make([]int64, 0, len(snapshots)),
	}
	for _, s := range snapshots {
		data.Labels = append(data.Labels, s.CapturedAt.Format("Jan 2 15:04"))
		data.ActivityCreate = append(data.ActivityCreate, s.ActivityCreateCount)
		data.ActivityUpdate = append(data.ActivityUpdate, s.ActivityUpdateCount)
		data.ActivityDelete = append(data.ActivityDelete, s.ActivityDeleteCount)
		data.ActivityLogin = append(data.ActivityLogin, s.ActivityLoginCount)
		data.ActivityLoginFailed = append(data.ActivityLoginFailed, s.ActivityLoginFailedCount)
		data.ConfigCount = append(data.ConfigCount, s.ConfigCount)
		data.SecretCount = append(data.SecretCount, s.SecretCount)
		data.FlagCount = append(data.FlagCount, s.FlagCount)
		data.NotificationSent = append(data.NotificationSent, s.NotificationSentCount)
		data.NotificationFailed = append(data.NotificationFailed, s.NotificationFailedCount)
		data.S2SRequests = append(data.S2SRequests, s.S2SRequestCount)
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

var _ DashboardReader = (*dashboard.Service)(nil)
var _ AnalyticsReader = (*analytics.Service)(nil)
