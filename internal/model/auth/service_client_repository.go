package model

import (
	"context"

	"github.com/google/uuid"
)

// ServiceClientRepository is the persistence boundary for ServiceClient and
// its OIDC-related join tables (config/flag Application scope, redirect
// URIs, allowed login groups).
type ServiceClientRepository interface {
	FindByName(ctx context.Context, name string) (*ServiceClient, error)
	FindByAPIKeyIDActive(ctx context.Context, apiKeyID string) (*ServiceClient, error)
	FindByID(ctx context.Context, id uuid.UUID) (*ServiceClient, error)
	FindActiveByID(ctx context.Context, id uuid.UUID) (*ServiceClient, error)
	Create(ctx context.Context, client *ServiceClient) error
	Update(ctx context.Context, client *ServiceClient) error
	Delete(ctx context.Context, client *ServiceClient) error
	List(ctx context.Context, q string, isActive *bool) ([]ServiceClient, error)

	ListApplicationIDs(ctx context.Context, clientID uuid.UUID) ([]uuid.UUID, error)
	SetApplications(ctx context.Context, clientID uuid.UUID, applicationIDs []uuid.UUID) error

	ListRedirectURIs(ctx context.Context, clientID uuid.UUID) ([]string, error)
	SetRedirectURIs(ctx context.Context, clientID uuid.UUID, redirectURIs []string) error

	ListAllowedGroupIDs(ctx context.Context, clientID uuid.UUID) ([]uuid.UUID, error)
	SetAllowedGroups(ctx context.Context, clientID uuid.UUID, groupIDs []uuid.UUID) error
}

// OIDCSigningKeyRepository is the persistence boundary for the singleton
// OIDCSigningKey row.
type OIDCSigningKeyRepository interface {
	Get(ctx context.Context) (*OIDCSigningKey, error)
	// GetOrCreate returns the singleton signing key, calling generate and
	// inserting its result under a Postgres advisory lock if no key exists
	// yet - closing the race between concurrent first-time callers (e.g. two
	// server instances booting simultaneously) without ever needing to
	// discard a generated keypair.
	GetOrCreate(ctx context.Context, generate func() (*OIDCSigningKey, error)) (*OIDCSigningKey, error)
}

// OIDCAuthorizationCodeRepository is the persistence boundary for
// OIDCAuthorizationCode.
type OIDCAuthorizationCodeRepository interface {
	Create(ctx context.Context, code *OIDCAuthorizationCode) error
	// FindAndConsume atomically loads code and marks it used, returning nil
	// if the code doesn't exist, is already used, or has expired - so a code
	// can never be replayed even under concurrent requests.
	FindAndConsume(ctx context.Context, code string) (*OIDCAuthorizationCode, error)
}
