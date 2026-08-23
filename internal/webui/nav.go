package webui

import (
	"github.com/labstack/echo/v4"

	"controlplane/internal/session"
	"controlplane/views/layout"
)

// navUser builds the layout.NavUser view for the current request, mirroring
// how base.html's context processors expose request.user everywhere.
func navUser(c echo.Context) layout.NavUser {
	sess := session.FromContext(c)
	user := CurrentUser(c)
	if user == nil {
		return layout.NavUser{CSRFToken: sess.CSRFToken()}
	}
	return layout.NavUser{
		LoggedIn:  true,
		Email:     user.Email,
		IsStaff:   user.IsStaff,
		CSRFToken: sess.CSRFToken(),
	}
}

func flashes(c echo.Context) []session.Flash {
	return session.FromContext(c).PopFlashes()
}

func csrfToken(c echo.Context) string {
	return session.FromContext(c).CSRFToken()
}
