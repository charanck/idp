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
)

// ProxyAuthResolver is what the forward-auth verify endpoint needs to
// resolve a proxy-forwarded Host header to the ServiceClient whose
// IsProxyAuthEnabled toggle and AllowedGroupIDs allow-list gate access.
// Satisfied by *auth.AuthService.
type ProxyAuthResolver interface {
	ServiceClientByHost(ctx context.Context, host string) (*authmodel.ServiceClient, error)
	ServiceClientAllowsUser(ctx context.Context, client *authmodel.ServiceClient, userID uuid.UUID) (bool, error)
}

// GroupLister feeds the X-Auth-Request-Groups identity header. Satisfied by
// *auth.AuthService.
type GroupLister interface {
	UserGroups(ctx context.Context, userID uuid.UUID) ([]authmodel.Group, error)
}

type ForwardAuthHandler struct {
	clients   ProxyAuthResolver
	groups    GroupLister
	publicURL string
}

// NewForwardAuthHandler builds the forward-auth verify handler. publicURL is
// idp's own externally-reachable base URL (see appconfig.Config.PublicURL) -
// required to build a browser-reachable "/login/?next=..." redirect, since
// this endpoint is always reached over the internal network and can't infer
// its own public origin from the request it receives.
func NewForwardAuthHandler(clients ProxyAuthResolver, groups GroupLister, publicURL string) *ForwardAuthHandler {
	return &ForwardAuthHandler{clients: clients, groups: groups, publicURL: publicURL}
}

// Verify is a generic forward-auth endpoint a reverse proxy calls on every
// request to gate access to a proxied app using the control-plane's own
// login session (shared via the COOKIE_DOMAIN-scoped session cookie), for
// both Traefik ForwardAuth and nginx auth_request style configs. It reads
// the forwarded Host/URI/Proto standard proxy headers, resolves the
// ServiceClient mapped to that host (see service_client_domains), and checks
// the session user against that client's AllowedGroupIDs - the same gate
// OIDC login uses.
//
// On success: 200 with X-Auth-Request-Email/-User/-Groups identity headers,
// the header names nginx/oauth2-proxy conventions already expect.
//
// On failure: 401 by default - nginx's auth_request module treats any
// non-2xx subrequest response as denial, typically remapped to a login
// redirect via `error_page 401 = /login/`. Passing ?redirect=1 instead
// returns a 302 to /login/?next=<forwarded-url>, for proxies (Traefik
// ForwardAuth) that forward the auth response to the client verbatim rather
// than remapping the status code themselves. A host with no client mapped,
// or a client with proxy-auth disabled/inactive, fails closed (401/redirect),
// never silently allowed.
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

	client, err := h.clients.ServiceClientByHost(c.Request().Context(), host)
	if err != nil {
		return err
	}
	if client == nil || !client.IsActive || !client.IsProxyAuthEnabled {
		slog.DebugContext(c.Request().Context(), "forward-auth denied: host has no proxy-auth client mapping", "host", host, "user_id", user.ID, "client_found", client != nil)
		return h.deny(c, host, proto, uri)
	}

	allowed, err := h.clients.ServiceClientAllowsUser(c.Request().Context(), client, user.ID)
	if err != nil {
		return err
	}
	if !allowed {
		slog.DebugContext(c.Request().Context(), "forward-auth denied: user not allowed for client", "host", host, "user_id", user.ID, "client_id", client.ID)
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

	slog.DebugContext(c.Request().Context(), "forward-auth allowed", "host", host, "user_id", user.ID, "client_id", client.ID)

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
		// Absolute, so Traefik's forwardAuth (which resolves a relative
		// Location against the address it dialed - the internal
		// http://idp:8000 one) forwards it to the browser unchanged instead
		// of resolving it into that unreachable internal address.
		loginPath := "/login/?next=" + url.QueryEscape(next)
		if h.publicURL != "" {
			return c.Redirect(http.StatusFound, h.publicURL+loginPath)
		}
		return c.Redirect(http.StatusFound, loginPath)
	}
	return echo.NewHTTPError(http.StatusUnauthorized)
}
