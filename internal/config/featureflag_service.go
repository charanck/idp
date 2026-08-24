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

func (s *FeatureFlagService) getScope(ctx context.Context, service, environment string) (*Application, *Environment, error) {
	db := s.db.WithContext(ctx)

	var app Application
	if err := db.Where("name = ?", service).First(&app).Error; err != nil {
		return nil, nil, err
	}
	var env Environment
	if err := db.Where("application_id = ? AND name = ?", app.ID, environment).First(&env).Error; err != nil {
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
	db := s.db.WithContext(ctx)

	var app Application
	if err := db.Where("name = ?", service).First(&app).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("application %q does not exist: %w", service, ErrApplicationNotFound)
		}
		return nil, err
	}

	query := db.Where("application_id = ?", app.ID).Order("name")
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
		err := db.Where("application_id = ? AND environment_id = ? AND name = ?", app.ID, env.ID, name).First(&flag).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			flag = FeatureFlag{
				ApplicationID: app.ID,
				EnvironmentID: env.ID,
				Name:          name,
				Description:   description,
				IsEnabled:     opts.IsEnabled,
			}
			if err := db.Create(&flag).Error; err != nil {
				return nil, err
			}
		} else if err != nil {
			return nil, err
		} else {
			flag.Description = description
			flag.IsEnabled = opts.IsEnabled
			flag.DeletedAt = nil
			if err := db.Save(&flag).Error; err != nil {
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
func (s *FeatureFlagService) GetFlag(ctx context.Context, service, environment, name string) (*FeatureFlag, error) {
	app, env, err := s.getScope(ctx, service, environment)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var flag FeatureFlag
	err = s.db.WithContext(ctx).Where("application_id = ? AND environment_id = ? AND name = ? AND deleted_at IS NULL", app.ID, env.ID, name).First(&flag).Error
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
	app, env, err := s.getScope(ctx, service, environment)
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
	err = s.db.WithContext(ctx).Preload("Application").Preload("Environment").
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
	app, env, err := s.getScope(ctx, service, environment)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		slog.Warn("feature flag toggle failed: scope not found", "service", service, "environment", environment, "name", name)
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var flag FeatureFlag
	err = s.db.WithContext(ctx).Where("application_id = ? AND environment_id = ? AND name = ? AND deleted_at IS NULL", app.ID, env.ID, name).First(&flag).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		slog.Warn("feature flag toggle failed: not found", "service", service, "environment", environment, "name", name)
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	flag.IsEnabled = !flag.IsEnabled
	if err := s.db.WithContext(ctx).Save(&flag).Error; err != nil {
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

// ListFlagsFilter filters ListAllFlags.
type ListFlagsFilter struct {
	ApplicationID *uuid.UUID
	EnvironmentID *uuid.UUID
	IsEnabled     *bool
}

// ListAllFlags lists non-deleted feature flags (with Application/Environment
// preloaded) across every service/environment, for the admin flags list page.
func (s *FeatureFlagService) ListAllFlags(ctx context.Context, filter ListFlagsFilter) ([]FeatureFlag, error) {
	query := s.db.WithContext(ctx).Preload("Application").Preload("Environment").
		Joins("JOIN applications ON applications.id = feature_flags.application_id").
		Where("feature_flags.deleted_at IS NULL").
		Order("applications.name, feature_flags.name, feature_flags.description")
	if filter.ApplicationID != nil {
		query = query.Where("feature_flags.application_id = ?", *filter.ApplicationID)
	}
	if filter.EnvironmentID != nil {
		query = query.Where("feature_flags.environment_id = ?", *filter.EnvironmentID)
	}
	if filter.IsEnabled != nil {
		query = query.Where("feature_flags.is_enabled = ?", *filter.IsEnabled)
	}
	var flags []FeatureFlag
	if err := query.Find(&flags).Error; err != nil {
		return nil, err
	}
	return flags, nil
}

// GetFlagByID returns a feature flag by ID (with Application/Environment
// preloaded), or nil if not found.
func (s *FeatureFlagService) GetFlagByID(ctx context.Context, id uuid.UUID) (*FeatureFlag, error) {
	var flag FeatureFlag
	err := s.db.WithContext(ctx).Preload("Application").Preload("Environment").First(&flag, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil //nolint:nilnil // "not found" is a valid outcome, not an error.
	}
	if err != nil {
		return nil, err
	}
	return &flag, nil
}

// ToggleFlagByID flips a feature flag's enabled state by ID, returning nil if not found.
func (s *FeatureFlagService) ToggleFlagByID(ctx context.Context, id uuid.UUID) (*FeatureFlag, error) {
	flag, err := s.GetFlagByID(ctx, id)
	if err != nil || flag == nil {
		return flag, err
	}

	flag.IsEnabled = !flag.IsEnabled
	if err := s.db.WithContext(ctx).Save(flag).Error; err != nil {
		return nil, err
	}
	if err := s.InvalidateScopeCache(ctx, flag.Application.Name, flag.Environment.Name); err != nil {
		return nil, err
	}
	return flag, nil
}

// SoftDeleteFlagByID soft-deletes (sets deleted_at) a feature flag by ID,
// returning nil if not found.
func (s *FeatureFlagService) SoftDeleteFlagByID(ctx context.Context, id uuid.UUID) (*FeatureFlag, error) {
	flag, err := s.GetFlagByID(ctx, id)
	if err != nil || flag == nil {
		return flag, err
	}

	now := time.Now()
	flag.DeletedAt = &now
	if err := s.db.WithContext(ctx).Save(flag).Error; err != nil {
		return nil, err
	}
	if err := s.InvalidateScopeCache(ctx, flag.Application.Name, flag.Environment.Name); err != nil {
		return nil, err
	}
	return flag, nil
}
