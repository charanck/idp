package http_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	apihttp "controlplane/api/http"
	"controlplane/internal/config"
)

func newFeatureFlagListRequest(service, environment string) (echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/feature-flags?service="+service+"&environment="+environment, nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	return c, rec
}

func TestFeatureFlagList_ReturnsFlagsAsJSON(t *testing.T) {
	description := "new checkout flow"
	lister := &fakeFeatureFlagLister{flags: []config.FeatureFlag{
		{
			ID:          uuid.New(),
			Name:        "new-checkout",
			Description: &description,
			IsEnabled:   true,
			Application: config.Application{Name: "payments"},
			Environment: config.Environment{Name: "prod"},
		},
	}}
	h := apihttp.NewFeatureFlagHandler(lister)

	c, rec := newFeatureFlagListRequest("payments", "prod")
	if err := h.List(c); err != nil {
		t.Fatalf("List: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var body []struct {
		Name      string `json:"name"`
		IsEnabled bool   `json:"is_enabled"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(body) != 1 || body[0].Name != "new-checkout" || !body[0].IsEnabled {
		t.Fatalf("unexpected body: %+v", body)
	}
}

func TestFeatureFlagList_ServiceErrorReturns500(t *testing.T) {
	lister := &fakeFeatureFlagLister{err: errFakeService}
	h := apihttp.NewFeatureFlagHandler(lister)

	c, _ := newFeatureFlagListRequest("payments", "prod")
	err := h.List(c)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	httpErr, ok := err.(*echo.HTTPError)
	if !ok {
		t.Fatalf("expected *echo.HTTPError, got %T", err)
	}
	if httpErr.Code != http.StatusInternalServerError {
		t.Fatalf("code = %d, want 500", httpErr.Code)
	}
}
