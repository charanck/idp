package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"controlplane/internal/cache"
	model "controlplane/internal/model/config"
)

// FeatureFlagService mirrors config_management/services.py's FeatureFlagService.
type FeatureFlagService struct {
	flags        model.FeatureFlagRepository
	apps         model.ApplicationRepository
	envs         model.EnvironmentRepository
	cache        cache.Cache
	cacheTimeout time.Duration
}

func NewFeatureFlagService(flags model.FeatureFlagRepository, apps model.ApplicationRepository, envs model.EnvironmentRepository, c cache.Cache, cacheTimeout time.Duration) *FeatureFlagService {
	return &FeatureFlagService{flags: flags, apps: apps, envs: envs, cache: c, cacheTimeout: cacheTimeout}
}

// ErrApplicationNotFound, ErrEnvironmentRequired and ErrNoEnvironmentsFound
// mirror the ValueErrors raised by FeatureFlagService.create_flag.
var (
	ErrApplicationNotFound = errors.New("application does not exist")
	ErrEnvironmentRequired = errors.New("environment is required when create_all_environments is false")
	ErrNoEnvironmentsFound = errors.New("no environments found for the selected application")
)

// getScope looks up an existing Application/Environment, propagating the raw
// gorm error (including gorm.ErrRecordNotFound) rather than swallowing it -
// unlike ConfigService.getScope, every caller here needs to distinguish
// "scope missing" from other errors itself.
func (s *FeatureFlagService) getScope(ctx context.Context, service, environment string) (*model.Application, *model.Environment, error) {
	app, err := s.apps.FindByName(ctx, service)
	if err != nil {
		return nil, nil, err
	}
	env, err := s.envs.FindByApplicationAndName(ctx, app.ID, environment)
	if err != nil {
		return nil, nil, err
	}
	return app, env, nil
}

func flagScopeVersionKey(service, environment string) string {
	return fmt.Sprintf("flag:scope-version:%s:%s", service, environment)
}

func (s *FeatureFlagService) getScopeVersion(ctx context.Context, service, environment string) (int64, error) {
	return s.cache.GetVersion(ctx, flagScopeVersionKey(service, environment))
}

// InvalidateScopeCache bumps the scope-version counter for a service/environment,
// invalidating any cached feature-flag list payloads for it.
func (s *FeatureFlagService) InvalidateScopeCache(ctx context.Context, service, environment string) error {
	return s.cache.BumpVersion(ctx, flagScopeVersionKey(service, environment))
}

// CreateFlagOptions bundles create_flag's optional parameters.
type CreateFlagOptions struct {
	Description           string
	IsEnabled             bool
	Environment           string // required when CreateAllEnvironments is false
	CreateAllEnvironments bool
}

// CreateFlag creates (or updates) a feature flag across one or all
// environments of an application, mirroring FeatureFlagService.create_flag.
func (s *FeatureFlagService) CreateFlag(ctx context.Context, service, name string, opts CreateFlagOptions) ([]model.FeatureFlag, error) {
	app, err := s.apps.FindByName(ctx, service)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("application %q does not exist: %w", service, ErrApplicationNotFound)
		}
		return nil, err
	}

	var environments []model.Environment
	if opts.CreateAllEnvironments {
		environments, err = s.envs.ListByApplicationID(ctx, app.ID)
		if err != nil {
			return nil, err
		}
	} else {
		if opts.Environment == "" {
			return nil, ErrEnvironmentRequired
		}
		env, err := s.envs.FindByApplicationAndName(ctx, app.ID, opts.Environment)
		if err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, err
			}
		} else {
			environments = []model.Environment{*env}
		}
	}
	if len(environments) == 0 {
		return nil, ErrNoEnvironmentsFound
	}

	var description *string
	if opts.Description != "" {
		description = &opts.Description
	}

	flags := make([]model.FeatureFlag, 0, len(environments))
	for _, env := range environments {
		flag, err := s.flags.FindByScopeAndName(ctx, app.ID, env.ID, name)
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			flag = &model.FeatureFlag{
				ApplicationID: app.ID,
				EnvironmentID: env.ID,
				Name:          name,
				Description:   description,
				IsEnabled:     opts.IsEnabled,
			}
			if err := s.flags.Create(ctx, flag); err != nil {
				return nil, err
			}
		case err != nil:
			return nil, err
		default:
			flag.Description = description
			flag.IsEnabled = opts.IsEnabled
			flag.DeletedAt = nil
			if err := s.flags.Update(ctx, flag); err != nil {
				return nil, err
			}
		}

		flag.Application = *app
		flag.Environment = env
		flags = append(flags, *flag)
		if err := s.InvalidateScopeCache(ctx, service, env.Name); err != nil {
			return nil, err
		}
	}

	slog.Info("created/updated feature flag", "service", service, "name", name, "environments", len(flags))
	return flags, nil
}

// GetFlag returns a feature flag by name, or nil if not found (including if
// the service/environment scope itself doesn't exist).
func (s *FeatureFlagService) GetFlag(ctx context.Context, service, environment, name string) (*model.FeatureFlag, error) {
	app, env, err := s.getScope(ctx, service, environment)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	flag, err := s.flags.FindActiveByScopeAndName(ctx, app.ID, env.ID, name)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	flag.Application = *app
	flag.Environment = *env
	return flag, nil
}

// ListFlags lists all active (non-deleted) feature flags for a service/environment.
func (s *FeatureFlagService) ListFlags(ctx context.Context, service, environment string) ([]model.FeatureFlag, error) {
	app, env, err := s.getScope(ctx, service, environment)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return []model.FeatureFlag{}, nil
	}
	if err != nil {
		return nil, err
	}

	scopeVersion, err := s.getScopeVersion(ctx, service, environment)
	if err != nil {
		return nil, err
	}
	cacheKey := fmt.Sprintf("flag:list:%s:%s:v%d", service, environment, scopeVersion)

	if cached, found, err := s.cache.Get(ctx, cacheKey); err != nil {
		return nil, err
	} else if found {
		var flags []model.FeatureFlag
		if err := json.Unmarshal([]byte(cached), &flags); err == nil {
			return flags, nil
		}
	}

	flags, err := s.flags.ListActiveByScope(ctx, app.ID, env.ID)
	if err != nil {
		return nil, err
	}

	if serialized, err := json.Marshal(flags); err == nil {
		if err := s.cache.Set(ctx, cacheKey, string(serialized), s.cacheTimeout); err != nil {
			return nil, err
		}
	}
	return flags, nil
}

// ToggleFlag flips a feature flag's enabled state, returning nil if not found.
func (s *FeatureFlagService) ToggleFlag(ctx context.Context, service, environment, name string) (*model.FeatureFlag, error) {
	app, env, err := s.getScope(ctx, service, environment)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		slog.Warn("feature flag toggle failed: scope not found", "service", service, "environment", environment, "name", name)
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	flag, err := s.flags.FindActiveByScopeAndName(ctx, app.ID, env.ID, name)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		slog.Warn("feature flag toggle failed: not found", "service", service, "environment", environment, "name", name)
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	flag.IsEnabled = !flag.IsEnabled
	if err := s.flags.Update(ctx, flag); err != nil {
		return nil, err
	}
	if err := s.InvalidateScopeCache(ctx, service, environment); err != nil {
		return nil, err
	}
	flag.Application = *app
	flag.Environment = *env
	slog.Info("toggled feature flag", "service", service, "environment", environment, "name", name, "is_enabled", flag.IsEnabled)
	return flag, nil
}

// ListAllFlags lists non-deleted feature flags (with Application/Environment
// preloaded) across every service/environment, for the admin flags list page.
func (s *FeatureFlagService) ListAllFlags(ctx context.Context, filter model.ListFlagsFilter) ([]model.FeatureFlag, error) {
	return s.flags.List(ctx, filter)
}

// GetFlagByID returns a feature flag by ID (with Application/Environment
// preloaded), or nil if not found.
func (s *FeatureFlagService) GetFlagByID(ctx context.Context, id uuid.UUID) (*model.FeatureFlag, error) {
	flag, err := s.flags.FindByID(ctx, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil //nolint:nilnil // "not found" is a valid outcome, not an error.
	}
	if err != nil {
		return nil, err
	}
	return flag, nil
}

// ToggleFlagByID flips a feature flag's enabled state by ID, returning nil if not found.
func (s *FeatureFlagService) ToggleFlagByID(ctx context.Context, id uuid.UUID) (*model.FeatureFlag, error) {
	flag, err := s.GetFlagByID(ctx, id)
	if err != nil || flag == nil {
		return flag, err
	}

	flag.IsEnabled = !flag.IsEnabled
	if err := s.flags.Update(ctx, flag); err != nil {
		return nil, err
	}
	if err := s.InvalidateScopeCache(ctx, flag.Application.Name, flag.Environment.Name); err != nil {
		return nil, err
	}
	return flag, nil
}

// SoftDeleteFlagByID soft-deletes (sets deleted_at) a feature flag by ID,
// returning nil if not found.
func (s *FeatureFlagService) SoftDeleteFlagByID(ctx context.Context, id uuid.UUID) (*model.FeatureFlag, error) {
	flag, err := s.GetFlagByID(ctx, id)
	if err != nil || flag == nil {
		return flag, err
	}

	now := time.Now()
	flag.DeletedAt = &now
	if err := s.flags.Update(ctx, flag); err != nil {
		return nil, err
	}
	if err := s.InvalidateScopeCache(ctx, flag.Application.Name, flag.Environment.Name); err != nil {
		return nil, err
	}
	return flag, nil
}
