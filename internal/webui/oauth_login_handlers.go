package webui

import (
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"

	"controlplane/internal/auth"
	"controlplane/internal/session"
)

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

// oauthLoginHandler mirrors web_ui/views.py's oauth_login: initiate the
// authorization-code flow, storing the CSRF-style state and provider ID in
// the session for the callback to validate.
func oauthLoginHandler(deps *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			return echo.NewHTTPError(http.StatusNotFound)
		}
		var provider auth.OAuthProvider
		if err := deps.DB.First(&provider, "id = ? AND is_active = ?", id, true).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return echo.NewHTTPError(http.StatusNotFound)
			}
			return err
		}

		redirectURI := redirectURIFor(c, provider.ID.String())
		authURL, state, err := deps.OAuthService.GetAuthorizationURL(&provider, redirectURI)
		if err != nil {
			return err
		}

		sess := session.FromContext(c)
		sess.Set("oauth_state_"+provider.ID.String(), state)
		sess.Set("oauth_provider_"+provider.ID.String(), provider.ID.String())

		return c.Redirect(http.StatusFound, authURL)
	}
}

// oauthCallbackHandler mirrors web_ui/views.py's oauth_callback: validate
// state, exchange the code, fetch userinfo, authenticate-or-create the
// user, and reject login for inactive users (including newly-auto-created
// ones).
func oauthCallbackHandler(deps *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			return echo.NewHTTPError(http.StatusNotFound)
		}
		var provider auth.OAuthProvider
		if err := deps.DB.First(&provider, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return echo.NewHTTPError(http.StatusNotFound)
			}
			return err
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

		token, err := deps.OAuthService.ExchangeCodeForToken(ctx, &provider, code, redirectURI)
		if err != nil {
			AddFlash(c, "error", "OAuth login failed: "+err.Error())
			return c.Redirect(http.StatusFound, "/login/")
		}

		userInfo, err := deps.OAuthService.GetUserInfo(ctx, &provider, token.AccessToken)
		if err != nil {
			AddFlash(c, "error", "OAuth login failed: "+err.Error())
			return c.Redirect(http.StatusFound, "/login/")
		}

		user, _, err := deps.OAuthService.AuthenticateOrCreateUser(&provider, token, userInfo)
		if err != nil {
			AddFlash(c, "error", "OAuth login failed: "+err.Error())
			return c.Redirect(http.StatusFound, "/login/")
		}

		if !user.IsActive {
			deps.Activity.LogLoginFailed(user.Email, requestContext(c), "OAuth login via "+provider.Name+": account not active")
			AddFlash(c, "error", "Your account is pending admin approval. Please contact an administrator.")
			return c.Redirect(http.StatusFound, "/login/")
		}

		sess.Regenerate()
		sess.SetUserID(user.ID.String())
		deps.Activity.LogLogin(user.Email, requestContext(c), "OAuth login via "+provider.Name)

		AddFlash(c, "success", "Successfully logged in with "+provider.Name+"!")
		return c.Redirect(http.StatusFound, "/dashboard/")
	}
}
