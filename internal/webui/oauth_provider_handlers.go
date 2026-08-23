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
	"controlplane/views/pages"
)

const oauthProvidersPageSize = 10

func oauthProvidersListHandler(deps *Deps) echo.HandlerFunc {
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

		var providers []auth.OAuthProvider
		if err := query.Find(&providers).Error; err != nil {
			return err
		}

		extra := url.Values{}
		if q != "" {
			extra.Set("q", q)
		}
		if statusFilter != "" {
			extra.Set("status", statusFilter)
		}

		page := Paginate(providers, oauthProvidersPageSize, PageParam(c))
		return pages.OAuthProvidersList(flashes(c), navUser(c), pages.OAuthProvidersListData{
			Providers: page.Items, CurrentQ: q, CurrentStatus: statusFilter, ExtraQuery: extra.Encode(),
			Page: page.Number, NumPages: page.NumPages,
			HasPrev: page.HasPrevious, HasNext: page.HasNext,
			PrevNum: page.PreviousNumber, NextNum: page.NextNumber,
			Window: page.PageRange(), CSRFToken: csrfToken(c),
		}).Render(c.Request().Context(), c.Response())
	}
}

func oauthProviderFormFromRequest(c echo.Context, existing *auth.OAuthProvider) auth.OAuthProvider {
	p := auth.OAuthProvider{}
	if existing != nil {
		p = *existing
	}
	p.Name = strings.TrimSpace(c.FormValue("name"))
	p.ClientID = strings.TrimSpace(c.FormValue("client_id"))
	if secret := c.FormValue("client_secret"); secret != "" || existing == nil {
		p.ClientSecret = secret
	}
	p.AuthorizationURL = strings.TrimSpace(c.FormValue("authorization_url"))
	p.TokenURL = strings.TrimSpace(c.FormValue("token_url"))
	if userinfo := strings.TrimSpace(c.FormValue("userinfo_url")); userinfo != "" {
		p.UserinfoURL = &userinfo
	} else {
		p.UserinfoURL = nil
	}
	p.Scope = strings.TrimSpace(c.FormValue("scope"))
	p.AutoCreateUsers = c.FormValue("auto_create_users") != ""
	p.IsActive = c.FormValue("is_active") != ""
	return p
}

func oauthProviderCreateHandler(deps *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
		if c.Request().Method == http.MethodGet {
			return pages.OAuthProviderForm(flashes(c), navUser(c), pages.OAuthProviderFormData{
				CSRFToken: csrfToken(c), Title: "Create OAuth Provider", Action: "/oauth/providers/create/",
			}).Render(c.Request().Context(), c.Response())
		}

		p := oauthProviderFormFromRequest(c, nil)
		reRender := func(errMsg string) error {
			return pages.OAuthProviderForm(flashes(c), navUser(c), pages.OAuthProviderFormData{
				CSRFToken: csrfToken(c), Title: "Create OAuth Provider", Action: "/oauth/providers/create/",
				Name: p.Name, ClientID: p.ClientID, ClientSecret: p.ClientSecret,
				AuthorizationURL: p.AuthorizationURL, TokenURL: p.TokenURL,
				UserinfoURL: derefOrEmpty(p.UserinfoURL), Scope: p.Scope,
				AutoCreateUsers: p.AutoCreateUsers, IsActive: p.IsActive, Error: errMsg,
			}).Render(c.Request().Context(), c.Response())
		}

		if p.Name == "" || p.ClientID == "" || p.ClientSecret == "" || p.AuthorizationURL == "" || p.TokenURL == "" {
			return reRender("Name, client ID, client secret, authorization URL, and token URL are required.")
		}

		if err := deps.DB.Create(&p).Error; err != nil {
			return err
		}

		deps.Activity.LogCreate("oauth_provider", p.ID.String(), p.Name, requestContext(c), map[string]any{"scope": p.Scope, "active": p.IsActive})
		AddFlash(c, "success", "OAuth provider \""+p.Name+"\" created successfully.")
		return c.Redirect(http.StatusFound, "/oauth/providers/")
	}
}

func loadOAuthProvider(deps *Deps, c echo.Context) (*auth.OAuthProvider, error) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return nil, echo.NewHTTPError(http.StatusNotFound)
	}
	var p auth.OAuthProvider
	if err := deps.DB.First(&p, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, echo.NewHTTPError(http.StatusNotFound)
		}
		return nil, err
	}
	return &p, nil
}

func oauthProviderEditHandler(deps *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
		existing, err := loadOAuthProvider(deps, c)
		if err != nil {
			return err
		}

		if c.Request().Method == http.MethodGet {
			return pages.OAuthProviderForm(flashes(c), navUser(c), pages.OAuthProviderFormData{
				CSRFToken: csrfToken(c), Title: "Edit OAuth Provider", Action: "/oauth/providers/" + existing.ID.String() + "/edit/",
				Name: existing.Name, ClientID: existing.ClientID, ClientSecret: existing.ClientSecret,
				AuthorizationURL: existing.AuthorizationURL, TokenURL: existing.TokenURL,
				UserinfoURL: derefOrEmpty(existing.UserinfoURL), Scope: existing.Scope,
				AutoCreateUsers: existing.AutoCreateUsers, IsActive: existing.IsActive, IsEdit: true,
			}).Render(c.Request().Context(), c.Response())
		}

		p := oauthProviderFormFromRequest(c, existing)
		reRender := func(errMsg string) error {
			return pages.OAuthProviderForm(flashes(c), navUser(c), pages.OAuthProviderFormData{
				CSRFToken: csrfToken(c), Title: "Edit OAuth Provider", Action: "/oauth/providers/" + existing.ID.String() + "/edit/",
				Name: p.Name, ClientID: p.ClientID, ClientSecret: p.ClientSecret,
				AuthorizationURL: p.AuthorizationURL, TokenURL: p.TokenURL,
				UserinfoURL: derefOrEmpty(p.UserinfoURL), Scope: p.Scope,
				AutoCreateUsers: p.AutoCreateUsers, IsActive: p.IsActive, IsEdit: true, Error: errMsg,
			}).Render(c.Request().Context(), c.Response())
		}

		if p.Name == "" || p.ClientID == "" || p.ClientSecret == "" || p.AuthorizationURL == "" || p.TokenURL == "" {
			return reRender("Name, client ID, client secret, authorization URL, and token URL are required.")
		}

		if err := deps.DB.Save(&p).Error; err != nil {
			return err
		}

		deps.Activity.LogUpdate("oauth_provider", p.ID.String(), p.Name, requestContext(c), map[string]any{"scope": p.Scope, "active": p.IsActive})
		AddFlash(c, "success", "OAuth provider \""+p.Name+"\" updated successfully.")
		return c.Redirect(http.StatusFound, "/oauth/providers/")
	}
}

func oauthProviderDeleteHandler(deps *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
		p, err := loadOAuthProvider(deps, c)
		if err != nil {
			return err
		}

		if c.Request().Method == http.MethodGet {
			return pages.ConfirmDelete(flashes(c), navUser(c), "oauth_providers", pages.ConfirmDeleteData{
				CSRFToken: csrfToken(c), Title: "Delete OAuth Provider",
				Message:    "Are you sure you want to delete \"" + p.Name + "\"?",
				Action:     "/oauth/providers/" + p.ID.String() + "/delete/",
				CancelHref: "/oauth/providers/",
			}).Render(c.Request().Context(), c.Response())
		}

		if err := deps.DB.Delete(p).Error; err != nil {
			return err
		}
		deps.Activity.LogDelete("oauth_provider", p.ID.String(), p.Name, requestContext(c), nil)
		AddFlash(c, "success", "OAuth provider \""+p.Name+"\" deleted successfully.")
		return c.Redirect(http.StatusFound, "/oauth/providers/")
	}
}

func oauthProviderToggleHandler(deps *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
		p, err := loadOAuthProvider(deps, c)
		if err != nil {
			return err
		}
		p.IsActive = !p.IsActive
		if err := deps.DB.Save(p).Error; err != nil {
			return err
		}
		deps.Activity.LogToggle("oauth_provider", p.ID.String(), p.Name, requestContext(c), map[string]any{"is_active": p.IsActive})

		status := "deactivated"
		if p.IsActive {
			status = "activated"
		}
		AddFlash(c, "success", "OAuth provider \""+p.Name+"\" "+status+".")
		return c.Redirect(http.StatusFound, "/oauth/providers/")
	}
}

func derefOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
