package http_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"

	apihttp "controlplane/api/http"
)

type fakeSubscriber struct {
	rdb *redis.Client
}

func (f *fakeSubscriber) Subscribe(ctx context.Context, userID string) *redis.PubSub {
	return f.rdb.Subscribe(ctx, "notif:"+userID)
}

func newSSERequest(bearer string) (echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/notifications/sse/events", nil)
	if bearer != "" {
		req.Header.Set(echo.HeaderAuthorization, "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	return c, rec
}

func TestStream_MissingBearerTokenReturns401(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	defer mr.Close()
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	h := apihttp.NewSSEHandler(&fakeSessionValidator{}, &fakeSubscriber{rdb: rdb})

	c, _ := newSSERequest("")
	err = h.Stream(c)
	httpErr, ok := err.(*echo.HTTPError)
	if !ok {
		t.Fatalf("expected *echo.HTTPError, got %T", err)
	}
	if httpErr.Code != http.StatusUnauthorized {
		t.Fatalf("code = %d, want 401", httpErr.Code)
	}
}

func TestStream_InvalidTokenReturns401(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	defer mr.Close()
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	h := apihttp.NewSSEHandler(&fakeSessionValidator{err: errFakeNotificationService}, &fakeSubscriber{rdb: rdb})

	c, _ := newSSERequest("bad-token")
	err = h.Stream(c)
	httpErr, ok := err.(*echo.HTTPError)
	if !ok {
		t.Fatalf("expected *echo.HTTPError, got %T", err)
	}
	if httpErr.Code != http.StatusUnauthorized {
		t.Fatalf("code = %d, want 401", httpErr.Code)
	}
}
