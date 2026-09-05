package web

import (
	"context"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"

	"controlplane/internal/auth"
	authmodel "controlplane/internal/model/auth"
	"controlplane/web/template/pages"
)

// PolicyStore is what the Policies settings page needs. Satisfied by *auth.AuthService.
type PolicyStore interface {
	GetPolicy(ctx context.Context) (*authmodel.Policy, error)
	UpdatePolicy(ctx context.Context, selfRegistrationAllowedDomains string) (*authmodel.Policy, error)
}

type PolicyHandler struct {
	policies PolicyStore
	activity ActivityRecorder
}

func NewPolicyHandler(policies PolicyStore, activity ActivityRecorder) *PolicyHandler {
	return &PolicyHandler{policies: policies, activity: activity}
}

func (h *PolicyHandler) Show(c echo.Context) error {
	policy, err := h.policies.GetPolicy(c.Request().Context())
	if err != nil {
		return err
	}

	if c.Request().Method == http.MethodGet {
		return pages.PoliciesForm(flashes(c), navUser(c), pages.PoliciesFormData{
			CSRFToken:                      csrfToken(c),
			SelfRegistrationAllowedDomains: policy.SelfRegistrationAllowedDomains,
		}).Render(c.Request().Context(), c.Response())
	}

	domains := strings.TrimSpace(c.FormValue("self_registration_allowed_domains"))
	if _, err := h.policies.UpdatePolicy(c.Request().Context(), domains); err != nil {
		return pages.PoliciesForm(flashes(c), navUser(c), pages.PoliciesFormData{
			CSRFToken: csrfToken(c), SelfRegistrationAllowedDomains: domains, Error: err.Error(),
		}).Render(c.Request().Context(), c.Response())
	}

	h.activity.LogUpdate(requestContext(c), "policy", "1", "policies", nil)
	AddFlash(c, "success", "Policies updated.")
	return c.Redirect(http.StatusFound, "/policies/")
}

var _ PolicyStore = (*auth.AuthService)(nil)
