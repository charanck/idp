package web

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"

	"controlplane/internal/auth"
	authmodel "controlplane/internal/model/auth"
	"controlplane/web/template/pages"
)

// PolicyStore is what the Policies settings page needs. Satisfied by *auth.AuthService.
type PolicyStore interface {
	GetPolicy(ctx context.Context) (*authmodel.Policy, error)
	UpdatePolicy(ctx context.Context, in auth.UpdatePolicyInput) (*authmodel.Policy, error)
}

type PolicyHandler struct {
	policies PolicyStore
	activity ActivityRecorder
}

func NewPolicyHandler(policies PolicyStore, activity ActivityRecorder) *PolicyHandler {
	return &PolicyHandler{policies: policies, activity: activity}
}

func policyFormData(csrfToken string, policy *authmodel.Policy, errMsg string) pages.PoliciesFormData {
	return pages.PoliciesFormData{
		CSRFToken:                      csrfToken,
		Error:                          errMsg,
		SelfRegistrationAllowedDomains: policy.SelfRegistrationAllowedDomains,
		PasswordMinLength:              policy.PasswordMinLength,
		PasswordRequireUpper:           policy.PasswordRequireUpper,
		PasswordRequireLower:           policy.PasswordRequireLower,
		PasswordRequireDigit:           policy.PasswordRequireDigit,
		PasswordRequireSymbol:          policy.PasswordRequireSymbol,
		PasswordMaxAgeDays:             policy.PasswordMaxAgeDays,
		MaxFailedLoginAttempts:         policy.MaxFailedLoginAttempts,
		LockoutDurationMinutes:         policy.LockoutDurationMinutes,
		SessionIdleTimeoutMinutes:      policy.SessionIdleTimeoutMinutes,
		SSOOnly:                        policy.SSOOnly,
		LoginIPAllowlist:               policy.LoginIPAllowlist,
	}
}

func (h *PolicyHandler) Show(c echo.Context) error {
	policy, err := h.policies.GetPolicy(c.Request().Context())
	if err != nil {
		return err
	}

	if c.Request().Method == http.MethodGet {
		return pages.PoliciesForm(flashes(c), navUser(c), policyFormData(csrfToken(c), policy, "")).Render(c.Request().Context(), c.Response())
	}

	in := auth.UpdatePolicyInput{
		SelfRegistrationAllowedDomains: strings.TrimSpace(c.FormValue("self_registration_allowed_domains")),
		PasswordMinLength:              formInt(c, "password_min_length"),
		PasswordRequireUpper:           c.FormValue("password_require_upper") != "",
		PasswordRequireLower:           c.FormValue("password_require_lower") != "",
		PasswordRequireDigit:           c.FormValue("password_require_digit") != "",
		PasswordRequireSymbol:          c.FormValue("password_require_symbol") != "",
		PasswordMaxAgeDays:             formInt(c, "password_max_age_days"),
		MaxFailedLoginAttempts:         formInt(c, "max_failed_login_attempts"),
		LockoutDurationMinutes:         formInt(c, "lockout_duration_minutes"),
		SessionIdleTimeoutMinutes:      formInt(c, "session_idle_timeout_minutes"),
		SSOOnly:                        c.FormValue("sso_only") != "",
		LoginIPAllowlist:               strings.TrimSpace(c.FormValue("login_ip_allowlist")),
	}

	updated, err := h.policies.UpdatePolicy(c.Request().Context(), in)
	if err != nil {
		return pages.PoliciesForm(flashes(c), navUser(c), policyFormData(csrfToken(c), policy, err.Error())).Render(c.Request().Context(), c.Response())
	}
	policy = updated

	h.activity.LogUpdate(requestContext(c), "policy", "1", "policies", nil)

	if IsHXRequest(c) {
		TriggerToast(c, "success", "Policies updated.")
		return pages.PoliciesForm(flashes(c), navUser(c), policyFormData(csrfToken(c), policy, "")).Render(c.Request().Context(), c.Response())
	}
	AddFlash(c, "success", "Policies updated.")
	return c.Redirect(http.StatusFound, "/policies/")
}

// formInt parses a form field as an int, defaulting to 0 on empty/invalid input.
func formInt(c echo.Context, name string) int {
	v, err := strconv.Atoi(strings.TrimSpace(c.FormValue(name)))
	if err != nil {
		return 0
	}
	return v
}

var _ PolicyStore = (*auth.AuthService)(nil)
