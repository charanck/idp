package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	model "controlplane/internal/model/auth"
)

// ErrSystemGroup is returned when a caller attempts to mutate one of the
// built-in, non-deletable Admin/User groups.
var ErrSystemGroup = errors.New("built-in group cannot be modified")

// ListGroups lists groups, optionally filtered by a case-insensitive name substring.
func (s *AuthService) ListGroups(ctx context.Context, q string) ([]model.Group, error) {
	return s.groups.List(ctx, q)
}

// GetGroupByID returns a group by ID, or nil if not found.
func (s *AuthService) GetGroupByID(ctx context.Context, id uuid.UUID) (*model.Group, error) {
	group, err := s.groups.FindByID(ctx, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil //nolint:nilnil // "not found" is a valid outcome, not an error.
	}
	if err != nil {
		return nil, err
	}
	return group, nil
}

// GroupApplicationIDs returns the Application allow-list for a group (empty = unrestricted).
func (s *AuthService) GroupApplicationIDs(ctx context.Context, groupID uuid.UUID) ([]uuid.UUID, error) {
	return s.groups.ListApplicationIDs(ctx, groupID)
}

// CreateGroupInput bundles CreateGroup/UpdateGroup's mutable fields.
type CreateGroupInput struct {
	Name           string
	Permissions    []string
	ApplicationIDs []uuid.UUID
}

// CreateGroup creates a custom group with the given module permissions and
// optional Application allow-list.
func (s *AuthService) CreateGroup(ctx context.Context, in CreateGroupInput) (*model.Group, error) {
	permsJSON, err := json.Marshal(in.Permissions)
	if err != nil {
		return nil, err
	}
	group := &model.Group{
		ID:          uuid.New(),
		Name:        in.Name,
		IsSystem:    false,
		Permissions: datatypes.JSON(permsJSON),
	}
	if err := s.groups.Create(ctx, group); err != nil {
		return nil, err
	}
	if err := s.groups.SetApplications(ctx, group.ID, in.ApplicationIDs); err != nil {
		return nil, err
	}
	return group, nil
}

// UpdateGroup updates a custom group's name/permissions/Application scope,
// rejecting mutation of built-in (is_system) groups.
func (s *AuthService) UpdateGroup(ctx context.Context, id uuid.UUID, in CreateGroupInput) (*model.Group, error) {
	group, err := s.GetGroupByID(ctx, id)
	if err != nil || group == nil {
		return group, err
	}
	if group.IsSystem {
		return nil, fmt.Errorf("group %q: %w", group.Name, ErrSystemGroup)
	}

	permsJSON, err := json.Marshal(in.Permissions)
	if err != nil {
		return nil, err
	}
	group.Name = in.Name
	group.Permissions = datatypes.JSON(permsJSON)
	if err := s.groups.Update(ctx, group); err != nil {
		return nil, err
	}
	if err := s.groups.SetApplications(ctx, group.ID, in.ApplicationIDs); err != nil {
		return nil, err
	}
	return group, nil
}

// DeleteGroup deletes a custom group by ID, rejecting built-in groups.
func (s *AuthService) DeleteGroup(ctx context.Context, id uuid.UUID) (*model.Group, error) {
	group, err := s.GetGroupByID(ctx, id)
	if err != nil || group == nil {
		return group, err
	}
	if group.IsSystem {
		return nil, fmt.Errorf("group %q: %w", group.Name, ErrSystemGroup)
	}
	if err := s.groups.Delete(ctx, group); err != nil {
		return nil, err
	}
	return group, nil
}

// UserGroups returns the groups a user belongs to.
func (s *AuthService) UserGroups(ctx context.Context, userID uuid.UUID) ([]model.Group, error) {
	return s.groups.ListByUserID(ctx, userID)
}

// SetUserGroups replaces a user's group memberships.
func (s *AuthService) SetUserGroups(ctx context.Context, userID uuid.UUID, groupIDs []uuid.UUID) error {
	return s.groups.SetUserGroups(ctx, userID, groupIDs)
}

// defaultGroupByName finds a built-in group by name, returning nil (not an
// error) if it doesn't exist so callers can degrade gracefully in tests that
// don't seed the built-in groups.
func (s *AuthService) defaultGroupByName(ctx context.Context, name string) (*model.Group, error) {
	groups, err := s.groups.List(ctx, name)
	if err != nil {
		return nil, err
	}
	for _, g := range groups {
		if g.Name == name {
			return &g, nil
		}
	}
	return nil, nil
}
