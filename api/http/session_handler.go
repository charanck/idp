package http

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	configmodel "controlplane/internal/model/config"
	"controlplane/internal/notification"
)

type createSessionRequest struct {
	UserID  string `json:"user_id"`
	Service string `json:"service"`
}

type createSessionResponse struct {
	Token     string `json:"token"`
	ExpiresIn int    `json:"expires_in_seconds"`
}

// SessionIssuer is the narrow slice of *notification.TokenIssuer that
// SessionHandler needs.
type SessionIssuer interface {
	Issue(userID string, applicationID uuid.UUID) (string, error)
}

// ApplicationResolver is the narrow slice of configmodel.ApplicationRepository
// that SessionHandler needs to scope a minted token to the calling service
// client's application, the same get-or-create resolution CreateNotification
// uses for its own "service" field.
type ApplicationResolver interface {
	GetOrCreate(ctx context.Context, name string) (*configmodel.Application, error)
}

// SessionHandler mints short-lived bearer tokens authorizing their caller to
// act as a given userID, scoped to a given application, against the
// endpoints that don't accept an X-API-Key: streaming their SSE events
// (SSEHandler) and listing/consuming their InApp unread inbox
// (InAppHandler).
type SessionHandler struct {
	issuer SessionIssuer
	apps   ApplicationResolver
}

func NewSessionHandler(issuer SessionIssuer, apps ApplicationResolver) *SessionHandler {
	return &SessionHandler{issuer: issuer, apps: apps}
}

// Create serves POST /notifications/sessions.
func (h *SessionHandler) Create(c echo.Context) error {
	var req createSessionRequest
	if err := c.Bind(&req); err != nil || req.UserID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "user_id is required")
	}
	if req.Service == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "service is required")
	}

	app, err := h.apps.GetOrCreate(c.Request().Context(), req.Service)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to resolve service")
	}

	token, err := h.issuer.Issue(req.UserID, app.ID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to mint notification session")
	}
	return c.JSON(http.StatusOK, createSessionResponse{
		Token:     token,
		ExpiresIn: int(notification.SessionTokenTTL.Seconds()),
	})
}

// bearerToken extracts the token from an "Authorization: Bearer <token>"
// header value, or "" if the header is missing or malformed.
func bearerToken(header string) string {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return ""
	}
	return strings.TrimPrefix(header, prefix)
}

var _ SessionIssuer = (*notification.TokenIssuer)(nil)
