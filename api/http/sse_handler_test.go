package http_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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

// TestStream_FlushesHeadersBeforeFirstEvent guards against a real deadlock
// found via e2e testing: net/http's ResponseWriter buffers the status
// line/headers until enough body data accumulates or Flush is called, so a
// client that waits for the response (e.g. to confirm the stream opened
// before triggering whatever will publish the first event - exactly how a
// real SSE consumer behaves) would hang forever if Stream only flushed once
// an event arrived. Headers must reach the client on connect, independent
// of whether/when any event is ever published.
func TestStream_FlushesHeadersBeforeFirstEvent(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	defer mr.Close()
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	h := apihttp.NewSSEHandler(&fakeSessionValidator{userID: "user-1"}, &fakeSubscriber{rdb: rdb})

	c, rec := newSSERequest("token")
	ctx, cancel := context.WithCancel(context.Background())
	c.SetRequest(c.Request().WithContext(ctx))

	done := make(chan error, 1)
	go func() { done <- h.Stream(c) }()

	deadline := time.Now().Add(2 * time.Second)
	for !rec.Flushed {
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("headers were never flushed to the client before any event was published")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stream did not return after context cancellation")
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
