package web

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"golang.org/x/oauth2"

	"controlplane/internal/auth"
	authmodel "controlplane/internal/model/auth"
	"controlplane/internal/session"
)

// OAuthFlow is what the OAuth login/callback handlers need to drive the
// authorization-code flow. Satisfied by *auth.OAuthService.
type OAuthFlow interface {
	GetActiveProviderByID(ctx context.Context, id uuid.UUID) (*authmodel.OAuthProvider, error)
	GetProviderByID(ctx context.Context, id uuid.UUID) (*authmodel.OAuthProvider, error)
	GetAuthorizationURL(provider *authmodel.OAuthProvider, redirectURI string) (authURL, state string, err error)
	ExchangeCodeForToken(ctx context.Context, provider *authmodel.OAuthProvider, code, redirectURI string) (*oauth2.Token, error)
	GetUserInfo(ctx context.Context, provider *authmodel.OAuthProvider, accessToken string) (map[string]any, error)
	AuthenticateOrCreateUser(ctx context.Context, provider *authmodel.OAuthProvider, token *oauth2.Token, userInfo map[string]any) (*authmodel.User, *authmodel.OAuthUserToken, error)
}

type OAuthLoginHandler struct {
	oauth    OAuthFlow
	activity ActivityRecorder
}

func NewOAuthLoginHandler(oauth OAuthFlow, activity ActivityRecorder) *OAuthLoginHandler {
	return &OAuthLoginHandler{oauth: oauth, activity: activity}
}

func redirectURIFor(c echo.Context, providerID string) string {
	scheme := "http"
	// Behind a reverse proxy/load balancer that terminates TLS, the
	// connection to this process is plain HTTP even for HTTPS clients, so
	// c.Request().TLS is always nil in production - trust X-Forwarded-Proto
	// too, same as the session cookie's Secure flag in internal/session.
	if c.Request().TLS != nil || strings.EqualFold(c.Request().Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return scheme + "://" + c.Request().Host + "/oauth/callback/" + providerID + "/"
}

// Login mirrors web_ui/views.py's oauth_login: initiate the
// authorization-code flow, storing the CSRF-style state and provider ID in
// the session for the callback to validate.
func (h *OAuthLoginHandler) Login(c echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound)
	}
	provider, err := h.oauth.GetActiveProviderByID(c.Request().Context(), id)
	if err != nil {
		return err
	}
	if provider == nil {
		return echo.NewHTTPError(http.StatusNotFound)
	}

	redirectURI := redirectURIFor(c, provider.ID.String())
	authURL, state, err := h.oauth.GetAuthorizationURL(provider, redirectURI)
	if err != nil {
		return err
	}

	sess := session.FromContext(c)
	sess.Set("oauth_state_"+provider.ID.String(), state)
	sess.Set("oauth_provider_"+provider.ID.String(), provider.ID.String())

	return c.Redirect(http.StatusFound, authURL)
}

// Callback mirrors web_ui/views.py's oauth_callback: validate state,
// exchange the code, fetch userinfo, authenticate-or-create the user, and
// reject login for inactive users (including newly-auto-created ones).
func (h *OAuthLoginHandler) Callback(c echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound)
	}
	provider, err := h.oauth.GetProviderByID(c.Request().Context(), id)
	if err != nil {
		return err
	}
	if provider == nil {
		return echo.NewHTTPError(http.StatusNotFound)
	}

	if oauthErr := c.QueryParam("error"); oauthErr != "" {
		AddFlash(c, "error", "OAuth error: "+oauthErr)
		return c.Redirect(http.StatusFound, "/login/")
	}

	code := c.QueryParam("code")
	if code == "" {
		AddFlash(c, "error", "No authorization code received.")
		return c.Redirect(http.StatusFound, "/login/")
	}

	state := c.QueryParam("state")
	sess := session.FromContext(c)
	expectedState, ok := sess.PopString("oauth_state_" + provider.ID.String())
	if !ok || expectedState == "" || state == "" || state != expectedState {
		AddFlash(c, "error", "OAuth state validation failed. Please try again.")
		return c.Redirect(http.StatusFound, "/login/")
	}

	ctx := c.Request().Context()
	redirectURI := redirectURIFor(c, provider.ID.String())

	token, err := h.oauth.ExchangeCodeForToken(ctx, provider, code, redirectURI)
	if err != nil {
		AddFlash(c, "error", "OAuth login failed: "+err.Error())
		return c.Redirect(http.StatusFound, "/login/")
	}

	userInfo, err := h.oauth.GetUserInfo(ctx, provider, token.AccessToken)
	if err != nil {
		AddFlash(c, "error", "OAuth login failed: "+err.Error())
		return c.Redirect(http.StatusFound, "/login/")
	}

	user, _, err := h.oauth.AuthenticateOrCreateUser(ctx, provider, token, userInfo)
	if err != nil {
		AddFlash(c, "error", "OAuth login failed: "+err.Error())
		return c.Redirect(http.StatusFound, "/login/")
	}

	if !user.IsActive {
		h.activity.LogLoginFailed(requestContext(c), user.Email, "OAuth login via "+provider.Name+": account not active")
		AddFlash(c, "error", "Your account is pending admin approval. Please contact an administrator.")
		return c.Redirect(http.StatusFound, "/login/")
	}

	sess.Regenerate()
	sess.SetUserID(user.ID.String())
	h.activity.LogLogin(requestContext(c), user.Email, "OAuth login via "+provider.Name)

	AddFlash(c, "success", "Successfully logged in with "+provider.Name+"!")
	return c.Redirect(http.StatusFound, "/dashboard/")
}

var _ OAuthFlow = (*auth.OAuthService)(nil)
