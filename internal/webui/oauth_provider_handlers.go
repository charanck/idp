package webui

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"controlplane/internal/auth"
	"controlplane/views/pages"
)

const oauthProvidersPageSize = 10

func oauthProvidersListHandler(deps *Deps) echo.HandlerFunc {
	return func(c echo.Context) error {
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

		providers, err := deps.OAuthService.ListProviders(c.Request().Context(), q, isActive)
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

		created, err := deps.OAuthService.CreateProvider(c.Request().Context(), p)
		if err != nil {
			return err
		}

		deps.Activity.LogCreate(requestContext(c), "oauth_provider", created.ID.String(), created.Name, map[string]any{"scope": created.Scope, "active": created.IsActive})
		AddFlash(c, "success", "OAuth provider \""+created.Name+"\" created successfully.")
		return c.Redirect(http.StatusFound, "/oauth/providers/")
	}
}

func loadOAuthProvider(deps *Deps, c echo.Context) (*auth.OAuthProvider, error) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return nil, echo.NewHTTPError(http.StatusNotFound)
	}
	p, err := deps.OAuthService.GetProviderByID(c.Request().Context(), id)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, echo.NewHTTPError(http.StatusNotFound)
	}
	return p, nil
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

		updated, err := deps.OAuthService.UpdateProvider(c.Request().Context(), p)
		if err != nil {
			return err
		}

		deps.Activity.LogUpdate(requestContext(c), "oauth_provider", updated.ID.String(), updated.Name, map[string]any{"scope": updated.Scope, "active": updated.IsActive})
		AddFlash(c, "success", "OAuth provider \""+updated.Name+"\" updated successfully.")
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

		if _, err := deps.OAuthService.DeleteProvider(c.Request().Context(), p.ID); err != nil {
			return err
		}
		deps.Activity.LogDelete(requestContext(c), "oauth_provider", p.ID.String(), p.Name, nil)
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
		p, err = deps.OAuthService.ToggleProvider(c.Request().Context(), p.ID)
		if err != nil {
			return err
		}
		deps.Activity.LogToggle(requestContext(c), "oauth_provider", p.ID.String(), p.Name, map[string]any{"is_active": p.IsActive})

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
