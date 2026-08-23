package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/gorm"

	"controlplane/internal/cache"
)

// FeatureFlagService mirrors config_management/services.py's FeatureFlagService.
type FeatureFlagService struct {
	db           *gorm.DB
	cache        cache.Cache
	cacheTimeout time.Duration
}

func NewFeatureFlagService(db *gorm.DB, c cache.Cache, cacheTimeout time.Duration) *FeatureFlagService {
	return &FeatureFlagService{db: db, cache: c, cacheTimeout: cacheTimeout}
}

// ErrApplicationNotFound, ErrEnvironmentRequired and ErrNoEnvironmentsFound
// mirror the ValueErrors raised by FeatureFlagService.create_flag.
var (
	ErrApplicationNotFound = errors.New("application does not exist")
	ErrEnvironmentRequired = errors.New("environment is required when create_all_environments is false")
	ErrNoEnvironmentsFound = errors.New("no environments found for the selected application")
)

func (s *FeatureFlagService) getScope(service, environment string) (*Application, *Environment, error) {
	var app Application
	if err := s.db.Where("name = ?", service).First(&app).Error; err != nil {
		return nil, nil, err
	}
	var env Environment
	if err := s.db.Where("application_id = ? AND name = ?", app.ID, environment).First(&env).Error; err != nil {
		return nil, nil, err
	}
	return &app, &env, nil
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
func (s *FeatureFlagService) CreateFlag(ctx context.Context, service, name string, opts CreateFlagOptions) ([]FeatureFlag, error) {
	var app Application
	if err := s.db.Where("name = ?", service).First(&app).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("application %q does not exist: %w", service, ErrApplicationNotFound)
		}
		return nil, err
	}

	query := s.db.Where("application_id = ?", app.ID).Order("name")
	if !opts.CreateAllEnvironments {
		if opts.Environment == "" {
			return nil, ErrEnvironmentRequired
		}
		query = query.Where("name = ?", opts.Environment)
	}

	var environments []Environment
	if err := query.Find(&environments).Error; err != nil {
		return nil, err
	}
	if len(environments) == 0 {
		return nil, ErrNoEnvironmentsFound
	}

	var description *string
	if opts.Description != "" {
		description = &opts.Description
	}

	flags := make([]FeatureFlag, 0, len(environments))
	for _, env := range environments {
		var flag FeatureFlag
		err := s.db.Where("application_id = ? AND environment_id = ? AND name = ?", app.ID, env.ID, name).First(&flag).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			flag = FeatureFlag{
				ApplicationID: app.ID,
				EnvironmentID: env.ID,
				Name:          name,
				Description:   description,
				IsEnabled:     opts.IsEnabled,
			}
			if err := s.db.Create(&flag).Error; err != nil {
				return nil, err
			}
		} else if err != nil {
			return nil, err
		} else {
			flag.Description = description
			flag.IsEnabled = opts.IsEnabled
			flag.DeletedAt = nil
			if err := s.db.Save(&flag).Error; err != nil {
				return nil, err
			}
		}

		flag.Application = app
		flag.Environment = env
		flags = append(flags, flag)
		if err := s.InvalidateScopeCache(ctx, service, env.Name); err != nil {
			return nil, err
		}
	}

	slog.Info("created/updated feature flag", "service", service, "name", name, "environments", len(flags))
	return flags, nil
}

// GetFlag returns a feature flag by name, or nil if not found (including if
// the service/environment scope itself doesn't exist).
func (s *FeatureFlagService) GetFlag(service, environment, name string) (*FeatureFlag, error) {
	app, env, err := s.getScope(service, environment)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var flag FeatureFlag
	err = s.db.Where("application_id = ? AND environment_id = ? AND name = ? AND deleted_at IS NULL", app.ID, env.ID, name).First(&flag).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	flag.Application = *app
	flag.Environment = *env
	return &flag, nil
}

// ListFlags lists all active (non-deleted) feature flags for a service/environment.
func (s *FeatureFlagService) ListFlags(ctx context.Context, service, environment string) ([]FeatureFlag, error) {
	app, env, err := s.getScope(service, environment)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return []FeatureFlag{}, nil
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
		var flags []FeatureFlag
		if err := json.Unmarshal([]byte(cached), &flags); err == nil {
			return flags, nil
		}
	}

	var flags []FeatureFlag
	err = s.db.Preload("Application").Preload("Environment").
		Where("application_id = ? AND environment_id = ? AND deleted_at IS NULL", app.ID, env.ID).
		Find(&flags).Error
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
func (s *FeatureFlagService) ToggleFlag(ctx context.Context, service, environment, name string) (*FeatureFlag, error) {
	app, env, err := s.getScope(service, environment)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		slog.Warn("feature flag toggle failed: scope not found", "service", service, "environment", environment, "name", name)
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var flag FeatureFlag
	err = s.db.Where("application_id = ? AND environment_id = ? AND name = ? AND deleted_at IS NULL", app.ID, env.ID, name).First(&flag).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		slog.Warn("feature flag toggle failed: not found", "service", service, "environment", environment, "name", name)
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	flag.IsEnabled = !flag.IsEnabled
	if err := s.db.Save(&flag).Error; err != nil {
		return nil, err
	}
	if err := s.InvalidateScopeCache(ctx, service, environment); err != nil {
		return nil, err
	}

	flag.Application = *app
	flag.Environment = *env
	slog.Info("toggled feature flag", "service", service, "environment", environment, "name", name, "is_enabled", flag.IsEnabled)
	return &flag, nil
}
