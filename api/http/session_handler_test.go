package http_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"

	apihttp "controlplane/api/http"
)

func newSessionRequest(body string) (echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/notifications/sessions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	return c, rec
}

func TestSessionCreate_MissingUserIDReturns400(t *testing.T) {
	h := apihttp.NewSessionHandler(&fakeSessionIssuer{}, &fakeApplicationResolver{})

	c, _ := newSessionRequest(`{}`)
	err := h.Create(c)
	httpErr, ok := err.(*echo.HTTPError)
	if !ok {
		t.Fatalf("expected *echo.HTTPError, got %T", err)
	}
	if httpErr.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400", httpErr.Code)
	}
}

func TestSessionCreate_IssuerErrorReturns500(t *testing.T) {
	h := apihttp.NewSessionHandler(&fakeSessionIssuer{err: errFakeNotificationService}, &fakeApplicationResolver{})

	c, _ := newSessionRequest(`{"user_id":"user-1","service":"svc-1"}`)
	err := h.Create(c)
	httpErr, ok := err.(*echo.HTTPError)
	if !ok {
		t.Fatalf("expected *echo.HTTPError, got %T", err)
	}
	if httpErr.Code != http.StatusInternalServerError {
		t.Fatalf("code = %d, want 500", httpErr.Code)
	}
}

func TestSessionCreate_ReturnsTokenAsJSON(t *testing.T) {
	h := apihttp.NewSessionHandler(&fakeSessionIssuer{token: "minted-token"}, &fakeApplicationResolver{})

	c, rec := newSessionRequest(`{"user_id":"user-1","service":"svc-1"}`)
	if err := h.Create(c); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var resp struct {
		Token     string `json:"token"`
		ExpiresIn int    `json:"expires_in_seconds"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Token != "minted-token" || resp.ExpiresIn <= 0 {
		t.Fatalf("unexpected response: %+v", resp)
	}
}
