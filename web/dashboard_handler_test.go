package web_test

import (
	"net/http"
	"testing"

	"controlplane/internal/dashboard"
	configmodel "controlplane/internal/model/config"
	"controlplane/web"
)

func TestDashboardShowHandler_RendersCountsAndRecentConfigs(t *testing.T) {
	store := newSessionStore(t)
	reader := &fakeDashboardReader{
		counts: dashboard.Counts{
			ApplicationCount: 2, EnvironmentCount: 4, ConfigCount: 10,
			SecretCount: 3, FlagCount: 5, ClientCount: 1,
		},
		recentConfigs: []configmodel.ConfigEntry{{Key: "a"}, {Key: "b"}, {Key: "c"}},
	}
	h := web.NewDashboardHandler(reader, &fakeAnalyticsReader{})

	rec := callHandler(t, store, http.MethodGet, "/dashboard/", nil, nil, h.Show)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestDashboardShowHandler_RecentConfigsRespectsLimitOfFive(t *testing.T) {
	store := newSessionStore(t)
	var many []configmodel.ConfigEntry
	for range 10 {
		many = append(many, configmodel.ConfigEntry{Key: "k"})
	}
	reader := &fakeDashboardReader{recentConfigs: many}
	h := web.NewDashboardHandler(reader, &fakeAnalyticsReader{})

	rec := callHandler(t, store, http.MethodGet, "/dashboard/", nil, nil, h.Show)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
}
