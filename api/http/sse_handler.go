package http

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"

	"controlplane/internal/notification"
)

// SessionValidator is the narrow slice of *notification.TokenIssuer that
// SSEHandler and InAppHandler need.
type SessionValidator interface {
	Validate(token string) (notification.SessionClaims, error)
}

// Subscriber is the narrow slice of *notification.Hub that SSEHandler needs.
type Subscriber interface {
	Subscribe(ctx context.Context, userID string, applicationID uuid.UUID) *redis.PubSub
}

// SSEHandler streams real-time delivery notices for the bearer token's
// authorized user, fire-and-forget: nothing is replayed for events published
// while nobody was subscribed (see notification.Hub).
type SSEHandler struct {
	validator SessionValidator
	hub       Subscriber
}

func NewSSEHandler(validator SessionValidator, hub Subscriber) *SSEHandler {
	return &SSEHandler{validator: validator, hub: hub}
}

// Stream serves GET /notifications/sse/events.
func (h *SSEHandler) Stream(c echo.Context) error {
	token := bearerToken(c.Request().Header.Get(echo.HeaderAuthorization))
	if token == "" {
		return echo.NewHTTPError(http.StatusUnauthorized, "missing authorization header")
	}
	claims, err := h.validator.Validate(token)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "invalid or expired session")
	}

	w := c.Response()
	w.Header().Set(echo.HeaderContentType, "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	// Go's http.ResponseWriter buffers the status line/headers until enough
	// body data accumulates or Flush is called - without this, a client
	// waiting on the response (e.g. to confirm the stream is open before
	// triggering whatever will publish the first event) never sees the
	// headers, since nothing else writes to w until an event arrives.
	w.Flush()

	ctx := c.Request().Context()
	sub := h.hub.Subscribe(ctx, claims.UserID, claims.ApplicationID)
	defer sub.Close()

	ch := sub.Channel()
	for {
		select {
		case <-ctx.Done():
			return nil
		case msg, ok := <-ch:
			if !ok {
				return nil
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", msg.Payload); err != nil {
				return nil
			}
			w.Flush()
		}
	}
}

var (
	_ SessionValidator = (*notification.TokenIssuer)(nil)
	_ Subscriber       = (*notification.Hub)(nil)
)
