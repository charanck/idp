package model

import (
	"context"

	"github.com/google/uuid"
)

// OAuthProviderRepository is the persistence boundary for OAuthProvider.
type OAuthProviderRepository interface {
	List(ctx context.Context, q string, isActive *bool) ([]OAuthProvider, error)
	FindByID(ctx context.Context, id uuid.UUID) (*OAuthProvider, error)
	FindActiveByID(ctx context.Context, id uuid.UUID) (*OAuthProvider, error)
	Create(ctx context.Context, p *OAuthProvider) error
	Update(ctx context.Context, p *OAuthProvider) error
	Delete(ctx context.Context, p *OAuthProvider) error
}
