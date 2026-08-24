package notification

import (
	"fmt"
	"net/http"

	"github.com/labstack/echo/v4"
)

// sseEventsHandler streams real-time delivery notices for the bearer token's
// authorized user, fire-and-forget: nothing is replayed for events published
// while nobody was subscribed (see Hub).
func sseEventsHandler(deps *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
		token := bearerToken(c.Request().Header.Get(echo.HeaderAuthorization))
		if token == "" {
			return echo.NewHTTPError(http.StatusUnauthorized, "missing authorization header")
		}
		userID, err := deps.TokenIssuer.Validate(token)
		if err != nil {
			return echo.NewHTTPError(http.StatusUnauthorized, "invalid or expired session")
		}

		w := c.Response()
		w.Header().Set(echo.HeaderContentType, "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)

		ctx := c.Request().Context()
		sub := deps.Hub.Subscribe(ctx, userID)
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
}
