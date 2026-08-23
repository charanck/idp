package webui

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"

	"controlplane/internal/auth"
	"controlplane/internal/crypto"
	"controlplane/views/pages"
)

const clientsPageSize = 20

func clientsListHandler(deps *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
		q := strings.TrimSpace(c.QueryParam("q"))
		statusFilter := c.QueryParam("status")

		query := deps.DB.Order("created_at DESC")
		if q != "" {
			query = query.Where("name ILIKE ?", "%"+q+"%")
		}
		switch statusFilter {
		case "active":
			query = query.Where("is_active = true")
		case "inactive":
			query = query.Where("is_active = false")
		}

		var clients []auth.ServiceClient
		if err := query.Find(&clients).Error; err != nil {
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
}

func clientCreateHandler(deps *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
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

		creds, err := deps.AuthService.CreateServiceClient(name)
		if err != nil {
			if errors.Is(err, auth.ErrAlreadyExists) {
				return pages.ClientForm(flashes(c), navUser(c), pages.ClientFormData{
					CSRFToken: csrfToken(c), Name: name, Error: "A service client with that name already exists.",
				}).Render(c.Request().Context(), c.Response())
			}
			return err
		}

		deps.Activity.LogCreate("client", creds.Client.ID.String(), creds.Client.Name, requestContext(c), nil)
		AddFlash(c, "success", "Service client created successfully.")

		apiKeyID := ""
		if creds.Client.APIKeyID != nil {
			apiKeyID = *creds.Client.APIKeyID
		}
		return pages.ClientCreated(flashes(c), navUser(c), pages.ClientCreatedData{
			ClientID: creds.Client.ID.String(), Name: creds.Client.Name, APIKeyID: apiKeyID, APIKey: creds.APIKey,
		}).Render(c.Request().Context(), c.Response())
	}
}

func loadClient(deps *Deps, c echo.Context) (*auth.ServiceClient, error) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return nil, echo.NewHTTPError(http.StatusNotFound)
	}
	var client auth.ServiceClient
	if err := deps.DB.First(&client, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, echo.NewHTTPError(http.StatusNotFound)
		}
		return nil, err
	}
	return &client, nil
}

func clientDetailHandler(deps *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
		client, err := loadClient(deps, c)
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
}

func clientToggleHandler(deps *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
		client, err := loadClient(deps, c)
		if err != nil {
			return err
		}
		client.IsActive = !client.IsActive
		if err := deps.DB.Save(client).Error; err != nil {
			return err
		}
		deps.Activity.LogToggle("client", client.ID.String(), client.Name, requestContext(c), map[string]any{"is_active": client.IsActive})

		status := "deactivated"
		if client.IsActive {
			status = "activated"
		}
		AddFlash(c, "success", "Service client "+client.Name+" "+status+".")
		return c.Redirect(http.StatusFound, "/clients/")
	}
}

func clientDeleteHandler(deps *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
		client, err := loadClient(deps, c)
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

		if err := deps.DB.Delete(client).Error; err != nil {
			return err
		}
		deps.Activity.LogDelete("client", client.ID.String(), client.Name, requestContext(c), nil)
		AddFlash(c, "success", "Service client "+client.Name+" deleted successfully.")
		return c.Redirect(http.StatusFound, "/clients/")
	}
}

func clientRegenerateKeyHandler(deps *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
		client, err := loadClient(deps, c)
		if err != nil {
			return err
		}

		if c.Request().Method == http.MethodPost {
			newKey, err := crypto.GenerateKey()
			if err != nil {
				return err
			}
			client.EncryptionKey = newKey
			if err := deps.DB.Save(client).Error; err != nil {
				return err
			}
			deps.Activity.LogUpdate("client", client.ID.String(), client.Name, requestContext(c), map[string]any{"action": "regenerate_encryption_key"})
			AddFlash(c, "success", "Encryption key regenerated for "+client.Name+". The client must be updated with the new key.")
		}

		return c.Redirect(http.StatusFound, "/clients/"+client.ID.String()+"/")
	}
}
