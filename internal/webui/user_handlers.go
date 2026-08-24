package webui

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"controlplane/internal/auth"
	"controlplane/views/pages"
)

const usersPageSize = 20

func usersListHandler(deps *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
		q := strings.TrimSpace(c.QueryParam("q"))
		staffFilter := c.QueryParam("staff")

		var isStaff *bool
		switch staffFilter {
		case "yes":
			v := true
			isStaff = &v
		case "no":
			v := false
			isStaff = &v
		}

		users, err := deps.AuthService.ListUsers(c.Request().Context(), q, isStaff)
		if err != nil {
			return err
		}

		extra := url.Values{}
		if q != "" {
			extra.Set("q", q)
		}
		if staffFilter != "" {
			extra.Set("staff", staffFilter)
		}

		page := Paginate(users, usersPageSize, PageParam(c))
		return pages.UsersList(flashes(c), navUser(c), pages.UsersListData{
			Users: page.Items, CurrentQ: q, CurrentStaff: staffFilter, ExtraQuery: extra.Encode(),
			Page: page.Number, NumPages: page.NumPages,
			HasPrev: page.HasPrevious, HasNext: page.HasNext,
			PrevNum: page.PreviousNumber, NextNum: page.NextNumber,
			Window: page.PageRange(),
		}).Render(c.Request().Context(), c.Response())
	}
}

func userCreateHandler(deps *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
		if c.Request().Method == http.MethodGet {
			return pages.UserForm(flashes(c), navUser(c), pages.UserFormData{
				CSRFToken: csrfToken(c), Action: "/users/create/", Title: "Create User",
			}).Render(c.Request().Context(), c.Response())
		}

		reRender := func(errMsg string) error {
			return pages.UserForm(flashes(c), navUser(c), pages.UserFormData{
				CSRFToken: csrfToken(c), Action: "/users/create/", Title: "Create User",
				Email: c.FormValue("email"), Username: c.FormValue("username"),
				IsActive: c.FormValue("is_active") != "", Error: errMsg,
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

		newUser, err := deps.AuthService.CreateUserAdmin(c.Request().Context(), auth.CreateUserAdminInput{
			Email: email, Username: username, Password: password1, IsActive: c.FormValue("is_active") != "",
		})
		if err != nil {
			if errors.Is(err, auth.ErrAlreadyExists) {
				return reRender("A user with that email already exists.")
			}
			return err
		}

		deps.Activity.LogCreate(requestContext(c), "user", newUser.ID.String(), newUser.Email, nil)
		AddFlash(c, "success", "User "+newUser.Email+" created successfully.")
		return c.Redirect(http.StatusFound, "/users/")
	}
}

// userEditHandler mirrors web_ui/views.py's user_edit: if an admin edits
// their own account and tries to change is_staff/is_active, those two
// fields are silently reverted (role changes must come from another admin).
func userEditHandler(deps *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			return echo.NewHTTPError(http.StatusNotFound)
		}
		target, err := deps.AuthService.GetUserByIDAny(c.Request().Context(), id)
		if err != nil {
			return err
		}
		if target == nil {
			return echo.NewHTTPError(http.StatusNotFound)
		}

		current := CurrentUser(c)
		editingSelf := current != nil && current.ID == target.ID

		if c.Request().Method == http.MethodGet {
			return pages.UserForm(flashes(c), navUser(c), pages.UserFormData{
				CSRFToken: csrfToken(c), Action: "/users/" + target.ID.String() + "/edit/", Title: "Edit User",
				Email: target.Email, Username: target.Username, IsActive: target.IsActive, IsStaff: target.IsStaff,
				IsEdit: true, EditingSelf: editingSelf,
			}).Render(c.Request().Context(), c.Response())
		}

		reRender := func(errMsg string) error {
			return pages.UserForm(flashes(c), navUser(c), pages.UserFormData{
				CSRFToken: csrfToken(c), Action: "/users/" + target.ID.String() + "/edit/", Title: "Edit User",
				Email: c.FormValue("email"), Username: c.FormValue("username"),
				IsActive: c.FormValue("is_active") != "", IsStaff: c.FormValue("is_staff") != "",
				IsEdit: true, EditingSelf: editingSelf, Error: errMsg,
			}).Render(c.Request().Context(), c.Response())
		}

		email := strings.TrimSpace(c.FormValue("email"))
		username := strings.TrimSpace(c.FormValue("username"))
		if email == "" || username == "" {
			return reRender("Email and username are required.")
		}

		newIsActive := c.FormValue("is_active") != ""
		newIsStaff := c.FormValue("is_staff") != ""

		if editingSelf && (newIsActive != target.IsActive || newIsStaff != target.IsStaff) {
			newIsActive = target.IsActive
			newIsStaff = target.IsStaff
			AddFlash(c, "warning", "You cannot change your own role or active status. Ask another admin to do it.")
		}

		updated, err := deps.AuthService.UpdateUserAdmin(c.Request().Context(), id, auth.UpdateUserAdminInput{
			Email: email, Username: username, IsActive: newIsActive, IsStaff: newIsStaff,
		})
		if err != nil {
			return err
		}

		deps.Activity.LogUpdate(requestContext(c), "user", updated.ID.String(), updated.Email, nil)
		AddFlash(c, "success", "User "+updated.Email+" updated successfully.")
		return c.Redirect(http.StatusFound, "/users/")
	}
}

func userDeleteHandler(deps *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			return echo.NewHTTPError(http.StatusNotFound)
		}
		target, err := deps.AuthService.GetUserByIDAny(c.Request().Context(), id)
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

		if _, err := deps.AuthService.DeleteUser(c.Request().Context(), id); err != nil {
			return err
		}

		deps.Activity.LogDelete(requestContext(c), "user", target.ID.String(), target.Email, nil)
		AddFlash(c, "success", "User "+target.Email+" deleted successfully.")
		return c.Redirect(http.StatusFound, "/users/")
	}
}
