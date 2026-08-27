package repository

import (
	"context"

	"github.com/google/uuid"
	"gorm.io/gorm"

	model "controlplane/internal/model/config"
)

type gormApplicationRepository struct {
	db *gorm.DB
}

func NewApplicationRepository(db *gorm.DB) *gormApplicationRepository {
	return &gormApplicationRepository{db: db}
}

func (r *gormApplicationRepository) List(ctx context.Context, q string) ([]model.Application, error) {
	query := r.db.WithContext(ctx).Order("name")
	if q != "" {
		query = query.Where("name ILIKE ?", "%"+q+"%")
	}
	var apps []model.Application
	if err := query.Find(&apps).Error; err != nil {
		return nil, err
	}
	return apps, nil
}

func (r *gormApplicationRepository) FindByID(ctx context.Context, id uuid.UUID) (*model.Application, error) {
	var app model.Application
	if err := r.db.WithContext(ctx).First(&app, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &app, nil
}

func (r *gormApplicationRepository) FindByName(ctx context.Context, name string) (*model.Application, error) {
	var app model.Application
	if err := r.db.WithContext(ctx).Where("name = ?", name).First(&app).Error; err != nil {
		return nil, err
	}
	return &app, nil
}

func (r *gormApplicationRepository) Create(ctx context.Context, app *model.Application) error {
	return r.db.WithContext(ctx).Create(app).Error
}

func (r *gormApplicationRepository) Update(ctx context.Context, app *model.Application) error {
	return r.db.WithContext(ctx).Save(app).Error
}

func (r *gormApplicationRepository) Delete(ctx context.Context, app *model.Application) error {
	return r.db.WithContext(ctx).Delete(app).Error
}

func (r *gormApplicationRepository) ListDistinctNames(ctx context.Context) ([]string, error) {
	var names []string
	err := r.db.WithContext(ctx).Model(&model.Application{}).Distinct().Order("name").Pluck("name", &names).Error
	if err != nil {
		return nil, err
	}
	return names, nil
}

var _ model.ApplicationRepository = (*gormApplicationRepository)(nil)
