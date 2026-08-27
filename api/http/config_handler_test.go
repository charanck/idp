package http_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"

	apihttp "controlplane/api/http"
	"controlplane/internal/config"
	authmodel "controlplane/internal/model/auth"
)

func newConfigListRequest(service, environment string) (echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/configs/list?service="+service+"&environment="+environment, nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("service_client", &authmodel.ServiceClient{Name: "billing-service", EncryptionKey: "key"})
	return c, rec
}

func TestList_ReturnsConfigsAsJSON(t *testing.T) {
	lister := &fakeConfigLister{configs: []config.ClientConfig{
		{ID: "1", Service: "payments", Environment: "prod", Key: "API_URL", Value: "https://api.example.com", Type: "string", IsSecret: false},
	}}
	h := apihttp.NewConfigHandler(lister)

	c, rec := newConfigListRequest("payments", "prod")
	if err := h.List(c); err != nil {
		t.Fatalf("List: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var body []struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(body) != 1 || body[0].Key != "API_URL" || body[0].Value != "https://api.example.com" {
		t.Fatalf("unexpected body: %+v", body)
	}
}

func TestList_ServiceErrorReturns500(t *testing.T) {
	lister := &fakeConfigLister{err: errFakeService}
	h := apihttp.NewConfigHandler(lister)

	c, _ := newConfigListRequest("payments", "prod")
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
