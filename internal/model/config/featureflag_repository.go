package model

import (
	"context"

	"github.com/google/uuid"
)

// ListFlagsFilter filters FeatureFlagRepository.List. ApplicationIDs scopes
// results to a group-based Application allow-list (nil/empty =
// unrestricted); it composes with ApplicationID, which narrows to one
// specific Application within that scope.
type ListFlagsFilter struct {
	ApplicationID  *uuid.UUID
	ApplicationIDs []uuid.UUID
	EnvironmentID  *uuid.UUID
	IsEnabled      *bool
}

// FeatureFlagRepository is the persistence boundary for FeatureFlag rows.
// Not-found lookups return gorm's raw error (including gorm.ErrRecordNotFound)
// rather than swallowing it - callers decide how to translate that.
//
// FindByScopeAndName and FindActiveByScopeAndName are deliberately separate:
// CreateFlag's undelete-on-recreate behavior needs to see soft-deleted rows
// (no deleted_at filter), while GetFlag/ToggleFlag must never resurrect one.
type FeatureFlagRepository interface {
	FindByScopeAndName(ctx context.Context, applicationID, environmentID uuid.UUID, name string) (*FeatureFlag, error)
	FindActiveByScopeAndName(ctx context.Context, applicationID, environmentID uuid.UUID, name string) (*FeatureFlag, error)
	ListActiveByScope(ctx context.Context, applicationID, environmentID uuid.UUID) ([]FeatureFlag, error)
	Create(ctx context.Context, flag *FeatureFlag) error
	Update(ctx context.Context, flag *FeatureFlag) error
	FindByID(ctx context.Context, id uuid.UUID) (*FeatureFlag, error)
	List(ctx context.Context, filter ListFlagsFilter) ([]FeatureFlag, error)
}
