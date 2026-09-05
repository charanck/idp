package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"controlplane/internal/auth"
	authmodel "controlplane/internal/model/auth"
	"controlplane/web/template/pages"
)

const groupsPageSize = 20

// GroupStore is what group CRUD handlers need. Satisfied by *auth.AuthService.
type GroupStore interface {
	ListGroups(ctx context.Context, q string) ([]authmodel.Group, error)
	GetGroupByID(ctx context.Context, id uuid.UUID) (*authmodel.Group, error)
	GroupApplicationIDs(ctx context.Context, groupID uuid.UUID) ([]uuid.UUID, error)
	CreateGroup(ctx context.Context, in auth.CreateGroupInput) (*authmodel.Group, error)
	UpdateGroup(ctx context.Context, id uuid.UUID, in auth.CreateGroupInput) (*authmodel.Group, error)
	DeleteGroup(ctx context.Context, id uuid.UUID) (*authmodel.Group, error)
}

type GroupHandler struct {
	groups   GroupStore
	apps     ApplicationStore
	activity ActivityRecorder
}

func NewGroupHandler(groups GroupStore, apps ApplicationStore, activity ActivityRecorder) *GroupHandler {
	return &GroupHandler{groups: groups, apps: apps, activity: activity}
}

func (h *GroupHandler) List(c echo.Context) error {
	q := strings.TrimSpace(c.QueryParam("q"))

	groups, err := h.groups.ListGroups(c.Request().Context(), q)
	if err != nil {
		return err
	}

	page := Paginate(groups, groupsPageSize, PageParam(c))

	return pages.GroupsList(flashes(c), navUser(c), pages.GroupsListData{
		Groups: page.Items, CurrentQ: q, Page: page.Number, NumPages: page.NumPages,
		HasPrev: page.HasPrevious, HasNext: page.HasNext,
		PrevNum: page.PreviousNumber, NextNum: page.NextNumber,
		Window: page.PageRange(), CSRFToken: csrfToken(c),
	}).Render(c.Request().Context(), c.Response())
}

func (h *GroupHandler) allApplications(ctx context.Context) ([]pages.GroupApplicationOption, error) {
	apps, err := h.apps.ListAllApplications(ctx, "", nil)
	if err != nil {
		return nil, err
	}
	opts := make([]pages.GroupApplicationOption, 0, len(apps))
	for _, app := range apps {
		opts = append(opts, pages.GroupApplicationOption{ID: app.ID.String(), Name: app.Name})
	}
	return opts, nil
}

func groupFormInput(c echo.Context) auth.CreateGroupInput {
	in := auth.CreateGroupInput{Name: strings.TrimSpace(c.FormValue("name"))}
	for _, module := range auth.AllModules {
		if c.FormValue("module_"+module) != "" {
			in.Permissions = append(in.Permissions, module)
		}
	}
	for _, idStr := range c.Request().Form["application_ids"] {
		if id, err := uuid.Parse(idStr); err == nil {
			in.ApplicationIDs = append(in.ApplicationIDs, id)
		}
	}
	return in
}

func selectedSet(ids []uuid.UUID) map[string]bool {
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[id.String()] = true
	}
	return set
}

func (h *GroupHandler) Create(c echo.Context) error {
	apps, err := h.allApplications(c.Request().Context())
	if err != nil {
		return err
	}

	if c.Request().Method == http.MethodGet {
		return pages.GroupForm(flashes(c), navUser(c), pages.GroupFormData{
			CSRFToken: csrfToken(c), Title: "Create Group", Action: "/groups/create/",
			Applications: apps, SelectedModules: map[string]bool{},
			SelectedApplicationIDs: map[string]bool{},
		}).Render(c.Request().Context(), c.Response())
	}

	if err := c.Request().ParseForm(); err != nil {
		return err
	}
	in := groupFormInput(c)
	reRender := func(errMsg string) error {
		return pages.GroupForm(flashes(c), navUser(c), pages.GroupFormData{
			CSRFToken: csrfToken(c), Title: "Create Group", Action: "/groups/create/",
			Applications: apps, Name: in.Name, Error: errMsg,
			SelectedModules:        selectedModules(in.Permissions),
			SelectedApplicationIDs: selectedSet(in.ApplicationIDs),
		}).Render(c.Request().Context(), c.Response())
	}

	if in.Name == "" {
		return reRender("Name is required.")
	}

	created, err := h.groups.CreateGroup(c.Request().Context(), in)
	if err != nil {
		return reRender(err.Error())
	}

	h.activity.LogCreate(requestContext(c), "group", created.ID.String(), created.Name, nil)
	AddFlash(c, "success", "Group \""+created.Name+"\" created.")
	return c.Redirect(http.StatusFound, "/groups/")
}

func selectedModules(modules []string) map[string]bool {
	set := make(map[string]bool, len(modules))
	for _, m := range modules {
		set[m] = true
	}
	return set
}

func (h *GroupHandler) loadGroup(c echo.Context) (*authmodel.Group, error) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return nil, echo.NewHTTPError(http.StatusNotFound)
	}
	group, err := h.groups.GetGroupByID(c.Request().Context(), id)
	if err != nil {
		return nil, err
	}
	if group == nil {
		return nil, echo.NewHTTPError(http.StatusNotFound)
	}
	return group, nil
}

func groupModules(group *authmodel.Group) []string {
	var modules []string
	if len(group.Permissions) > 0 {
		_ = json.Unmarshal(group.Permissions, &modules)
	}
	return modules
}

func (h *GroupHandler) Edit(c echo.Context) error {
	existing, err := h.loadGroup(c)
	if err != nil {
		return err
	}
	if existing.IsSystem {
		return echo.NewHTTPError(http.StatusForbidden, "built-in groups cannot be edited")
	}

	apps, err := h.allApplications(c.Request().Context())
	if err != nil {
		return err
	}
	appIDs, err := h.groups.GroupApplicationIDs(c.Request().Context(), existing.ID)
	if err != nil {
		return err
	}

	action := "/groups/" + existing.ID.String() + "/edit/"
	if c.Request().Method == http.MethodGet {
		return pages.GroupForm(flashes(c), navUser(c), pages.GroupFormData{
			CSRFToken: csrfToken(c), Title: "Edit Group", Action: action, IsEdit: true,
			Name: existing.Name, Applications: apps,
			SelectedModules:        selectedModules(groupModules(existing)),
			SelectedApplicationIDs: selectedSet(appIDs),
		}).Render(c.Request().Context(), c.Response())
	}

	if err := c.Request().ParseForm(); err != nil {
		return err
	}
	in := groupFormInput(c)
	reRender := func(errMsg string) error {
		return pages.GroupForm(flashes(c), navUser(c), pages.GroupFormData{
			CSRFToken: csrfToken(c), Title: "Edit Group", Action: action, IsEdit: true,
			Name: in.Name, Applications: apps, Error: errMsg,
			SelectedModules:        selectedModules(in.Permissions),
			SelectedApplicationIDs: selectedSet(in.ApplicationIDs),
		}).Render(c.Request().Context(), c.Response())
	}

	if in.Name == "" {
		return reRender("Name is required.")
	}

	updated, err := h.groups.UpdateGroup(c.Request().Context(), existing.ID, in)
	if err != nil {
		return reRender(err.Error())
	}

	h.activity.LogUpdate(requestContext(c), "group", updated.ID.String(), updated.Name, nil)
	AddFlash(c, "success", "Group \""+updated.Name+"\" updated.")
	return c.Redirect(http.StatusFound, "/groups/")
}

func (h *GroupHandler) Delete(c echo.Context) error {
	existing, err := h.loadGroup(c)
	if err != nil {
		return err
	}
	if existing.IsSystem {
		return echo.NewHTTPError(http.StatusForbidden, "built-in groups cannot be deleted")
	}

	if c.Request().Method == http.MethodGet {
		return pages.ConfirmDelete(flashes(c), navUser(c), "groups", pages.ConfirmDeleteData{
			CSRFToken: csrfToken(c), Title: "Delete Group",
			Message:    "Are you sure you want to delete \"" + existing.Name + "\"?",
			Action:     "/groups/" + existing.ID.String() + "/delete/",
			CancelHref: "/groups/",
		}).Render(c.Request().Context(), c.Response())
	}

	if _, err := h.groups.DeleteGroup(c.Request().Context(), existing.ID); err != nil {
		if errors.Is(err, auth.ErrSystemGroup) {
			return echo.NewHTTPError(http.StatusForbidden, err.Error())
		}
		return err
	}
	h.activity.LogDelete(requestContext(c), "group", existing.ID.String(), existing.Name, nil)
	AddFlash(c, "success", "Group \""+existing.Name+"\" deleted.")
	return c.Redirect(http.StatusFound, "/groups/")
}

var _ GroupStore = (*auth.AuthService)(nil)
