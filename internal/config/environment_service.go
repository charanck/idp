package config

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ListEnvironmentsFilter filters ListAllEnvironments.
type ListEnvironmentsFilter struct {
	ApplicationID *uuid.UUID
	Query         string
}

// ListAllEnvironments lists environments (with Application preloaded),
// optionally filtered by application and a case-insensitive name substring.
func (s *ConfigService) ListAllEnvironments(ctx context.Context, filter ListEnvironmentsFilter) ([]Environment, error) {
	query := s.db.WithContext(ctx).Preload("Application").Order("name")
	if filter.ApplicationID != nil {
		query = query.Where("application_id = ?", *filter.ApplicationID)
	}
	if filter.Query != "" {
		query = query.Where("name ILIKE ?", "%"+filter.Query+"%")
	}
	var envs []Environment
	if err := query.Find(&envs).Error; err != nil {
		return nil, err
	}
	return envs, nil
}

// ListEnvironmentsByApplicationID lists all environments for a single
// application, unfiltered and unordered beyond name.
func (s *ConfigService) ListEnvironmentsByApplicationID(ctx context.Context, applicationID uuid.UUID) ([]Environment, error) {
	var envs []Environment
	if err := s.db.WithContext(ctx).Where("application_id = ?", applicationID).Find(&envs).Error; err != nil {
		return nil, err
	}
	return envs, nil
}

// GetEnvironmentByID returns an environment by ID, or nil if not found.
func (s *ConfigService) GetEnvironmentByID(ctx context.Context, id uuid.UUID) (*Environment, error) {
	var env Environment
	err := s.db.WithContext(ctx).First(&env, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil //nolint:nilnil // "not found" is a valid outcome, not an error.
	}
	if err != nil {
		return nil, err
	}
	return &env, nil
}

// GetEnvironmentWithApplicationByID returns an environment by ID with its
// Application preloaded, or nil if not found.
func (s *ConfigService) GetEnvironmentWithApplicationByID(ctx context.Context, id uuid.UUID) (*Environment, error) {
	var env Environment
	err := s.db.WithContext(ctx).Preload("Application").First(&env, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil //nolint:nilnil // "not found" is a valid outcome, not an error.
	}
	if err != nil {
		return nil, err
	}
	return &env, nil
}

// CreateEnvironment creates a new environment under an application, returning
// ErrAlreadyExists if the application already has an environment with that name.
func (s *ConfigService) CreateEnvironment(ctx context.Context, applicationID uuid.UUID, name string) (*Environment, error) {
	db := s.db.WithContext(ctx)

	var existing Environment
	err := db.Where("application_id = ? AND name = ?", applicationID, name).First(&existing).Error
	if err == nil {
		return nil, ErrAlreadyExists
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	env := Environment{ApplicationID: applicationID, Name: name}
	if err := db.Create(&env).Error; err != nil {
		return nil, err
	}
	return &env, nil
}

// UpdateEnvironment updates an environment's application/name by ID,
// returning nil if not found.
func (s *ConfigService) UpdateEnvironment(ctx context.Context, id, applicationID uuid.UUID, name string) (*Environment, error) {
	env, err := s.GetEnvironmentByID(ctx, id)
	if err != nil || env == nil {
		return env, err
	}
	env.ApplicationID = applicationID
	env.Name = name
	if err := s.db.WithContext(ctx).Save(env).Error; err != nil {
		return nil, err
	}
	return env, nil
}

// DeleteEnvironment deletes an environment by ID (cascading to its
// configs/flags), returning nil if not found.
func (s *ConfigService) DeleteEnvironment(ctx context.Context, id uuid.UUID) (*Environment, error) {
	env, err := s.GetEnvironmentWithApplicationByID(ctx, id)
	if err != nil || env == nil {
		return env, err
	}
	if err := s.db.WithContext(ctx).Delete(env).Error; err != nil {
		return nil, err
	}
	return env, nil
}
