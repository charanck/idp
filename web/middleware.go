package web

import (
	"context"
	"net/http"
	"net/url"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"controlplane/internal/activity"
	"controlplane/internal/auth"
	"controlplane/internal/ratelimit"
	"controlplane/internal/session"
)

const contextKeyUser = "webui_current_user"

// CurrentUser returns the logged-in user attached by AuthMiddleware.LoadUser, if any.
func CurrentUser(c echo.Context) *auth.User {
	u, _ := c.Get(contextKeyUser).(*auth.User)
	return u
}

// UserLoader resolves the logged-in user for a session, used by
// AuthMiddleware.LoadUser. Satisfied by *auth.AuthService.
type UserLoader interface {
	GetUserByID(ctx context.Context, id uuid.UUID) (*auth.User, error)
}

// AuthMiddleware groups the session/user-aware middleware every route needs:
// loading the current user, requiring login, and requiring staff.
type AuthMiddleware struct {
	users UserLoader
}

func NewAuthMiddleware(users UserLoader) *AuthMiddleware {
	return &AuthMiddleware{users: users}
}

// LoadUser attaches the logged-in user (if any) to the request context from
// the session, without enforcing authentication, so every page - including
// public ones like the login page - can render user-aware nav state.
func (m *AuthMiddleware) LoadUser() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			sess := session.FromContext(c)
			if sess != nil {
				if idStr, ok := sess.UserID(); ok {
					if id, err := uuid.Parse(idStr); err == nil {
						if user, err := m.users.GetUserByID(c.Request().Context(), id); err == nil && user != nil {
							c.Set(contextKeyUser, user)
						}
					}
				}
			}
			return next(c)
		}
	}
}

// LoginRequired mirrors Django's @login_required: redirects to /login/ (with
// ?next=) when there is no authenticated user in session.
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

// AdminRequired mirrors web_ui/views.py's admin_required decorator:
// @login_required plus an is_staff check, flashing an error and redirecting
// to the dashboard when the logged-in user isn't staff.
func (m *AuthMiddleware) AdminRequired() echo.MiddlewareFunc {
	loginRequired := m.LoginRequired()
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return loginRequired(func(c echo.Context) error {
			user := CurrentUser(c)
			if !user.IsStaff {
				AddFlash(c, "danger", "You do not have permission to access this page.")
				return c.Redirect(http.StatusFound, "/dashboard/")
			}
			return next(c)
		})
	}
}

// CSRFProtect ensures a CSRF token exists in session (so GET pages can embed
// it in forms) and validates it on state-changing requests, mirroring
// Django's CsrfViewMiddleware for form-encoded POSTs.
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

// AddFlash queues a one-time message for the next page render, mirroring
// django.contrib.messages. Recognized tags: success, info, warning, danger.
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
