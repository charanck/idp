package config

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ErrAlreadyExists is returned by create operations when the target name is
// already taken.
var ErrAlreadyExists = errors.New("already exists")

// ListAllApplications lists every application, optionally filtered by a
// case-insensitive name substring, ordered by name.
func (s *ConfigService) ListAllApplications(ctx context.Context, q string) ([]Application, error) {
	return s.apps.List(ctx, q)
}

// GetApplicationByID returns an application by ID, or nil if not found.
func (s *ConfigService) GetApplicationByID(ctx context.Context, id uuid.UUID) (*Application, error) {
	app, err := s.apps.FindByID(ctx, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil //nolint:nilnil // "not found" is a valid outcome, not an error.
	}
	if err != nil {
		return nil, err
	}
	return app, nil
}

// CreateApplication creates a new application, returning ErrAlreadyExists if
// the name is already taken.
func (s *ConfigService) CreateApplication(ctx context.Context, name string) (*Application, error) {
	_, err := s.apps.FindByName(ctx, name)
	if err == nil {
		return nil, fmt.Errorf("application %q already exists: %w", name, ErrAlreadyExists)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	app := &Application{Name: name}
	if err := s.apps.Create(ctx, app); err != nil {
		return nil, err
	}
	return app, nil
}

// UpdateApplication renames an application by ID, returning nil if not found.
func (s *ConfigService) UpdateApplication(ctx context.Context, id uuid.UUID, name string) (*Application, error) {
	app, err := s.GetApplicationByID(ctx, id)
	if err != nil || app == nil {
		return app, err
	}
	app.Name = name
	if err := s.apps.Update(ctx, app); err != nil {
		return nil, err
	}
	return app, nil
}

// DeleteApplication deletes an application by ID (cascading to its
// environments/configs/flags), returning nil if not found.
func (s *ConfigService) DeleteApplication(ctx context.Context, id uuid.UUID) (*Application, error) {
	app, err := s.GetApplicationByID(ctx, id)
	if err != nil || app == nil {
		return app, err
	}
	if err := s.apps.Delete(ctx, app); err != nil {
		return nil, err
	}
	return app, nil
}
