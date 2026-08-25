package notification

import (
	"context"
	"fmt"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"
)

// SessionValidator is the narrow slice of *TokenIssuer that SSEHandler and
// InAppHandler need.
type SessionValidator interface {
	Validate(token string) (userID string, err error)
}

// Subscriber is the narrow slice of *Hub that SSEHandler needs.
type Subscriber interface {
	Subscribe(ctx context.Context, userID string) *redis.PubSub
}

// SSEHandler streams real-time delivery notices for the bearer token's
// authorized user, fire-and-forget: nothing is replayed for events published
// while nobody was subscribed (see Hub).
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
	userID, err := h.validator.Validate(token)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "invalid or expired session")
	}

	w := c.Response()
	w.Header().Set(echo.HeaderContentType, "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	ctx := c.Request().Context()
	sub := h.hub.Subscribe(ctx, userID)
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
	_ SessionValidator = (*TokenIssuer)(nil)
	_ Subscriber       = (*Hub)(nil)
)
