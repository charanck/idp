package e2e

import (
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"testing"
)

// analyticsScriptRe matches the templ.JSONScript-rendered <script> tag
// web/template/pages/dashboard.templ embeds the chart payload in - see
// web/dashboard_handler.go's analyticsChartJSON.
var analyticsScriptRe = regexp.MustCompile(`(?s)<script id="analytics-data" type="application/json">(.*?)</script>`)

// analyticsChartData mirrors web/dashboard_handler.go's unexported
// analyticsChartData: the shape fed to the dashboard's Chart.js trend
// charts, one label per hourly snapshot plus one value slice per series.
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

// TestDashboard_EmbedsValidAnalyticsChartJSON proves the dashboard actually
// renders the analytics payload the Chart.js trend charts read from, not
// just that the page loads - real snapshots are only ever produced by the
// hourly DBOS-scheduled workflow (internal/analytics/scheduler.go), so this
// can't drive a snapshot into existence over HTTP; instead it asserts the
// embedded JSON is well-formed and internally consistent (every series the
// same length as labels), which is what would break if the handler's
// marshaling or the template's script tag ever drifted apart.
func TestDashboard_EmbedsValidAnalyticsChartJSON(t *testing.T) {
	base := e2eBaseURL(t)
	admin := newAdminSession(t)

	resp, err := admin.http.Get(base + "/dashboard/")
	if err != nil {
		t.Fatalf("GET /dashboard/: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read dashboard body: %v", err)
	}

	m := analyticsScriptRe.FindSubmatch(body)
	if m == nil {
		t.Fatalf("no analytics-data script tag found in dashboard response")
	}

	var data analyticsChartData
	if err := json.Unmarshal(m[1], &data); err != nil {
		t.Fatalf("unmarshal analytics-data payload: %v\npayload: %s", err, m[1])
	}

	want := len(data.Labels)
	for name, series := range map[string][]int64{
		"activityCreate":      data.ActivityCreate,
		"activityUpdate":      data.ActivityUpdate,
		"activityDelete":      data.ActivityDelete,
		"activityLogin":       data.ActivityLogin,
		"activityLoginFailed": data.ActivityLoginFailed,
		"configCount":         data.ConfigCount,
		"secretCount":         data.SecretCount,
		"flagCount":           data.FlagCount,
		"notificationSent":    data.NotificationSent,
		"notificationFailed":  data.NotificationFailed,
		"s2sRequests":         data.S2SRequests,
	} {
		if len(series) != want {
			t.Fatalf("series %q has %d entries, want %d (one per label): %+v", name, len(series), want, data)
		}
	}
}

// TestDashboard_RequiresLogin_DoesNotExposeAnalytics asserts an
// unauthenticated request is bounced to /login/ rather than rendering the
// dashboard (and, with it, the analytics chart payload) - the same
// LoginRequired check other admin/session-only pages get, applied here so
// analytics data specifically is never reachable without a session.
func TestDashboard_RequiresLogin_DoesNotExposeAnalytics(t *testing.T) {
	base := e2eBaseURL(t)
	anon := newAnonymousClient(t)

	resp, err := anon.Get(base + "/dashboard/")
	if err != nil {
		t.Fatalf("GET /dashboard/: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); len(loc) < 7 || loc[:7] != "/login/" {
		t.Fatalf("Location = %q, want a redirect to /login/", loc)
	}
}
