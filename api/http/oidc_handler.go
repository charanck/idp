package http

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"

	"controlplane/internal/auth"
)

// OIDCProvider is the narrow slice of *auth.OIDCService the stateless
// discovery/token/userinfo endpoints need. Satisfied by *auth.OIDCService.
type OIDCProvider interface {
	JWKS(ctx context.Context) (map[string]any, error)
	ExchangeCode(ctx context.Context, clientID, clientSecret, code, redirectURI, issuer string) (idToken, accessToken string, expiresIn int, err error)
	ValidateAccessToken(ctx context.Context, tokenString string) (map[string]any, error)
}

// OIDCHandler serves the stateless, machine-facing half of the OIDC
// Identity Provider: discovery document, JWKS, token exchange, and
// userinfo. The browser-facing /oauth2/authorize endpoint lives in
// web.OIDCHandler instead, since it needs session auth + CSRF.
type OIDCHandler struct {
	oidc OIDCProvider
}

func NewOIDCHandler(oidc OIDCProvider) *OIDCHandler {
	return &OIDCHandler{oidc: oidc}
}

// issuerFrom derives this control-plane's own base URL (scheme + host) from
// the incoming request, matching web/oauth_login_handler.go's
// redirectURIFor - there's no fixed public-URL config to read instead.
func issuerFrom(c echo.Context) string {
	scheme := "http"
	if c.Request().TLS != nil || strings.EqualFold(c.Request().Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return scheme + "://" + c.Request().Host
}

// Discovery serves GET /.well-known/openid-configuration.
func (h *OIDCHandler) Discovery(c echo.Context) error {
	issuer := issuerFrom(c)
	return c.JSON(http.StatusOK, map[string]any{
		"issuer":                                issuer,
		"authorization_endpoint":                issuer + "/oauth2/authorize",
		"token_endpoint":                        issuer + "/oauth2/token",
		"userinfo_endpoint":                     issuer + "/oauth2/userinfo",
		"jwks_uri":                              issuer + "/.well-known/jwks.json",
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"scopes_supported":                      []string{"openid", "profile", "email", "groups"},
		"token_endpoint_auth_methods_supported": []string{"client_secret_basic", "client_secret_post"},
		"grant_types_supported":                 []string{"authorization_code"},
	})
}

// JWKS serves GET /.well-known/jwks.json.
func (h *OIDCHandler) JWKS(c echo.Context) error {
	jwks, err := h.oidc.JWKS(c.Request().Context())
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to build JWKS")
	}
	return c.JSON(http.StatusOK, jwks)
}

// Token serves POST /oauth2/token: grant_type=authorization_code only.
// client_id/client_secret are read from HTTP Basic auth if present,
// otherwise from the form body, per standard OAuth2 client authentication.
func (h *OIDCHandler) Token(c echo.Context) error {
	if c.FormValue("grant_type") != "authorization_code" {
		return oauthError(c, http.StatusBadRequest, "unsupported_grant_type")
	}

	clientID, clientSecret := c.FormValue("client_id"), c.FormValue("client_secret")
	if basicID, basicSecret, ok := c.Request().BasicAuth(); ok {
		clientID, clientSecret = basicID, basicSecret
	}

	idToken, accessToken, expiresIn, err := h.oidc.ExchangeCode(
		c.Request().Context(), clientID, clientSecret,
		c.FormValue("code"), c.FormValue("redirect_uri"), issuerFrom(c),
	)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrOIDCClientAuthFailed):
			return oauthError(c, http.StatusUnauthorized, "invalid_client")
		case errors.Is(err, auth.ErrOIDCCodeInvalid):
			return oauthError(c, http.StatusBadRequest, "invalid_grant")
		default:
			return oauthError(c, http.StatusInternalServerError, "server_error")
		}
	}

	return c.JSON(http.StatusOK, map[string]any{
		"access_token": accessToken,
		"id_token":     idToken,
		"token_type":   "Bearer",
		"expires_in":   expiresIn,
	})
}

// UserInfo serves GET /oauth2/userinfo: Authorization: Bearer <access_token>.
func (h *OIDCHandler) UserInfo(c echo.Context) error {
	authHeader := c.Request().Header.Get("Authorization")
	token, ok := strings.CutPrefix(authHeader, "Bearer ")
	if !ok || token == "" {
		return oauthError(c, http.StatusUnauthorized, "invalid_token")
	}

	claims, err := h.oidc.ValidateAccessToken(c.Request().Context(), token)
	if err != nil {
		return oauthError(c, http.StatusUnauthorized, "invalid_token")
	}
	return c.JSON(http.StatusOK, claims)
}

func oauthError(c echo.Context, status int, code string) error {
	return c.JSON(status, map[string]string{"error": code})
}

var _ OIDCProvider = (*auth.OIDCService)(nil)
