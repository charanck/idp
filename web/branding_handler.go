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

// BrandingStore is what the Branding settings page needs. Satisfied by *auth.AuthService.
type BrandingStore interface {
	GetBranding(ctx context.Context) (*authmodel.Branding, error)
	UpdateBranding(ctx context.Context, in auth.UpdateBrandingInput) (*authmodel.Branding, error)
}

type BrandingHandler struct {
	branding BrandingStore
	activity ActivityRecorder
}

func NewBrandingHandler(branding BrandingStore, activity ActivityRecorder) *BrandingHandler {
	return &BrandingHandler{branding: branding, activity: activity}
}

func brandingFormData(csrfToken string, branding *authmodel.Branding, errMsg string) pages.BrandingFormData {
	return pages.BrandingFormData{
		CSRFToken:          csrfToken,
		Error:              errMsg,
		ProductName:        branding.ProductName,
		LogoURL:            branding.LogoURL,
		AccentColor:        branding.AccentColor,
		BackgroundImageURL: branding.BackgroundImageURL,
	}
}

func (h *BrandingHandler) Show(c echo.Context) error {
	branding, err := h.branding.GetBranding(c.Request().Context())
	if err != nil {
		return err
	}

	if c.Request().Method == http.MethodGet {
		return pages.BrandingForm(flashes(c), navUser(c), brandingFormData(csrfToken(c), branding, "")).Render(c.Request().Context(), c.Response())
	}

	in := auth.UpdateBrandingInput{
		ProductName:        strings.TrimSpace(c.FormValue("product_name")),
		LogoURL:            strings.TrimSpace(c.FormValue("logo_url")),
		AccentColor:        strings.TrimSpace(c.FormValue("accent_color")),
		BackgroundImageURL: strings.TrimSpace(c.FormValue("background_image_url")),
	}

	updated, err := h.branding.UpdateBranding(c.Request().Context(), in)
	if err != nil {
		submitted := &authmodel.Branding{
			ProductName:        in.ProductName,
			LogoURL:            in.LogoURL,
			AccentColor:        in.AccentColor,
			BackgroundImageURL: in.BackgroundImageURL,
		}
		return pages.BrandingForm(flashes(c), navUser(c), brandingFormData(csrfToken(c), submitted, err.Error())).Render(c.Request().Context(), c.Response())
	}
	branding = updated

	h.activity.LogUpdate(requestContext(c), "branding", "1", "branding", nil)

	if IsHXRequest(c) {
		TriggerToast(c, "success", "Branding updated.")
		return pages.BrandingForm(flashes(c), navUser(c), brandingFormData(csrfToken(c), branding, "")).Render(c.Request().Context(), c.Response())
	}
	AddFlash(c, "success", "Branding updated.")
	return c.Redirect(http.StatusFound, "/branding/")
}

var _ BrandingStore = (*auth.AuthService)(nil)
