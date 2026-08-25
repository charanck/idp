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
	return s.envs.List(ctx, filter)
}

// ListEnvironmentsByApplicationID lists all environments for a single
// application, ordered by name.
func (s *ConfigService) ListEnvironmentsByApplicationID(ctx context.Context, applicationID uuid.UUID) ([]Environment, error) {
	return s.envs.ListByApplicationID(ctx, applicationID)
}

// GetEnvironmentByID returns an environment by ID, or nil if not found.
func (s *ConfigService) GetEnvironmentByID(ctx context.Context, id uuid.UUID) (*Environment, error) {
	env, err := s.envs.FindByID(ctx, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil //nolint:nilnil // "not found" is a valid outcome, not an error.
	}
	if err != nil {
		return nil, err
	}
	return env, nil
}

// GetEnvironmentWithApplicationByID returns an environment by ID with its
// Application preloaded, or nil if not found.
func (s *ConfigService) GetEnvironmentWithApplicationByID(ctx context.Context, id uuid.UUID) (*Environment, error) {
	env, err := s.envs.FindByIDWithApplication(ctx, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil //nolint:nilnil // "not found" is a valid outcome, not an error.
	}
	if err != nil {
		return nil, err
	}
	return env, nil
}

// CreateEnvironment creates a new environment under an application, returning
// ErrAlreadyExists if the application already has an environment with that name.
func (s *ConfigService) CreateEnvironment(ctx context.Context, applicationID uuid.UUID, name string) (*Environment, error) {
	_, err := s.envs.FindByApplicationAndName(ctx, applicationID, name)
	if err == nil {
		return nil, ErrAlreadyExists
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	env := &Environment{ApplicationID: applicationID, Name: name}
	if err := s.envs.Create(ctx, env); err != nil {
		return nil, err
	}
	return env, nil
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
	if err := s.envs.Update(ctx, env); err != nil {
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
	if err := s.envs.Delete(ctx, env); err != nil {
		return nil, err
	}
	return env, nil
}
