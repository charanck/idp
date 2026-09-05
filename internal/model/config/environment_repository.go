package model

import (
	"context"

	"github.com/google/uuid"
)

// ListEnvironmentsFilter filters EnvironmentRepository.List. ApplicationIDs
// scopes results to a group-based Application allow-list (nil/empty =
// unrestricted); it composes with ApplicationID, which narrows to one
// specific Application within that scope.
type ListEnvironmentsFilter struct {
	ApplicationID  *uuid.UUID
	ApplicationIDs []uuid.UUID
	Query          string
}

// EnvironmentRepository is the persistence boundary for Environment rows.
// Not-found lookups return gorm's raw error (including gorm.ErrRecordNotFound)
// rather than swallowing it - callers decide how to translate that.
type EnvironmentRepository interface {
	List(ctx context.Context, filter ListEnvironmentsFilter) ([]Environment, error)
	ListByApplicationID(ctx context.Context, applicationID uuid.UUID) ([]Environment, error)
	FindByID(ctx context.Context, id uuid.UUID) (*Environment, error)
	FindByIDWithApplication(ctx context.Context, id uuid.UUID) (*Environment, error)
	FindByApplicationAndName(ctx context.Context, applicationID uuid.UUID, name string) (*Environment, error)
	Create(ctx context.Context, env *Environment) error
	Update(ctx context.Context, env *Environment) error
	Delete(ctx context.Context, env *Environment) error
}
