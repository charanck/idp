package model

import (
	"context"

	"github.com/google/uuid"
)

// ServiceClientRepository is the persistence boundary for ServiceClient.
type ServiceClientRepository interface {
	FindByName(ctx context.Context, name string) (*ServiceClient, error)
	FindByAPIKeyIDActive(ctx context.Context, apiKeyID string) (*ServiceClient, error)
	FindByID(ctx context.Context, id uuid.UUID) (*ServiceClient, error)
	FindActiveByID(ctx context.Context, id uuid.UUID) (*ServiceClient, error)
	Create(ctx context.Context, client *ServiceClient) error
	Update(ctx context.Context, client *ServiceClient) error
	Delete(ctx context.Context, client *ServiceClient) error
	List(ctx context.Context, q string, isActive *bool) ([]ServiceClient, error)
}
