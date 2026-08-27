package auth

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"gorm.io/gorm"

	model "controlplane/internal/model/auth"
)

// ListProviders lists OAuth providers, optionally filtered by a
// case-insensitive name substring and active status.
func (s *OAuthService) ListProviders(ctx context.Context, q string, isActive *bool) ([]model.OAuthProvider, error) {
	return s.providers.List(ctx, q, isActive)
}

// ListActiveProviders lists all active OAuth providers, for the login page's
// "sign in with" options.
func (s *OAuthService) ListActiveProviders(ctx context.Context) ([]model.OAuthProvider, error) {
	active := true
	return s.ListProviders(ctx, "", &active)
}

// GetProviderByID returns an OAuth provider by ID regardless of active
// status, or nil if not found.
func (s *OAuthService) GetProviderByID(ctx context.Context, id uuid.UUID) (*model.OAuthProvider, error) {
	p, err := s.providers.FindByID(ctx, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return p, nil
}

// GetActiveProviderByID returns an active OAuth provider by ID, or nil if
// not found/inactive.
func (s *OAuthService) GetActiveProviderByID(ctx context.Context, id uuid.UUID) (*model.OAuthProvider, error) {
	p, err := s.providers.FindActiveByID(ctx, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return p, nil
}

// CreateProvider creates a new OAuth provider.
func (s *OAuthService) CreateProvider(ctx context.Context, p model.OAuthProvider) (*model.OAuthProvider, error) {
	if err := s.providers.Create(ctx, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// UpdateProvider saves an existing OAuth provider's fields (p.ID must be set).
func (s *OAuthService) UpdateProvider(ctx context.Context, p model.OAuthProvider) (*model.OAuthProvider, error) {
	if err := s.providers.Update(ctx, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// DeleteProvider deletes an OAuth provider by ID, returning nil if not found.
func (s *OAuthService) DeleteProvider(ctx context.Context, id uuid.UUID) (*model.OAuthProvider, error) {
	p, err := s.GetProviderByID(ctx, id)
	if err != nil || p == nil {
		return p, err
	}
	if err := s.providers.Delete(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

// ToggleProvider flips an OAuth provider's active state by ID, returning nil if not found.
func (s *OAuthService) ToggleProvider(ctx context.Context, id uuid.UUID) (*model.OAuthProvider, error) {
	p, err := s.GetProviderByID(ctx, id)
	if err != nil || p == nil {
		return p, err
	}
	p.IsActive = !p.IsActive
	if err := s.providers.Update(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}
