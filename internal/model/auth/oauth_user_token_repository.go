package model

import (
	"context"

	"github.com/google/uuid"
)

// OAuthUserTokenRepository is the persistence boundary for OAuthUserToken.
type OAuthUserTokenRepository interface {
	FindByProviderAndProviderUserID(ctx context.Context, providerID uuid.UUID, providerUserID string) (*OAuthUserToken, error)
	Create(ctx context.Context, t *OAuthUserToken) error
	Update(ctx context.Context, t *OAuthUserToken) error
}
