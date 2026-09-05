package model

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// Group is the single access-control primitive: a user's effective
// permissions are the union of their groups' Permissions module lists, and
// (optionally) a group can restrict members to a subset of Applications via
// GroupRepository.ListApplicationIDs/SetApplications. Admin/User are
// built-in, non-deletable groups (IsSystem) seeded by migration 00007.
type Group struct {
	ID          uuid.UUID      `gorm:"column:id;type:uuid;primaryKey"`
	Name        string         `gorm:"column:name"`
	IsSystem    bool           `gorm:"column:is_system"`
	Permissions datatypes.JSON `gorm:"column:permissions"`
	CreatedAt   time.Time      `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt   time.Time      `gorm:"column:updated_at;autoUpdateTime"`
}

func (Group) TableName() string { return "groups" }

func (g *Group) BeforeCreate(tx *gorm.DB) error {
	if g.ID == uuid.Nil {
		g.ID = uuid.New()
	}
	return nil
}

// GroupRepository is the persistence boundary for Group and its two join
// tables (user_groups, group_applications). Join-table membership is
// represented as bare []uuid.UUID rather than a cross-package model struct.
type GroupRepository interface {
	List(ctx context.Context, q string) ([]Group, error)
	FindByID(ctx context.Context, id uuid.UUID) (*Group, error)
	Create(ctx context.Context, group *Group) error
	Update(ctx context.Context, group *Group) error
	Delete(ctx context.Context, group *Group) error

	ListApplicationIDs(ctx context.Context, groupID uuid.UUID) ([]uuid.UUID, error)
	SetApplications(ctx context.Context, groupID uuid.UUID, applicationIDs []uuid.UUID) error

	ListByUserID(ctx context.Context, userID uuid.UUID) ([]Group, error)
	SetUserGroups(ctx context.Context, userID uuid.UUID, groupIDs []uuid.UUID) error
}
