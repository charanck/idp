package http_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	apihttp "controlplane/api/http"
	authmodel "controlplane/internal/model/auth"
	configmodel "controlplane/internal/model/config"
)

func newFeatureFlagListRequest(service, environment string) (echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/feature-flags?service="+service+"&environment="+environment, nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("service_client", &authmodel.ServiceClient{Name: "billing-service"})
	return c, rec
}

func TestFeatureFlagList_ReturnsFlagsAsJSON(t *testing.T) {
	description := "new checkout flow"
	lister := &fakeFeatureFlagLister{flags: []configmodel.FeatureFlag{
		{
			ID:          uuid.New(),
			Name:        "new-checkout",
			Description: &description,
			IsEnabled:   true,
			Application: configmodel.Application{Name: "payments"},
			Environment: configmodel.Environment{Name: "prod"},
		},
	}}
	h := apihttp.NewFeatureFlagHandler(lister, &fakeApplicationFinder{}, &fakeClientApplicationScoper{})

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

func TestFeatureFlagList_OutsideApplicationScopeReturns404(t *testing.T) {
	paymentsID := uuid.New()
	lister := &fakeFeatureFlagLister{flags: []configmodel.FeatureFlag{}}
	apps := &fakeApplicationFinder{apps: map[string]*configmodel.Application{
		"other-service": {ID: uuid.New()},
	}}
	scoper := &fakeClientApplicationScoper{allowedIDs: []uuid.UUID{paymentsID}}
	h := apihttp.NewFeatureFlagHandler(lister, apps, scoper)

	c, rec := newFeatureFlagListRequest("other-service", "prod")
	if err := h.List(c); err != nil {
		httpErr, ok := err.(*echo.HTTPError)
		if !ok || httpErr.Code != http.StatusNotFound {
			t.Fatalf("List: %v, want 404", err)
		}
	} else if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestFeatureFlagList_ServiceErrorReturns500(t *testing.T) {
	lister := &fakeFeatureFlagLister{err: errFakeService}
	h := apihttp.NewFeatureFlagHandler(lister, &fakeApplicationFinder{}, &fakeClientApplicationScoper{})

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
