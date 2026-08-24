package webui

import (
	"context"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"

	"controlplane/internal/ratelimit"
	"controlplane/internal/security"
	"controlplane/internal/session"
	"controlplane/views/pages"
)

func activeOAuthProviders(ctx context.Context, deps *Deps) ([]pages.LoginOAuthProvider, error) {
	providers, err := deps.OAuthService.ListActiveProviders(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]pages.LoginOAuthProvider, 0, len(providers))
	for _, p := range providers {
		out = append(out, pages.LoginOAuthProvider{ID: p.ID.String(), Name: p.Name})
	}
	return out, nil
}

// homeHandler mirrors web_ui/views.py's home: redirect to the dashboard if
// logged in, else to the login page.
func homeHandler(_ *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
		if CurrentUser(c) != nil {
			return c.Redirect(http.StatusFound, "/dashboard/")
		}
		return c.Redirect(http.StatusFound, "/login/")
	}
}

func loginViewHandler(deps *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
		if CurrentUser(c) != nil {
			return c.Redirect(http.StatusFound, "/dashboard/")
		}
		providers, err := activeOAuthProviders(c.Request().Context(), deps)
		if err != nil {
			return err
		}

		if c.Request().Method == http.MethodGet {
			return pages.Login(flashes(c), pages.LoginData{CSRFToken: csrfToken(c), OAuthProviders: providers}).Render(c.Request().Context(), c.Response())
		}

		email := c.FormValue("username")
		password := c.FormValue("password")

		clientIP := ratelimit.ClientIP(c.Request().Header.Get("X-Forwarded-For"), c.RealIP())
		window := time.Duration(deps.AuthRateLimitWindowSeconds) * time.Second
		limited, err := deps.RateLimiter.IsRateLimited(c.Request().Context(), "web-login", clientIP, deps.AuthRateLimit, window)
		if err != nil {
			return err
		}
		if limited {
			return pages.Login(flashes(c), pages.LoginData{
				CSRFToken: csrfToken(c), Email: email, OAuthProviders: providers,
				Error: "Too many login attempts. Please try again later.",
			}).Render(c.Request().Context(), c.Response())
		}

		user, err := deps.AuthService.AuthenticateUser(c.Request().Context(), email, password)
		if err != nil {
			return err
		}
		if user == nil {
			deps.Activity.LogLoginFailed(requestContext(c), email, "Invalid web login attempt")
			return pages.Login(flashes(c), pages.LoginData{
				CSRFToken: csrfToken(c), Email: email, OAuthProviders: providers,
				Error: "Invalid email or password.",
			}).Render(c.Request().Context(), c.Response())
		}

		sess := session.FromContext(c)
		sess.Regenerate()
		sess.SetUserID(user.ID.String())
		deps.Activity.LogLogin(requestContext(c), user.Email, "Web login")

		if user.ForcePasswordReset {
			AddFlash(c, "warning", "You must change your password before continuing.")
			return c.Redirect(http.StatusFound, "/password/change/")
		}

		next := c.QueryParam("next")
		if next == "" {
			next = "/dashboard/"
		}
		return c.Redirect(http.StatusFound, next)
	}
}

// registerViewHandler mirrors web_ui/views.py's register_view: public
// self-registration is disabled, always bouncing to the login page.
func registerViewHandler(_ *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
		AddFlash(c, "info", "Self-registration is disabled. Contact an administrator to create an account.")
		return c.Redirect(http.StatusFound, "/login/")
	}
}

func logoutViewHandler(deps *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
		if user := CurrentUser(c); user != nil {
			deps.Activity.LogLogout(requestContext(c), user.Email)
		}
		session.FromContext(c).Destroy()
		return c.Redirect(http.StatusFound, "/login/")
	}
}

func passwordChangeViewHandler(deps *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
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

		var errs []string
		if !forceReset && !security.VerifyPassword(oldPassword, user.Password) {
			errs = append(errs, "Current password is incorrect.")
		}
		if newPassword1 != newPassword2 {
			errs = append(errs, "New passwords do not match.")
		}
		if len(newPassword1) < 8 {
			errs = append(errs, "New password must be at least 8 characters.")
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
		if err := deps.AuthService.SetPassword(c.Request().Context(), user.ID, hashed); err != nil {
			return err
		}

		AddFlash(c, "success", "Your password has been changed.")
		return c.Redirect(http.StatusFound, "/dashboard/")
	}
}
