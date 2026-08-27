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

const clientsPageSize = 20

// ClientStore is what service-client CRUD handlers need. Satisfied by *auth.AuthService.
type ClientStore interface {
	ListServiceClients(ctx context.Context, q string, isActive *bool) ([]authmodel.ServiceClient, error)
	CreateServiceClient(ctx context.Context, name string) (*auth.ServiceClientCredentials, error)
	GetServiceClientByIDAny(ctx context.Context, id uuid.UUID) (*authmodel.ServiceClient, error)
	ToggleServiceClient(ctx context.Context, id uuid.UUID) (*authmodel.ServiceClient, error)
	DeleteServiceClient(ctx context.Context, id uuid.UUID) (*authmodel.ServiceClient, error)
	RegenerateServiceClientKey(ctx context.Context, id uuid.UUID) (*authmodel.ServiceClient, error)
}

type ClientHandler struct {
	clients  ClientStore
	activity ActivityRecorder
}

func NewClientHandler(clients ClientStore, activity ActivityRecorder) *ClientHandler {
	return &ClientHandler{clients: clients, activity: activity}
}

func (h *ClientHandler) List(c echo.Context) error {
	q := strings.TrimSpace(c.QueryParam("q"))
	statusFilter := c.QueryParam("status")

	var isActive *bool
	switch statusFilter {
	case "active":
		v := true
		isActive = &v
	case "inactive":
		v := false
		isActive = &v
	}

	clients, err := h.clients.ListServiceClients(c.Request().Context(), q, isActive)
	if err != nil {
		return err
	}

	extra := url.Values{}
	if q != "" {
		extra.Set("q", q)
	}
	if statusFilter != "" {
		extra.Set("status", statusFilter)
	}

	page := Paginate(clients, clientsPageSize, PageParam(c))
	return pages.ClientsList(flashes(c), navUser(c), pages.ClientsListData{
		Clients: page.Items, CurrentQ: q, CurrentStatus: statusFilter, ExtraQuery: extra.Encode(),
		Page: page.Number, NumPages: page.NumPages,
		HasPrev: page.HasPrevious, HasNext: page.HasNext,
		PrevNum: page.PreviousNumber, NextNum: page.NextNumber,
		Window: page.PageRange(),
	}).Render(c.Request().Context(), c.Response())
}

func (h *ClientHandler) Create(c echo.Context) error {
	if c.Request().Method == http.MethodGet {
		return pages.ClientForm(flashes(c), navUser(c), pages.ClientFormData{
			CSRFToken: csrfToken(c),
		}).Render(c.Request().Context(), c.Response())
	}

	name := strings.TrimSpace(c.FormValue("name"))
	if name == "" {
		return pages.ClientForm(flashes(c), navUser(c), pages.ClientFormData{
			CSRFToken: csrfToken(c), Name: name, Error: "Client name is required.",
		}).Render(c.Request().Context(), c.Response())
	}

	creds, err := h.clients.CreateServiceClient(c.Request().Context(), name)
	if err != nil {
		if errors.Is(err, auth.ErrAlreadyExists) {
			return pages.ClientForm(flashes(c), navUser(c), pages.ClientFormData{
				CSRFToken: csrfToken(c), Name: name, Error: "A service client with that name already exists.",
			}).Render(c.Request().Context(), c.Response())
		}
		return err
	}

	h.activity.LogCreate(requestContext(c), "client", creds.Client.ID.String(), creds.Client.Name, nil)
	AddFlash(c, "success", "Service client created successfully.")

	apiKeyID := ""
	if creds.Client.APIKeyID != nil {
		apiKeyID = *creds.Client.APIKeyID
	}
	return pages.ClientCreated(flashes(c), navUser(c), pages.ClientCreatedData{
		ClientID: creds.Client.ID.String(), Name: creds.Client.Name, APIKeyID: apiKeyID, APIKey: creds.APIKey,
	}).Render(c.Request().Context(), c.Response())
}

func (h *ClientHandler) loadClient(c echo.Context) (*authmodel.ServiceClient, error) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return nil, echo.NewHTTPError(http.StatusNotFound)
	}
	client, err := h.clients.GetServiceClientByIDAny(c.Request().Context(), id)
	if err != nil {
		return nil, err
	}
	if client == nil {
		return nil, echo.NewHTTPError(http.StatusNotFound)
	}
	return client, nil
}

func (h *ClientHandler) Detail(c echo.Context) error {
	client, err := h.loadClient(c)
	if err != nil {
		return err
	}

	apiKeyID := ""
	if client.APIKeyID != nil {
		apiKeyID = *client.APIKeyID
	}
	return pages.ClientDetail(flashes(c), navUser(c), pages.ClientDetailData{
		CSRFToken: csrfToken(c), ID: client.ID.String(), Name: client.Name, APIKeyID: apiKeyID,
		EncryptionKey: client.EncryptionKey, IsActive: client.IsActive,
		CreatedAt: client.CreatedAt.Format("2006-01-02 15:04"), UpdatedAt: client.UpdatedAt.Format("2006-01-02 15:04"),
	}).Render(c.Request().Context(), c.Response())
}

func (h *ClientHandler) Toggle(c echo.Context) error {
	client, err := h.loadClient(c)
	if err != nil {
		return err
	}

	client, err = h.clients.ToggleServiceClient(c.Request().Context(), client.ID)
	if err != nil {
		return err
	}
	h.activity.LogToggle(requestContext(c), "client", client.ID.String(), client.Name, map[string]any{"is_active": client.IsActive})

	status := "deactivated"
	if client.IsActive {
		status = "activated"
	}
	AddFlash(c, "success", "Service client "+client.Name+" "+status+".")
	return c.Redirect(http.StatusFound, "/clients/")
}

func (h *ClientHandler) Delete(c echo.Context) error {
	client, err := h.loadClient(c)
	if err != nil {
		return err
	}

	if c.Request().Method == http.MethodGet {
		return pages.ConfirmDelete(flashes(c), navUser(c), "clients", pages.ConfirmDeleteData{
			CSRFToken: csrfToken(c), Title: "Delete Service Client",
			Message:    "Are you sure you want to delete \"" + client.Name + "\"?",
			Action:     "/clients/" + client.ID.String() + "/delete/",
			CancelHref: "/clients/",
		}).Render(c.Request().Context(), c.Response())
	}

	if _, err := h.clients.DeleteServiceClient(c.Request().Context(), client.ID); err != nil {
		return err
	}
	h.activity.LogDelete(requestContext(c), "client", client.ID.String(), client.Name, nil)
	AddFlash(c, "success", "Service client "+client.Name+" deleted successfully.")
	return c.Redirect(http.StatusFound, "/clients/")
}

func (h *ClientHandler) RegenerateKey(c echo.Context) error {
	client, err := h.loadClient(c)
	if err != nil {
		return err
	}

	if c.Request().Method == http.MethodPost {
		client, err = h.clients.RegenerateServiceClientKey(c.Request().Context(), client.ID)
		if err != nil {
			return err
		}
		h.activity.LogUpdate(requestContext(c), "client", client.ID.String(), client.Name, map[string]any{"action": "regenerate_encryption_key"})
		AddFlash(c, "success", "Encryption key regenerated for "+client.Name+". The client must be updated with the new key.")
	}

	return c.Redirect(http.StatusFound, "/clients/"+client.ID.String()+"/")
}

var _ ClientStore = (*auth.AuthService)(nil)
