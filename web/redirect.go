package web

import (
	"net/url"
	"strings"

	"github.com/labstack/echo/v4"
)

// safeNext validates a caller-supplied post-login redirect target so a
// crafted /login/?next=https://evil.example link can never produce a real
// 302 Location header to an untrusted host. Returns "" (caller must fall
// back to a fixed default) if next is empty, malformed, protocol-relative,
// or points somewhere outside idp's own host / the apps sharing its
// COOKIE_DOMAIN-scoped session cookie.
func safeNext(c echo.Context, next, cookieDomain string) string {
	if next == "" || strings.HasPrefix(next, "//") || strings.HasPrefix(next, "/\\") || strings.HasPrefix(next, "\\") {
		return ""
	}
	u, err := url.Parse(next)
	if err != nil {
		return ""
	}
	if u.Host == "" {
		if !strings.HasPrefix(u.Path, "/") {
			return ""
		}
		return next
	}
	reqHost := c.Request().Host
	if h, _, ok := strings.Cut(reqHost, ":"); ok {
		reqHost = h
	}
	nextHost := u.Hostname()
	if nextHost == reqHost {
		return next
	}
	if cookieDomain != "" && (nextHost == strings.TrimPrefix(cookieDomain, ".") || strings.HasSuffix(nextHost, cookieDomain)) {
		return next
	}
	return ""
}
