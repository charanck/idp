package config

import (
	"context"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// EnvironmentRepository is the persistence boundary for Environment rows.
// Not-found lookups return gorm's raw error (including gorm.ErrRecordNotFound)
// rather than swallowing it - callers decide how to translate that.
type EnvironmentRepository interface {
	List(ctx context.Context, filter ListEnvironmentsFilter) ([]Environment, error)
	ListByApplicationID(ctx context.Context, applicationID uuid.UUID) ([]Environment, error)
	FindByID(ctx context.Context, id uuid.UUID) (*Environment, error)
	FindByIDWithApplication(ctx context.Context, id uuid.UUID) (*Environment, error)
	FindByApplicationAndName(ctx context.Context, applicationID uuid.UUID, name string) (*Environment, error)
	Create(ctx context.Context, env *Environment) error
	Update(ctx context.Context, env *Environment) error
	Delete(ctx context.Context, env *Environment) error
}

type gormEnvironmentRepository struct {
	db *gorm.DB
}

func NewEnvironmentRepository(db *gorm.DB) *gormEnvironmentRepository {
	return &gormEnvironmentRepository{db: db}
}

func (r *gormEnvironmentRepository) List(ctx context.Context, filter ListEnvironmentsFilter) ([]Environment, error) {
	query := r.db.WithContext(ctx).Preload("Application").Order("name")
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

// ListByApplicationID lists every environment for an application, ordered by
// name (both today's admin dropdown listing and CreateFlag's "all
// environments" fan-out rely on this).
func (r *gormEnvironmentRepository) ListByApplicationID(ctx context.Context, applicationID uuid.UUID) ([]Environment, error) {
	var envs []Environment
	if err := r.db.WithContext(ctx).Where("application_id = ?", applicationID).Order("name").Find(&envs).Error; err != nil {
		return nil, err
	}
	return envs, nil
}

func (r *gormEnvironmentRepository) FindByID(ctx context.Context, id uuid.UUID) (*Environment, error) {
	var env Environment
	if err := r.db.WithContext(ctx).First(&env, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &env, nil
}

func (r *gormEnvironmentRepository) FindByIDWithApplication(ctx context.Context, id uuid.UUID) (*Environment, error) {
	var env Environment
	if err := r.db.WithContext(ctx).Preload("Application").First(&env, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &env, nil
}

func (r *gormEnvironmentRepository) FindByApplicationAndName(ctx context.Context, applicationID uuid.UUID, name string) (*Environment, error) {
	var env Environment
	err := r.db.WithContext(ctx).Where("application_id = ? AND name = ?", applicationID, name).First(&env).Error
	if err != nil {
		return nil, err
	}
	return &env, nil
}

func (r *gormEnvironmentRepository) Create(ctx context.Context, env *Environment) error {
	return r.db.WithContext(ctx).Create(env).Error
}

func (r *gormEnvironmentRepository) Update(ctx context.Context, env *Environment) error {
	return r.db.WithContext(ctx).Save(env).Error
}

func (r *gormEnvironmentRepository) Delete(ctx context.Context, env *Environment) error {
	return r.db.WithContext(ctx).Delete(env).Error
}
