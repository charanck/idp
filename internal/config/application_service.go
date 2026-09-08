package config

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"

	model "controlplane/internal/model/config"
)

// ErrAlreadyExists is returned by create operations when the target name is
// already taken.
var ErrAlreadyExists = errors.New("already exists")

// ListAllApplications lists applications, optionally filtered by a
// case-insensitive name substring, scoped to allowedIDs if non-empty (a
// group-based Application allow-list; empty = unrestricted), ordered by name.
func (s *ConfigService) ListAllApplications(ctx context.Context, q string, allowedIDs []uuid.UUID) ([]model.Application, error) {
	return s.apps.List(ctx, q, allowedIDs)
}

// GetApplicationByName returns an application by exact name, or nil if not found.
func (s *ConfigService) GetApplicationByName(ctx context.Context, name string) (*model.Application, error) {
	app, err := s.apps.FindByName(ctx, name)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil //nolint:nilnil // "not found" is a valid outcome, not an error.
	}
	if err != nil {
		return nil, err
	}
	return app, nil
}

// GetApplicationByID returns an application by ID, or nil if not found.
func (s *ConfigService) GetApplicationByID(ctx context.Context, id uuid.UUID) (*model.Application, error) {
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
func (s *ConfigService) CreateApplication(ctx context.Context, name string) (*model.Application, error) {
	_, err := s.apps.FindByName(ctx, name)
	if err == nil {
		return nil, fmt.Errorf("application %q already exists: %w", name, ErrAlreadyExists)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	app := &model.Application{Name: name}
	if err := s.apps.Create(ctx, app); err != nil {
		return nil, err
	}
	return app, nil
}

// UpdateApplication renames an application by ID, returning nil if not found.
func (s *ConfigService) UpdateApplication(ctx context.Context, id uuid.UUID, name string) (*model.Application, error) {
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
func (s *ConfigService) DeleteApplication(ctx context.Context, id uuid.UUID) (*model.Application, error) {
	app, err := s.GetApplicationByID(ctx, id)
	if err != nil || app == nil {
		return app, err
	}
	if err := s.apps.Delete(ctx, app); err != nil {
		return nil, err
	}
	return app, nil
}

// ListApplicationDomains returns the hostnames a reverse proxy may forward
// (X-Forwarded-Host) that map to an Application, for the forward-auth verify
// endpoint.
func (s *ConfigService) ListApplicationDomains(ctx context.Context, applicationID uuid.UUID) ([]string, error) {
	return s.domains.ListHosts(ctx, applicationID)
}

// SetApplicationDomains replaces the set of hostnames mapped to an
// Application wholesale, mirroring SetApplications/SetUserGroups' replace
// semantics.
func (s *ConfigService) SetApplicationDomains(ctx context.Context, applicationID uuid.UUID, hosts []string) error {
	return s.domains.SetHosts(ctx, applicationID, hosts)
}

// ApplicationByHost resolves the Application mapped to host (as forwarded by
// a reverse proxy via X-Forwarded-Host), or nil if no Application claims
// that host.
func (s *ConfigService) ApplicationByHost(ctx context.Context, host string) (*model.Application, error) {
	id, err := s.domains.FindApplicationIDByHost(ctx, host)
	if err != nil {
		return nil, err
	}
	if id == uuid.Nil {
		return nil, nil
	}
	return s.GetApplicationByID(ctx, id)
}
