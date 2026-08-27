package http_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"

	apihttp "controlplane/api/http"
	"controlplane/internal/auth"
)

func newMiddlewareRequest(apiKey string) (echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/configs/list", nil)
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	return c, rec
}

func TestAPIKeyAuthMiddleware_MissingHeaderReturns401(t *testing.T) {
	mw := apihttp.NewAPIKeyAuthMiddleware(&fakeAPIKeyAuthenticator{}, &fakeRateLimiter{}, 60, 100)
	called := false
	handler := mw.Middleware()(func(c echo.Context) error {
		called = true
		return nil
	})

	c, _ := newMiddlewareRequest("")
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

func TestAPIKeyAuthMiddleware_RateLimitedReturns429WithRetryAfter(t *testing.T) {
	mw := apihttp.NewAPIKeyAuthMiddleware(&fakeAPIKeyAuthenticator{}, &fakeRateLimiter{limited: true}, 60, 100)
	called := false
	handler := mw.Middleware()(func(c echo.Context) error {
		called = true
		return nil
	})

	c, rec := newMiddlewareRequest("key-id.secret")
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

func TestAPIKeyAuthMiddleware_InvalidKeyReturns401(t *testing.T) {
	mw := apihttp.NewAPIKeyAuthMiddleware(&fakeAPIKeyAuthenticator{client: nil}, &fakeRateLimiter{}, 60, 100)
	c, _ := newMiddlewareRequest("bad-key.secret")
	err := mw.Middleware()(func(c echo.Context) error { return nil })(c)
	httpErr, ok := err.(*echo.HTTPError)
	if !ok {
		t.Fatalf("expected *echo.HTTPError, got %T", err)
	}
	if httpErr.Code != http.StatusUnauthorized {
		t.Fatalf("code = %d, want 401", httpErr.Code)
	}
}

func TestAPIKeyAuthMiddleware_ValidKeySetsClientAndCallsNext(t *testing.T) {
	client := &auth.ServiceClient{Name: "billing-service"}
	mw := apihttp.NewAPIKeyAuthMiddleware(&fakeAPIKeyAuthenticator{client: client}, &fakeRateLimiter{}, 60, 100)

	var gotClient *auth.ServiceClient
	handler := mw.Middleware()(func(c echo.Context) error {
		gotClient = apihttp.ServiceClientFromContext(c)
		return nil
	})

	c, _ := newMiddlewareRequest("key-id.secret")
	if err := handler(c); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if gotClient != client {
		t.Fatalf("service client in context = %+v, want %+v", gotClient, client)
	}
}
