package web

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"controlplane/internal/auth"
	authmodel "controlplane/internal/model/auth"
	"controlplane/web/template/pages"
)

const usersPageSize = 20

// UserStore is what user CRUD handlers need. Satisfied by *auth.AuthService.
type UserStore interface {
	ListUsers(ctx context.Context, q string, isStaff *bool) ([]authmodel.User, error)
	CreateUserAdmin(ctx context.Context, in auth.CreateUserAdminInput) (*authmodel.User, error)
	GetUserByIDAny(ctx context.Context, id uuid.UUID) (*authmodel.User, error)
	UpdateUserAdmin(ctx context.Context, id uuid.UUID, in auth.UpdateUserAdminInput) (*authmodel.User, error)
	DeleteUser(ctx context.Context, id uuid.UUID) (*authmodel.User, error)
	ListGroups(ctx context.Context, q string) ([]authmodel.Group, error)
	UserGroups(ctx context.Context, userID uuid.UUID) ([]authmodel.Group, error)
	SetUserGroups(ctx context.Context, userID uuid.UUID, groupIDs []uuid.UUID) error
}

type UserHandler struct {
	users    UserStore
	activity ActivityRecorder
}

func NewUserHandler(users UserStore, activity ActivityRecorder) *UserHandler {
	return &UserHandler{users: users, activity: activity}
}

func (h *UserHandler) List(c echo.Context) error {
	ctx := c.Request().Context()
	q := strings.TrimSpace(c.QueryParam("q"))
	groupFilter := strings.TrimSpace(c.QueryParam("group"))

	users, err := h.users.ListUsers(ctx, q, nil)
	if err != nil {
		return err
	}
	groups, err := h.users.ListGroups(ctx, "")
	if err != nil {
		return err
	}

	rows := make([]pages.UserRow, 0, len(users))
	for _, u := range users {
		userGroups, err := h.users.UserGroups(ctx, u.ID)
		if err != nil {
			return err
		}
		if groupFilter != "" && !containsGroupID(userGroups, groupFilter) {
			continue
		}
		rows = append(rows, pages.UserRow{User: u, Groups: userGroups})
	}

	extra := url.Values{}
	if q != "" {
		extra.Set("q", q)
	}
	if groupFilter != "" {
		extra.Set("group", groupFilter)
	}

	page := Paginate(rows, usersPageSize, PageParam(c))
	return pages.UsersList(flashes(c), navUser(c), pages.UsersListData{
		Users: page.Items, Groups: groups, CurrentQ: q, CurrentGroup: groupFilter, ExtraQuery: extra.Encode(),
		Page: page.Number, NumPages: page.NumPages,
		HasPrev: page.HasPrevious, HasNext: page.HasNext,
		PrevNum: page.PreviousNumber, NextNum: page.NextNumber,
		Window: page.PageRange(),
	}).Render(c.Request().Context(), c.Response())
}

func containsGroupID(groups []authmodel.Group, id string) bool {
	for _, g := range groups {
		if g.ID.String() == id {
			return true
		}
	}
	return false
}

// userGroupIDsFromForm reads the group_ids multi-value form field, requiring
// c.Request().ParseForm() to have already been called.
func userGroupIDsFromForm(c echo.Context) []uuid.UUID {
	var ids []uuid.UUID
	for _, idStr := range c.Request().Form["group_ids"] {
		if id, err := uuid.Parse(idStr); err == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

// defaultUserGroupIDs returns the built-in User group's ID (pre-checked on
// the Create form, mirroring the default group new users are assigned to).
func defaultUserGroupIDs(groups []authmodel.Group) map[string]bool {
	for _, g := range groups {
		if g.IsSystem && g.Name == "User" {
			return map[string]bool{g.ID.String(): true}
		}
	}
	return map[string]bool{}
}

func (h *UserHandler) Create(c echo.Context) error {
	groups, err := h.users.ListGroups(c.Request().Context(), "")
	if err != nil {
		return err
	}

	if c.Request().Method == http.MethodGet {
		return pages.UserForm(flashes(c), navUser(c), pages.UserFormData{
			CSRFToken: csrfToken(c), Action: "/users/create/", Title: "Create User",
			Groups: groups, SelectedGroupIDs: defaultUserGroupIDs(groups),
		}).Render(c.Request().Context(), c.Response())
	}

	if err := c.Request().ParseForm(); err != nil {
		return err
	}
	groupIDs := userGroupIDsFromForm(c)

	reRender := func(errMsg string) error {
		return pages.UserForm(flashes(c), navUser(c), pages.UserFormData{
			CSRFToken: csrfToken(c), Action: "/users/create/", Title: "Create User",
			Email: c.FormValue("email"), Username: c.FormValue("username"),
			IsActive: c.FormValue("is_active") != "", Error: errMsg,
			Groups: groups, SelectedGroupIDs: selectedSet(groupIDs),
		}).Render(c.Request().Context(), c.Response())
	}

	email := strings.TrimSpace(c.FormValue("email"))
	username := strings.TrimSpace(c.FormValue("username"))
	password1 := c.FormValue("password1")
	password2 := c.FormValue("password2")

	if email == "" || username == "" {
		return reRender("Email and username are required.")
	}
	if password1 != password2 {
		return reRender("Passwords do not match.")
	}
	if len(password1) < 8 {
		return reRender("Password must be at least 8 characters long.")
	}

	newUser, err := h.users.CreateUserAdmin(c.Request().Context(), auth.CreateUserAdminInput{
		Email: email, Username: username, Password: password1, IsActive: c.FormValue("is_active") != "",
	})
	if err != nil {
		if errors.Is(err, auth.ErrAlreadyExists) {
			return reRender("A user with that email already exists.")
		}
		return err
	}
	if err := h.users.SetUserGroups(c.Request().Context(), newUser.ID, groupIDs); err != nil {
		return err
	}

	h.activity.LogCreate(requestContext(c), "user", newUser.ID.String(), newUser.Email, nil)
	AddFlash(c, "success", "User "+newUser.Email+" created successfully.")
	return c.Redirect(http.StatusFound, "/users/")
}

// Edit mirrors web_ui/views.py's user_edit: if an admin edits their own
// account and tries to change their active status or group membership, those
// changes are silently reverted (role changes must come from another admin).
func (h *UserHandler) Edit(c echo.Context) error {
	ctx := c.Request().Context()
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound)
	}
	target, err := h.users.GetUserByIDAny(ctx, id)
	if err != nil {
		return err
	}
	if target == nil {
		return echo.NewHTTPError(http.StatusNotFound)
	}

	current := CurrentUser(c)
	editingSelf := current != nil && current.ID == target.ID

	groups, err := h.users.ListGroups(ctx, "")
	if err != nil {
		return err
	}
	targetGroups, err := h.users.UserGroups(ctx, target.ID)
	if err != nil {
		return err
	}
	targetGroupIDs := make([]uuid.UUID, 0, len(targetGroups))
	for _, g := range targetGroups {
		targetGroupIDs = append(targetGroupIDs, g.ID)
	}

	if c.Request().Method == http.MethodGet {
		return pages.UserForm(flashes(c), navUser(c), pages.UserFormData{
			CSRFToken: csrfToken(c), Action: "/users/" + target.ID.String() + "/edit/", Title: "Edit User",
			Email: target.Email, Username: target.Username, IsActive: target.IsActive,
			IsEdit: true, EditingSelf: editingSelf,
			Groups: groups, SelectedGroupIDs: selectedSet(targetGroupIDs),
		}).Render(ctx, c.Response())
	}

	if err := c.Request().ParseForm(); err != nil {
		return err
	}
	groupIDs := userGroupIDsFromForm(c)

	reRender := func(errMsg string) error {
		return pages.UserForm(flashes(c), navUser(c), pages.UserFormData{
			CSRFToken: csrfToken(c), Action: "/users/" + target.ID.String() + "/edit/", Title: "Edit User",
			Email: c.FormValue("email"), Username: c.FormValue("username"),
			IsActive: c.FormValue("is_active") != "",
			IsEdit: true, EditingSelf: editingSelf, Error: errMsg,
			Groups: groups, SelectedGroupIDs: selectedSet(groupIDs),
		}).Render(ctx, c.Response())
	}

	email := strings.TrimSpace(c.FormValue("email"))
	username := strings.TrimSpace(c.FormValue("username"))
	if email == "" || username == "" {
		return reRender("Email and username are required.")
	}

	newIsActive := c.FormValue("is_active") != ""

	if editingSelf && newIsActive != target.IsActive {
		newIsActive = target.IsActive
		AddFlash(c, "warning", "You cannot change your own active status. Ask another admin to do it.")
	}
	if editingSelf && !sameGroupSet(groupIDs, targetGroupIDs) {
		groupIDs = targetGroupIDs
		AddFlash(c, "warning", "You cannot change your own group membership. Ask another admin to do it.")
	}

	updated, err := h.users.UpdateUserAdmin(ctx, id, auth.UpdateUserAdminInput{
		Email: email, Username: username, IsActive: newIsActive, IsStaff: target.IsStaff,
	})
	if err != nil {
		return err
	}
	if err := h.users.SetUserGroups(ctx, updated.ID, groupIDs); err != nil {
		return err
	}

	h.activity.LogUpdate(requestContext(c), "user", updated.ID.String(), updated.Email, nil)
	AddFlash(c, "success", "User "+updated.Email+" updated successfully.")
	return c.Redirect(http.StatusFound, "/users/")
}

func sameGroupSet(a, b []uuid.UUID) bool {
	if len(a) != len(b) {
		return false
	}
	set := make(map[uuid.UUID]bool, len(a))
	for _, id := range a {
		set[id] = true
	}
	for _, id := range b {
		if !set[id] {
			return false
		}
	}
	return true
}

func (h *UserHandler) Delete(c echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound)
	}
	target, err := h.users.GetUserByIDAny(c.Request().Context(), id)
	if err != nil {
		return err
	}
	if target == nil {
		return echo.NewHTTPError(http.StatusNotFound)
	}

	if c.Request().Method == http.MethodGet {
		return pages.ConfirmDelete(flashes(c), navUser(c), "users", pages.ConfirmDeleteData{
			CSRFToken: csrfToken(c), Title: "Delete User",
			Message:    "Are you sure you want to delete \"" + target.Email + "\"?",
			Action:     "/users/" + target.ID.String() + "/delete/",
			CancelHref: "/users/",
		}).Render(c.Request().Context(), c.Response())
	}

	current := CurrentUser(c)
	if current != nil && current.ID == target.ID {
		AddFlash(c, "error", "You cannot delete your own account.")
		return c.Redirect(http.StatusFound, "/users/")
	}

	if _, err := h.users.DeleteUser(c.Request().Context(), id); err != nil {
		return err
	}

	h.activity.LogDelete(requestContext(c), "user", target.ID.String(), target.Email, nil)
	AddFlash(c, "success", "User "+target.Email+" deleted successfully.")
	return c.Redirect(http.StatusFound, "/users/")
}

var _ UserStore = (*auth.AuthService)(nil)
