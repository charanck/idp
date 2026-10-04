package http_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"

	apihttp "controlplane/api/http"
)

func TestRegisterNotificationRoutes_CORSPreflight(t *testing.T) {
	e := echo.New()
	g := e.Group("/api/v1/notifications")

	apihttp.RegisterNotificationRoutes(
		g,
		&apihttp.NotificationHandler{},
		&apihttp.SessionHandler{},
		&apihttp.SSEHandler{},
		&apihttp.InAppHandler{},
		&apihttp.NotificationAPIKeyAuthMiddleware{},
	)

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/notifications", nil)
	req.Header.Set(echo.HeaderOrigin, "https://app.example.com")
	req.Header.Set(echo.HeaderAccessControlRequestMethod, http.MethodPost)
	req.Header.Set(echo.HeaderAccessControlRequestHeaders, "Authorization, X-API-Key")

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if got := rec.Header().Get(echo.HeaderAccessControlAllowOrigin); got != "*" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want *", got)
	}
	allowMethods := rec.Header().Get(echo.HeaderAccessControlAllowMethods)
	if !strings.Contains(allowMethods, http.MethodPost) || !strings.Contains(allowMethods, http.MethodOptions) {
		t.Fatalf("Access-Control-Allow-Methods = %q, want to include POST and OPTIONS", allowMethods)
	}
}
