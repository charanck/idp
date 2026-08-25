package notification_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"

	"controlplane/internal/notification"
)

func newNotificationMiddlewareRequest(apiKey string) (echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/notifications", nil)
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	return c, rec
}

func TestNotificationAPIKeyAuthMiddleware_MissingHeaderReturns401(t *testing.T) {
	mw := notification.NewAPIKeyAuthMiddleware(&fakeNotificationAuthenticator{}, &fakeNotificationRateLimiter{}, 60, 100)
	called := false
	handler := mw.Middleware()(func(c echo.Context) error {
		called = true
		return nil
	})

	c, _ := newNotificationMiddlewareRequest("")
	err := handler(c)
	if called {
		t.Fatal("next handler should not have been called")
	}
	httpErr, ok := err.(*echo.HTTPError)
	if !ok {
		t.Fatalf("expected *echo.HTTPError, got %T", err)
	}
	if httpErr.Code != http.StatusUnauthorized {
		t.Fatalf("code = %d, want 401", httpErr.Code)
	}
}

func TestNotificationAPIKeyAuthMiddleware_RateLimitedReturns429WithRetryAfter(t *testing.T) {
	mw := notification.NewAPIKeyAuthMiddleware(&fakeNotificationAuthenticator{}, &fakeNotificationRateLimiter{limited: true}, 60, 100)
	called := false
	handler := mw.Middleware()(func(c echo.Context) error {
		called = true
		return nil
	})

	c, rec := newNotificationMiddlewareRequest("key-id.secret")
	if err := handler(c); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if called {
		t.Fatal("next handler should not have been called")
	}
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec.Code)
	}
	if rec.Header().Get("Retry-After") != "60" {
		t.Fatalf("Retry-After = %q, want 60", rec.Header().Get("Retry-After"))
	}
}

func TestNotificationAPIKeyAuthMiddleware_InvalidKeyReturns401(t *testing.T) {
	mw := notification.NewAPIKeyAuthMiddleware(&fakeNotificationAuthenticator{subject: ""}, &fakeNotificationRateLimiter{}, 60, 100)
	c, _ := newNotificationMiddlewareRequest("bad-key.secret")
	err := mw.Middleware()(func(c echo.Context) error { return nil })(c)
	httpErr, ok := err.(*echo.HTTPError)
	if !ok {
		t.Fatalf("expected *echo.HTTPError, got %T", err)
	}
	if httpErr.Code != http.StatusUnauthorized {
		t.Fatalf("code = %d, want 401", httpErr.Code)
	}
}

func TestNotificationAPIKeyAuthMiddleware_ValidKeySetsSubjectAndCallsNext(t *testing.T) {
	mw := notification.NewAPIKeyAuthMiddleware(&fakeNotificationAuthenticator{subject: "billing-service"}, &fakeNotificationRateLimiter{}, 60, 100)

	var gotSubject string
	handler := mw.Middleware()(func(c echo.Context) error {
		gotSubject = notification.SubjectFromContext(c)
		return nil
	})

	c, _ := newNotificationMiddlewareRequest("key-id.secret")
	if err := handler(c); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if gotSubject != "billing-service" {
		t.Fatalf("subject in context = %q, want %q", gotSubject, "billing-service")
	}
}
