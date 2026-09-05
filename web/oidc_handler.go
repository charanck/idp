package web

import (
	"context"
	"net/http"
	"net/url"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"controlplane/internal/auth"
	authmodel "controlplane/internal/model/auth"
	"controlplane/web/template/pages"
)

// OIDCFlow is what the browser-facing /oauth2/authorize handler needs to
// drive the OIDC authorization-code flow. Satisfied by *auth.OIDCService.
// This is unrelated to OAuthFlow, which handles control-plane's own users
// logging in via an external IdP.
type OIDCFlow interface {
	ValidateClient(ctx context.Context, clientID, redirectURI string) (*authmodel.ServiceClient, error)
	UserAllowedForClient(ctx context.Context, client *authmodel.ServiceClient, userID uuid.UUID) (bool, error)
	IssueAuthorizationCode(ctx context.Context, client *authmodel.ServiceClient, userID uuid.UUID, redirectURI, scope, nonce string) (*authmodel.OIDCAuthorizationCode, error)
}

type OIDCHandler struct {
	oidc     OIDCFlow
	activity ActivityRecorder
}

func NewOIDCHandler(oidc OIDCFlow, activity ActivityRecorder) *OIDCHandler {
	return &OIDCHandler{oidc: oidc, activity: activity}
}

// Authorize implements GET/POST /oauth2/authorize: the browser-facing half
// of the OIDC authorization-code flow. GET starts the flow (validating
// client/redirect_uri, requiring login, checking the user's group
// authorization, and optionally rendering a consent screen); POST handles
// that consent screen's Allow/Deny submission.
func (h *OIDCHandler) Authorize(c echo.Context) error {
	ctx := c.Request().Context()
	isPost := c.Request().Method == http.MethodPost

	clientID := c.QueryParam("client_id")
	redirectURI := c.QueryParam("redirect_uri")
	responseType := c.QueryParam("response_type")
	scope := c.QueryParam("scope")
	state := c.QueryParam("state")
	nonce := c.QueryParam("nonce")
	if isPost {
		clientID = c.FormValue("client_id")
		redirectURI = c.FormValue("redirect_uri")
		responseType = c.FormValue("response_type")
		scope = c.FormValue("scope")
		state = c.FormValue("state")
		nonce = c.FormValue("nonce")
	}
	if scope == "" {
		scope = "openid"
	}

	client, err := h.oidc.ValidateClient(ctx, clientID, redirectURI)
	if err != nil {
		return h.errorPage(c, "This application isn't registered to log in through this identity provider.")
	}

	if responseType != "code" {
		return redirectWithOAuthError(c, redirectURI, state, "unsupported_response_type")
	}

	user := CurrentUser(c)
	if user == nil {
		next := url.QueryEscape(c.Request().URL.RequestURI())
		return c.Redirect(http.StatusFound, "/login/?next="+next)
	}

	allowed, err := h.oidc.UserAllowedForClient(ctx, client, user.ID)
	if err != nil {
		return err
	}
	if !allowed {
		return h.errorPage(c, "Your account isn't authorized to log into this application.")
	}

	if isPost {
		if c.FormValue("decision") != "allow" {
			return redirectWithOAuthError(c, redirectURI, state, "access_denied")
		}
	} else if client.RequireConsent {
		return pages.OIDCConsent(flashes(c), navUser(c), pages.OIDCConsentData{
			CSRFToken:    csrfToken(c),
			ClientName:   client.Name,
			Scope:        scope,
			ClientID:     clientID,
			RedirectURI:  redirectURI,
			ResponseType: responseType,
			State:        state,
			Nonce:        nonce,
		}).Render(ctx, c.Response())
	}

	authCode, err := h.oidc.IssueAuthorizationCode(ctx, client, user.ID, redirectURI, scope, nonce)
	if err != nil {
		return err
	}

	h.activity.LogLogin(requestContext(c), user.Email, "OIDC login via "+client.Name)

	redirectURL, err := url.Parse(redirectURI)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid redirect_uri")
	}
	q := redirectURL.Query()
	q.Set("code", authCode.Code)
	if state != "" {
		q.Set("state", state)
	}
	redirectURL.RawQuery = q.Encode()
	return c.Redirect(http.StatusFound, redirectURL.String())
}

func (h *OIDCHandler) errorPage(c echo.Context, message string) error {
	return pages.OIDCError(flashes(c), navUser(c), message).Render(c.Request().Context(), c.Response())
}

// redirectWithOAuthError redirects back to the relying party with a
// standard OAuth2 error query param, only used once redirectURI itself has
// already been validated against the client's registered redirect URIs.
func redirectWithOAuthError(c echo.Context, redirectURI, state, errorCode string) error {
	redirectURL, err := url.Parse(redirectURI)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, errorCode)
	}
	q := redirectURL.Query()
	q.Set("error", errorCode)
	if state != "" {
		q.Set("state", state)
	}
	redirectURL.RawQuery = q.Encode()
	return c.Redirect(http.StatusFound, redirectURL.String())
}

var _ OIDCFlow = (*auth.OIDCService)(nil)
