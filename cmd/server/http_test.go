package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"

	"controlplane/internal/traceid"
)

const testTraceID = "4bf92f3577b34da6a3ce929d0e0e4736"

func serveTraced(path string, headers map[string]string) (*httptest.ResponseRecorder, string) {
	e := echo.New()
	var seen string
	e.Use(traceIDMiddleware())
	e.GET("/*", func(c echo.Context) error {
		seen = traceid.FromContext(c.Request().Context())
		return c.NoContent(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec, seen
}

func TestTraceIDMiddleware_UsesHeaderAndEchoesIt(t *testing.T) {
	rec, seen := serveTraced("/api/v1/config/configs/list", map[string]string{"X-Request-Id": testTraceID})
	if seen != testTraceID || rec.Header().Get(traceid.ResponseHeader) != testTraceID {
		t.Fatalf("ctx=%q header=%q, want %q", seen, rec.Header().Get(traceid.ResponseHeader), testTraceID)
	}
}

func TestTraceIDMiddleware_GeneratesWhenMissing(t *testing.T) {
	rec, seen := serveTraced("/oauth2/token", nil)
	if len(seen) != 32 || rec.Header().Get(traceid.ResponseHeader) != seen {
		t.Fatalf("ctx=%q header=%q", seen, rec.Header().Get(traceid.ResponseHeader))
	}
}

func TestTraceIDMiddleware_SkipsWebUI(t *testing.T) {
	for _, path := range []string{"/login/", "/oauth2/authorize", "/static/app.css"} {
		rec, seen := serveTraced(path, map[string]string{"X-Trace-Id": testTraceID})
		if seen != "" || rec.Header().Get(traceid.ResponseHeader) != "" {
			t.Errorf("%s: ctx=%q header=%q, want none", path, seen, rec.Header().Get(traceid.ResponseHeader))
		}
	}
}
