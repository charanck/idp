package config

import (
	"context"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ApplicationRepository is the persistence boundary for Application rows.
// Not-found lookups return gorm's raw error (including gorm.ErrRecordNotFound)
// rather than swallowing it - callers decide how to translate that.
type ApplicationRepository interface {
	List(ctx context.Context, q string) ([]Application, error)
	FindByID(ctx context.Context, id uuid.UUID) (*Application, error)
	FindByName(ctx context.Context, name string) (*Application, error)
	Create(ctx context.Context, app *Application) error
	Update(ctx context.Context, app *Application) error
	Delete(ctx context.Context, app *Application) error
	ListDistinctNames(ctx context.Context) ([]string, error)
}

type gormApplicationRepository struct {
	db *gorm.DB
}

func NewApplicationRepository(db *gorm.DB) *gormApplicationRepository {
	return &gormApplicationRepository{db: db}
}

func (r *gormApplicationRepository) List(ctx context.Context, q string) ([]Application, error) {
	query := r.db.WithContext(ctx).Order("name")
	if q != "" {
		query = query.Where("name ILIKE ?", "%"+q+"%")
	}
	var apps []Application
	if err := query.Find(&apps).Error; err != nil {
		return nil, err
	}
	return apps, nil
}

func (r *gormApplicationRepository) FindByID(ctx context.Context, id uuid.UUID) (*Application, error) {
	var app Application
	if err := r.db.WithContext(ctx).First(&app, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &app, nil
}

func (r *gormApplicationRepository) FindByName(ctx context.Context, name string) (*Application, error) {
	var app Application
	if err := r.db.WithContext(ctx).Where("name = ?", name).First(&app).Error; err != nil {
		return nil, err
	}
	return &app, nil
}

func (r *gormApplicationRepository) Create(ctx context.Context, app *Application) error {
	return r.db.WithContext(ctx).Create(app).Error
}

func (r *gormApplicationRepository) Update(ctx context.Context, app *Application) error {
	return r.db.WithContext(ctx).Save(app).Error
}

func (r *gormApplicationRepository) Delete(ctx context.Context, app *Application) error {
	return r.db.WithContext(ctx).Delete(app).Error
}

func (r *gormApplicationRepository) ListDistinctNames(ctx context.Context) ([]string, error) {
	var names []string
	err := r.db.WithContext(ctx).Model(&Application{}).Distinct().Order("name").Pluck("name", &names).Error
	if err != nil {
		return nil, err
	}
	return names, nil
}
