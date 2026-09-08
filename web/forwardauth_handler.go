package web

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	authmodel "controlplane/internal/model/auth"
	model "controlplane/internal/model/config"
)

// ApplicationHostResolver is what the forward-auth verify endpoint needs to
// resolve a proxy-forwarded Host header to the Application whose Group
// allow-list gates access. Satisfied by *config.ConfigService.
type ApplicationHostResolver interface {
	ApplicationByHost(ctx context.Context, host string) (*model.Application, error)
}

// GroupLister feeds the X-Auth-Request-Groups identity header. Satisfied by
// *auth.AuthService.
type GroupLister interface {
	UserGroups(ctx context.Context, userID uuid.UUID) ([]authmodel.Group, error)
}

type ForwardAuthHandler struct {
	apps   ApplicationHostResolver
	groups GroupLister
}

func NewForwardAuthHandler(apps ApplicationHostResolver, groups GroupLister) *ForwardAuthHandler {
	return &ForwardAuthHandler{apps: apps, groups: groups}
}

// Verify is a generic forward-auth endpoint a reverse proxy calls on every
// request to gate access to a proxied app using the control-plane's own
// login session (shared via the COOKIE_DOMAIN-scoped session cookie), for
// both Traefik ForwardAuth and nginx auth_request style configs. It reads
// the forwarded Host/URI/Proto standard proxy headers, resolves the
// Application mapped to that host (see application_domains), and checks the
// session user's effective Group Application allow-list against it.
//
// On success: 200 with X-Auth-Request-Email/-User/-Groups identity headers,
// the header names nginx/oauth2-proxy conventions already expect.
//
// On failure: 401 by default - nginx's auth_request module treats any
// non-2xx subrequest response as denial, typically remapped to a login
// redirect via `error_page 401 = /login/`. Passing ?redirect=1 instead
// returns a 302 to /login/?next=<forwarded-url>, for proxies (Traefik
// ForwardAuth) that forward the auth response to the client verbatim rather
// than remapping the status code themselves. A host with no configured
// Application fails closed (401/redirect), never silently allowed.
func (h *ForwardAuthHandler) Verify(c echo.Context) error {
	host := c.Request().Header.Get("X-Forwarded-Host")
	uri := c.Request().Header.Get("X-Forwarded-Uri")
	if uri == "" {
		uri = c.Request().Header.Get("X-Original-URL")
	}
	proto := c.Request().Header.Get("X-Forwarded-Proto")

	user := CurrentUser(c)
	if user == nil || host == "" {
		slog.DebugContext(c.Request().Context(), "forward-auth denied: no session or host", "host", host, "uri", uri, "has_user", user != nil)
		return h.deny(c, host, proto, uri)
	}

	app, err := h.apps.ApplicationByHost(c.Request().Context(), host)
	if err != nil {
		return err
	}
	if app == nil || !ApplicationAllowed(c, app.ID) {
		slog.DebugContext(c.Request().Context(), "forward-auth denied: host has no application mapping or user not allowed", "host", host, "user_id", user.ID, "app_found", app != nil)
		return h.deny(c, host, proto, uri)
	}

	groups, err := h.groups.UserGroups(c.Request().Context(), user.ID)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(groups))
	for _, g := range groups {
		names = append(names, g.Name)
	}

	slog.DebugContext(c.Request().Context(), "forward-auth allowed", "host", host, "user_id", user.ID, "application_id", app.ID)

	resp := c.Response()
	resp.Header().Set("X-Auth-Request-Email", user.Email)
	resp.Header().Set("X-Auth-Request-User", user.ID.String())
	resp.Header().Set("X-Auth-Request-Groups", strings.Join(names, ","))
	return c.NoContent(http.StatusOK)
}

func (h *ForwardAuthHandler) deny(c echo.Context, host, proto, uri string) error {
	if c.QueryParam("redirect") != "" {
		next := "/dashboard/"
		if host != "" {
			if proto == "" {
				proto = "https"
			}
			next = proto + "://" + host + uri
		}
		return c.Redirect(http.StatusFound, "/login/?next="+url.QueryEscape(next))
	}
	return echo.NewHTTPError(http.StatusUnauthorized)
}
