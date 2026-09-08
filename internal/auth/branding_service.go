package auth

import (
	"context"
	"errors"
	"regexp"
	"strings"

	model "controlplane/internal/model/auth"
)

// ErrInvalidAccentColor is returned when AccentColor isn't a bare "#rrggbb"
// (or "#rgb") hex color. Enforced because AccentColor is rendered directly
// into a CSS custom property on the login page - restricting it to a hex
// color shape rules out CSS/markup injection via the branding form.
var ErrInvalidAccentColor = errors.New("accent color must be a hex color like #4f46e5")

// ErrInvalidBackgroundImageURL is returned when BackgroundImageURL isn't an
// http(s) URL, ruling out javascript:/data: URIs in the rendered <img>/CSS.
var ErrInvalidBackgroundImageURL = errors.New("background image URL must start with http:// or https://")

var hexColorPattern = regexp.MustCompile(`^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)

// GetBranding returns the singleton login/product branding settings row.
func (s *AuthService) GetBranding(ctx context.Context) (*model.Branding, error) {
	return s.branding.Get(ctx)
}

// UpdateBrandingInput bundles Branding's editable fields.
type UpdateBrandingInput struct {
	ProductName        string
	LogoURL            string
	AccentColor        string
	BackgroundImageURL string
}

// UpdateBranding replaces the singleton branding settings row.
func (s *AuthService) UpdateBranding(ctx context.Context, in UpdateBrandingInput) (*model.Branding, error) {
	if in.AccentColor != "" && !hexColorPattern.MatchString(in.AccentColor) {
		return nil, ErrInvalidAccentColor
	}
	if in.BackgroundImageURL != "" && !strings.HasPrefix(in.BackgroundImageURL, "http://") && !strings.HasPrefix(in.BackgroundImageURL, "https://") {
		return nil, ErrInvalidBackgroundImageURL
	}

	branding, err := s.branding.Get(ctx)
	if err != nil {
		return nil, err
	}
	branding.ProductName = in.ProductName
	branding.LogoURL = in.LogoURL
	branding.AccentColor = in.AccentColor
	branding.BackgroundImageURL = in.BackgroundImageURL
	if err := s.branding.Update(ctx, branding); err != nil {
		return nil, err
	}
	return branding, nil
}
