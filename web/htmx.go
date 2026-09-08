package web

import (
	"encoding/json"
	"net/http"

	"github.com/labstack/echo/v4"
)

// IsHXRequest reports whether the current request was made by htmx
// (hx-get/hx-post/etc.), as opposed to a normal full-page navigation or form
// submit. Handlers use this to decide how to send a client "go to this URL
// next" instruction after a successful action.
func IsHXRequest(c echo.Context) bool {
	return c.Request().Header.Get("HX-Request") == "true"
}

// HXRedirect sends the client to url after a successful action: a real
// browser navigation (via the HX-Redirect response header) for htmx
// requests, or a normal 302 otherwise. Every create/update/delete handler
// that currently ends with `c.Redirect(http.StatusFound, url)` on success
// should call this instead, so the same handler works for both a full page
// post and an htmx-submitted form/button without any other changes -
// htmx-enabled templates rely on this together with `hx-select` (pulling
// just the relevant fragment out of the full-page response the handler
// still renders) rather than handlers needing a separate partial-render path.
func HXRedirect(c echo.Context, url string) error {
	if IsHXRequest(c) {
		c.Response().Header().Set("HX-Redirect", url)
		return c.NoContent(http.StatusOK)
	}
	return c.Redirect(http.StatusFound, url)
}

// TriggerToast queues a client-side toast notification (see
// web/static/app.js's "toast" event listener and Alpine.store("toasts")) to
// fire when the current htmx response is swapped in, for feedback on
// actions that update a fragment in place rather than navigating away (e.g.
// a feature-flag toggle). No-op for non-htmx requests, since those already
// get a flash message via AddFlash across the following full-page render.
func TriggerToast(c echo.Context, tag, text string) {
	if !IsHXRequest(c) {
		return
	}
	payload, err := json.Marshal(map[string]any{
		"toast": map[string]string{"tag": tag, "text": text},
	})
	if err != nil {
		return
	}
	c.Response().Header().Set("HX-Trigger", string(payload))
}
