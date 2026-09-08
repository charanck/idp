package web

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"controlplane/internal/activity"
	"controlplane/internal/auth"
	authmodel "controlplane/internal/model/auth"
	"controlplane/internal/ratelimit"
	"controlplane/internal/session"
)

const (
	contextKeyUser     = "webui_current_user"
	contextKeyBranding = "webui_branding"
)

// CurrentUser returns the logged-in user attached by AuthMiddleware.LoadUser, if any.
func CurrentUser(c echo.Context) *authmodel.User {
	u, _ := c.Get(contextKeyUser).(*authmodel.User)
	return u
}

// BrandingFromContext returns the deployment's branding settings attached by
// AuthMiddleware.LoadUser, or a zero-value Branding (falling back to
// defaults) if unavailable.
func BrandingFromContext(c echo.Context) authmodel.Branding {
	b, _ := c.Get(contextKeyBranding).(authmodel.Branding)
	return b
}

// UserLoader resolves the logged-in user for a session, used by
// AuthMiddleware.LoadUser. Satisfied by *auth.AuthService.
type UserLoader interface {
	GetUserByID(ctx context.Context, id uuid.UUID) (*authmodel.User, error)
}

// PolicyLoader is what AuthMiddleware needs to enforce the session
// idle-timeout policy. Satisfied by *auth.AuthService.
type PolicyLoader interface {
	GetPolicy(ctx context.Context) (*authmodel.Policy, error)
}

// BrandingLoader is what AuthMiddleware needs to attach the deployment's
// login/product branding to every request. Satisfied by *auth.AuthService.
type BrandingLoader interface {
	GetBranding(ctx context.Context) (*authmodel.Branding, error)
}

// AuthMiddleware groups the session/user-aware middleware every route needs:
// loading the current user (plus their computed group permissions) and
// requiring login.
type AuthMiddleware struct {
	users    UserLoader
	groups   GroupPermissionLoader
	policies PolicyLoader
	branding BrandingLoader
}

func NewAuthMiddleware(users UserLoader, groups GroupPermissionLoader, policies PolicyLoader, branding BrandingLoader) *AuthMiddleware {
	return &AuthMiddleware{users: users, groups: groups, policies: policies, branding: branding}
}

// LoadUser attaches the logged-in user (if any), and their computed
// EffectivePermissions, to the request context from the session, without
// enforcing authentication, so every page - including public ones like the
// login page - can render user-aware nav state. A session that has exceeded
// the policy's idle timeout is destroyed instead, so the request proceeds
// as anonymous and LoginRequired bounces it to /login/. It also attaches the
// deployment's branding settings, needed even on the anonymous login page.
func (m *AuthMiddleware) LoadUser() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if branding, err := m.branding.GetBranding(c.Request().Context()); err == nil && branding != nil {
				c.Set(contextKeyBranding, *branding)
			}
			sess := session.FromContext(c)
			if sess != nil {
				if idStr, ok := sess.UserID(); ok {
					if id, err := uuid.Parse(idStr); err == nil {
						policy, err := m.policies.GetPolicy(c.Request().Context())
						if err == nil && sess.IdleTimedOut(policy.SessionIdleTimeoutMinutes) {
							slog.DebugContext(c.Request().Context(), "session destroyed: idle timeout exceeded", "user_id", id, "idle_timeout_minutes", policy.SessionIdleTimeoutMinutes)
							sess.Destroy()
						} else if user, err := m.users.GetUserByID(c.Request().Context(), id); err == nil && user != nil {
							c.Set(contextKeyUser, user)
							c.Set(contextKeyPermissions, m.computePermissions(c.Request().Context(), user.ID))
							sess.Touch()
						}
					}
				}
			}
			return next(c)
		}
	}
}

// computePermissions unions the user's groups' module permissions and
// Application allow-lists. Errors loading groups degrade to "no
// permissions" rather than failing the request - a transient lookup failure
// should deny access, not grant it.
func (m *AuthMiddleware) computePermissions(ctx context.Context, userID uuid.UUID) auth.EffectivePermissions {
	groups, err := m.groups.UserGroups(ctx, userID)
	if err != nil {
		return auth.EffectivePermissions{}
	}
	appIDs := make(map[uuid.UUID][]uuid.UUID, len(groups))
	for _, g := range groups {
		ids, err := m.groups.GroupApplicationIDs(ctx, g.ID)
		if err != nil {
			return auth.EffectivePermissions{}
		}
		appIDs[g.ID] = ids
	}
	return auth.ComputeEffectivePermissions(groups, appIDs)
}

// LoginRequired redirects to /login/ (with ?next=) when there is no
// authenticated user in session.
func (m *AuthMiddleware) LoginRequired() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if CurrentUser(c) == nil {
				next := url.QueryEscape(c.Request().URL.RequestURI())
				return c.Redirect(http.StatusFound, "/login/?next="+next)
			}
			return next(c)
		}
	}
}

// CSRFProtect ensures a CSRF token exists in session (so GET pages can embed
// it in forms) and validates it on state-changing, form-encoded POSTs.
func CSRFProtect() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			sess := session.FromContext(c)
			token := sess.CSRFToken()

			switch c.Request().Method {
			case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
				submitted := c.FormValue("csrf_token")
				if submitted == "" || submitted != token {
					return echo.NewHTTPError(http.StatusForbidden, "CSRF verification failed. Request aborted.")
				}
			}
			return next(c)
		}
	}
}

// AddFlash queues a one-time message for the next page render. Recognized
// tags: success, info, warning, danger.
func AddFlash(c echo.Context, tag, text string) {
	if sess := session.FromContext(c); sess != nil {
		sess.AddFlash(tag, text)
	}
}

// requestContext returns the request's context.Context carrying the
// request-derived fields (client IP, current user email) that the activity
// logger attaches to every row, mirroring log_activity's request.user.email
// fallback.
func requestContext(c echo.Context) context.Context {
	info := activity.RequestInfo{
		IPAddress: ratelimit.ClientIP(c.Request().Header.Get("X-Forwarded-For"), c.RealIP()),
	}
	if user := CurrentUser(c); user != nil {
		info.UserEmail = user.Email
	}
	return activity.WithRequestInfo(c.Request().Context(), info)
}

var _ UserLoader = (*auth.AuthService)(nil)
var _ BrandingLoader = (*auth.AuthService)(nil)
