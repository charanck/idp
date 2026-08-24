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
	query := s.db.WithContext(ctx).Order("name")
	if q != "" {
		query = query.Where("name ILIKE ?", "%"+q+"%")
	}
	var apps []Application
	if err := query.Find(&apps).Error; err != nil {
		return nil, err
	}
	return apps, nil
}

// GetApplicationByID returns an application by ID, or nil if not found.
func (s *ConfigService) GetApplicationByID(ctx context.Context, id uuid.UUID) (*Application, error) {
	var app Application
	err := s.db.WithContext(ctx).First(&app, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil //nolint:nilnil // "not found" is a valid outcome, not an error.
	}
	if err != nil {
		return nil, err
	}
	return &app, nil
}

// CreateApplication creates a new application, returning ErrAlreadyExists if
// the name is already taken.
func (s *ConfigService) CreateApplication(ctx context.Context, name string) (*Application, error) {
	db := s.db.WithContext(ctx)

	var existing Application
	err := db.Where("name = ?", name).First(&existing).Error
	if err == nil {
		return nil, fmt.Errorf("application %q already exists: %w", name, ErrAlreadyExists)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	app := Application{Name: name}
	if err := db.Create(&app).Error; err != nil {
		return nil, err
	}
	return &app, nil
}

// UpdateApplication renames an application by ID, returning nil if not found.
func (s *ConfigService) UpdateApplication(ctx context.Context, id uuid.UUID, name string) (*Application, error) {
	app, err := s.GetApplicationByID(ctx, id)
	if err != nil || app == nil {
		return app, err
	}
	app.Name = name
	if err := s.db.WithContext(ctx).Save(app).Error; err != nil {
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
	if err := s.db.WithContext(ctx).Delete(app).Error; err != nil {
		return nil, err
	}
	return app, nil
}
