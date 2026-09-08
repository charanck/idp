package web

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"controlplane/internal/auth"
	authmodel "controlplane/internal/model/auth"
	"controlplane/internal/ratelimit"
	"controlplane/internal/security"
	"controlplane/internal/session"
	"controlplane/web/template/pages"
)

// AuthStore is what login/password-change handlers need. Satisfied by *auth.AuthService.
type AuthStore interface {
	AuthenticateUser(ctx context.Context, email, password string) (*authmodel.User, error)
	SetPassword(ctx context.Context, userID uuid.UUID, hashedPassword string) error
	GetPolicy(ctx context.Context) (*authmodel.Policy, error)
	UserGroups(ctx context.Context, userID uuid.UUID) ([]authmodel.Group, error)
	GroupApplicationIDs(ctx context.Context, groupID uuid.UUID) ([]uuid.UUID, error)
}

// postLoginLandingPath is where a just-authenticated user should land:
// /dashboard/ if their groups grant the dashboard module, else /profile/
// (a User-tier account has nothing else to see in the web UI).
func postLoginLandingPath(ctx context.Context, store AuthStore, userID uuid.UUID) string {
	groups, err := store.UserGroups(ctx, userID)
	if err != nil {
		return "/profile/"
	}
	appIDs := make(map[uuid.UUID][]uuid.UUID, len(groups))
	for _, g := range groups {
		ids, err := store.GroupApplicationIDs(ctx, g.ID)
		if err != nil {
			return "/profile/"
		}
		appIDs[g.ID] = ids
	}
	if auth.ComputeEffectivePermissions(groups, appIDs).HasModule(auth.ModuleDashboard) {
		return "/dashboard/"
	}
	return "/profile/"
}

// OAuthActiveLister feeds the "log in with..." buttons on the login page.
// Satisfied by *auth.OAuthService.
type OAuthActiveLister interface {
	ListActiveProviders(ctx context.Context) ([]authmodel.OAuthProvider, error)
}

// RateLimiter throttles login attempts. Satisfied by *ratelimit.Limiter.
type RateLimiter interface {
	IsRateLimited(ctx context.Context, key, clientIP string, limit int, window time.Duration) (bool, error)
}

type AuthHandler struct {
	auth                   AuthStore
	oauthProviders         OAuthActiveLister
	limiter                RateLimiter
	activity               ActivityRecorder
	rateLimit              int
	rateLimitWindowSeconds int
}

func NewAuthHandler(auth AuthStore, oauthProviders OAuthActiveLister, limiter RateLimiter, activity ActivityRecorder, rateLimit, rateLimitWindowSeconds int) *AuthHandler {
	return &AuthHandler{
		auth: auth, oauthProviders: oauthProviders, limiter: limiter, activity: activity,
		rateLimit: rateLimit, rateLimitWindowSeconds: rateLimitWindowSeconds,
	}
}

// loginData fills in the deployment's branding (product name/logo/theme)
// onto a LoginData value, so every Login re-render (initial GET, validation
// errors, rate-limit/lockout/SSO rejections) shows the branded login screen.
func (h *AuthHandler) loginData(c echo.Context, in pages.LoginData) pages.LoginData {
	branding := BrandingFromContext(c)
	in.ProductName = branding.ProductName
	in.LogoURL = branding.LogoURL
	in.AccentColor = branding.AccentColor
	in.BackgroundImageURL = branding.BackgroundImageURL
	return in
}

func (h *AuthHandler) activeOAuthProviders(ctx context.Context) ([]pages.LoginOAuthProvider, error) {
	providers, err := h.oauthProviders.ListActiveProviders(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]pages.LoginOAuthProvider, 0, len(providers))
	for _, p := range providers {
		out = append(out, pages.LoginOAuthProvider{ID: p.ID.String(), Name: p.Name})
	}
	return out, nil
}

// Home mirrors web_ui/views.py's home: redirect to the dashboard if logged
// in, else to the login page.
func (h *AuthHandler) Home(c echo.Context) error {
	if user := CurrentUser(c); user != nil {
		return c.Redirect(http.StatusFound, postLoginLandingPath(c.Request().Context(), h.auth, user.ID))
	}
	return c.Redirect(http.StatusFound, "/login/")
}

func (h *AuthHandler) Login(c echo.Context) error {
	if user := CurrentUser(c); user != nil {
		return c.Redirect(http.StatusFound, postLoginLandingPath(c.Request().Context(), h.auth, user.ID))
	}
	providers, err := h.activeOAuthProviders(c.Request().Context())
	if err != nil {
		return err
	}

	if c.Request().Method == http.MethodGet {
		return pages.Login(flashes(c), h.loginData(c, pages.LoginData{CSRFToken: csrfToken(c), OAuthProviders: providers})).Render(c.Request().Context(), c.Response())
	}

	email := c.FormValue("username")
	password := c.FormValue("password")

	clientIP := ratelimit.ClientIP(c.Request().Header.Get("X-Forwarded-For"), c.RealIP())

	policy, err := h.auth.GetPolicy(c.Request().Context())
	if err != nil {
		return err
	}
	if !auth.IPAllowed(policy.LoginIPAllowlist, clientIP) {
		h.activity.LogLoginFailed(requestContext(c), email, "Login rejected: IP not allow-listed")
		return pages.Login(flashes(c), h.loginData(c, pages.LoginData{
			CSRFToken: csrfToken(c), Email: email, OAuthProviders: providers,
			Error: "Login is not permitted from this network.",
		})).Render(c.Request().Context(), c.Response())
	}

	window := time.Duration(h.rateLimitWindowSeconds) * time.Second
	limited, err := h.limiter.IsRateLimited(c.Request().Context(), "web-login", clientIP, h.rateLimit, window)
	if err != nil {
		return err
	}
	if limited {
		return pages.Login(flashes(c), h.loginData(c, pages.LoginData{
			CSRFToken: csrfToken(c), Email: email, OAuthProviders: providers,
			Error: "Too many login attempts. Please try again later.",
		})).Render(c.Request().Context(), c.Response())
	}

	user, err := h.auth.AuthenticateUser(c.Request().Context(), email, password)
	switch {
	case errors.Is(err, auth.ErrAccountLocked):
		h.activity.LogLoginFailed(requestContext(c), email, "Login rejected: account locked")
		return pages.Login(flashes(c), h.loginData(c, pages.LoginData{
			CSRFToken: csrfToken(c), Email: email, OAuthProviders: providers,
			Error: "This account is temporarily locked due to too many failed login attempts. Try again later.",
		})).Render(c.Request().Context(), c.Response())
	case errors.Is(err, auth.ErrPasswordAuthDisabled):
		h.activity.LogLoginFailed(requestContext(c), email, "Login rejected: SSO-only policy")
		return pages.Login(flashes(c), h.loginData(c, pages.LoginData{
			CSRFToken: csrfToken(c), Email: email, OAuthProviders: providers,
			Error: "Password login is disabled. Please sign in with SSO.",
		})).Render(c.Request().Context(), c.Response())
	case err != nil:
		return err
	}
	if user == nil {
		h.activity.LogLoginFailed(requestContext(c), email, "Invalid web login attempt")
		return pages.Login(flashes(c), h.loginData(c, pages.LoginData{
			CSRFToken: csrfToken(c), Email: email, OAuthProviders: providers,
			Error: "Invalid email or password.",
		})).Render(c.Request().Context(), c.Response())
	}

	sess := session.FromContext(c)
	sess.Regenerate()
	sess.SetUserID(user.ID.String())
	h.activity.LogLogin(requestContext(c), user.Email, "Web login")

	if user.ForcePasswordReset {
		AddFlash(c, "warning", "You must change your password before continuing.")
		return c.Redirect(http.StatusFound, "/password/change/")
	}
	if auth.IsPasswordExpired(policy, user.PasswordChangedAt, time.Now().UTC()) {
		AddFlash(c, "warning", "Your password has expired. Please choose a new one.")
		return c.Redirect(http.StatusFound, "/password/change/")
	}

	next := c.QueryParam("next")
	if next == "" {
		next = postLoginLandingPath(c.Request().Context(), h.auth, user.ID)
	}
	return c.Redirect(http.StatusFound, next)
}

// Register mirrors web_ui/views.py's register_view: public self-registration
// is disabled, always bouncing to the login page.
func (h *AuthHandler) Register(c echo.Context) error {
	AddFlash(c, "info", "Self-registration is disabled. Contact an administrator to create an account.")
	return c.Redirect(http.StatusFound, "/login/")
}

func (h *AuthHandler) Logout(c echo.Context) error {
	if user := CurrentUser(c); user != nil {
		h.activity.LogLogout(requestContext(c), user.Email)
	}
	session.FromContext(c).Destroy()
	return c.Redirect(http.StatusFound, "/login/")
}

func (h *AuthHandler) PasswordChange(c echo.Context) error {
	user := CurrentUser(c)
	forceReset := user.ForcePasswordReset

	if c.Request().Method == http.MethodGet {
		return pages.PasswordChange(flashes(c), navUser(c), pages.PasswordChangeData{
			CSRFToken: csrfToken(c), ForceReset: forceReset,
		}).Render(c.Request().Context(), c.Response())
	}

	oldPassword := c.FormValue("old_password")
	newPassword1 := c.FormValue("new_password1")
	newPassword2 := c.FormValue("new_password2")

	policy, err := h.auth.GetPolicy(c.Request().Context())
	if err != nil {
		return err
	}

	var errs []string
	if !forceReset && !security.VerifyPassword(oldPassword, user.Password) {
		errs = append(errs, "Current password is incorrect.")
	}
	if newPassword1 != newPassword2 {
		errs = append(errs, "New passwords do not match.")
	}
	if err := auth.ValidatePasswordAgainstPolicy(policy, newPassword1); err != nil {
		errs = append(errs, err.Error())
	}

	if len(errs) > 0 {
		return pages.PasswordChange(flashes(c), navUser(c), pages.PasswordChangeData{
			CSRFToken: csrfToken(c), ForceReset: forceReset, Errors: errs,
		}).Render(c.Request().Context(), c.Response())
	}

	hashed, err := security.HashPassword(newPassword1)
	if err != nil {
		return err
	}
	if err := h.auth.SetPassword(c.Request().Context(), user.ID, hashed); err != nil {
		return err
	}

	AddFlash(c, "success", "Your password has been changed.")
	return c.Redirect(http.StatusFound, "/profile/")
}

var (
	_ AuthStore         = (*auth.AuthService)(nil)
	_ OAuthActiveLister = (*auth.OAuthService)(nil)
	_ RateLimiter       = (*ratelimit.Limiter)(nil)
)
