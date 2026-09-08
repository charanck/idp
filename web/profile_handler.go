package web

import (
	"context"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/google/uuid"

	"controlplane/internal/auth"
	authmodel "controlplane/internal/model/auth"
	"controlplane/web/template/pages"
)

// ProfileStore is what the self-service profile page needs. Satisfied by
// *auth.AuthService.
type ProfileStore interface {
	UpdateOwnProfile(ctx context.Context, userID uuid.UUID, in auth.UpdateOwnProfileInput) (*authmodel.User, error)
}

type ProfileHandler struct {
	profile ProfileStore
}

func NewProfileHandler(profile ProfileStore) *ProfileHandler {
	return &ProfileHandler{profile: profile}
}

// Show renders the merged "My Profile" page (GET) and handles updates to the
// self-editable identity fields (POST) - username, first/last name. Email,
// groups, and active status stay admin-only via UpdateUserAdmin.
func (h *ProfileHandler) Show(c echo.Context) error {
	user := CurrentUser(c)

	if c.Request().Method == http.MethodGet {
		return pages.Profile(flashes(c), navUser(c), pages.ProfileData{
			CSRFToken: csrfToken(c),
			Username:  user.Username, FirstName: user.FirstName, LastName: user.LastName, Email: user.Email,
			Password: pages.PasswordChangeData{CSRFToken: csrfToken(c)},
		}).Render(c.Request().Context(), c.Response())
	}

	username := strings.TrimSpace(c.FormValue("username"))
	firstName := strings.TrimSpace(c.FormValue("first_name"))
	lastName := strings.TrimSpace(c.FormValue("last_name"))

	if username == "" {
		return pages.Profile(flashes(c), navUser(c), pages.ProfileData{
			CSRFToken: csrfToken(c),
			Username:  username, FirstName: firstName, LastName: lastName, Email: user.Email,
			ProfileError: "Username is required.",
			Password:     pages.PasswordChangeData{CSRFToken: csrfToken(c)},
		}).Render(c.Request().Context(), c.Response())
	}

	if _, err := h.profile.UpdateOwnProfile(c.Request().Context(), user.ID, auth.UpdateOwnProfileInput{
		Username: username, FirstName: firstName, LastName: lastName,
	}); err != nil {
		return err
	}

	AddFlash(c, "success", "Your profile has been updated.")
	return c.Redirect(http.StatusFound, "/profile/")
}

var _ ProfileStore = (*auth.AuthService)(nil)
