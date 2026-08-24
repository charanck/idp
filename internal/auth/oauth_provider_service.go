package auth

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ListProviders lists OAuth providers, optionally filtered by a
// case-insensitive name substring and active status.
func (s *OAuthService) ListProviders(ctx context.Context, q string, isActive *bool) ([]OAuthProvider, error) {
	query := s.db.WithContext(ctx).Order("name")
	if q != "" {
		query = query.Where("name ILIKE ?", "%"+q+"%")
	}
	if isActive != nil {
		query = query.Where("is_active = ?", *isActive)
	}
	var providers []OAuthProvider
	if err := query.Find(&providers).Error; err != nil {
		return nil, err
	}
	return providers, nil
}

// ListActiveProviders lists all active OAuth providers, for the login page's
// "sign in with" options.
func (s *OAuthService) ListActiveProviders(ctx context.Context) ([]OAuthProvider, error) {
	active := true
	return s.ListProviders(ctx, "", &active)
}

// GetProviderByID returns an OAuth provider by ID regardless of active
// status, or nil if not found.
func (s *OAuthService) GetProviderByID(ctx context.Context, id uuid.UUID) (*OAuthProvider, error) {
	var p OAuthProvider
	err := s.db.WithContext(ctx).First(&p, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil //nolint:nilnil // "not found" is a valid outcome, not an error.
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// GetActiveProviderByID returns an active OAuth provider by ID, or nil if
// not found/inactive.
func (s *OAuthService) GetActiveProviderByID(ctx context.Context, id uuid.UUID) (*OAuthProvider, error) {
	var p OAuthProvider
	err := s.db.WithContext(ctx).First(&p, "id = ? AND is_active = ?", id, true).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil //nolint:nilnil // "not found" is a valid outcome, not an error.
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// CreateProvider creates a new OAuth provider.
func (s *OAuthService) CreateProvider(ctx context.Context, p OAuthProvider) (*OAuthProvider, error) {
	if err := s.db.WithContext(ctx).Create(&p).Error; err != nil {
		return nil, err
	}
	return &p, nil
}

// UpdateProvider saves an existing OAuth provider's fields (p.ID must be set).
func (s *OAuthService) UpdateProvider(ctx context.Context, p OAuthProvider) (*OAuthProvider, error) {
	if err := s.db.WithContext(ctx).Save(&p).Error; err != nil {
		return nil, err
	}
	return &p, nil
}

// DeleteProvider deletes an OAuth provider by ID, returning nil if not found.
func (s *OAuthService) DeleteProvider(ctx context.Context, id uuid.UUID) (*OAuthProvider, error) {
	p, err := s.GetProviderByID(ctx, id)
	if err != nil || p == nil {
		return p, err
	}
	if err := s.db.WithContext(ctx).Delete(p).Error; err != nil {
		return nil, err
	}
	return p, nil
}

// ToggleProvider flips an OAuth provider's active state by ID, returning nil if not found.
func (s *OAuthService) ToggleProvider(ctx context.Context, id uuid.UUID) (*OAuthProvider, error) {
	p, err := s.GetProviderByID(ctx, id)
	if err != nil || p == nil {
		return p, err
	}
	p.IsActive = !p.IsActive
	if err := s.db.WithContext(ctx).Save(p).Error; err != nil {
		return nil, err
	}
	return p, nil
}
