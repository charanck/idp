package repository

import (
	"context"
	"errors"

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

func (r *gormApplicationRepository) List(ctx context.Context, q string, allowedIDs []uuid.UUID) ([]model.Application, error) {
	query := r.db.WithContext(ctx).Order("name")
	if len(allowedIDs) > 0 {
		query = query.Where("id IN ?", allowedIDs)
	}
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

// GetOrCreate returns the Application named name, creating it if it doesn't
// exist yet. A per-name advisory lock (shared with getOrCreateScopeTx's
// "config-upsert" lock only by hashing scheme, not by key - Applications
// aren't looked up by that lock, so this uses its own namespaced key) closes
// the find-then-create race between concurrent first-time callers.
func (r *gormApplicationRepository) GetOrCreate(ctx context.Context, name string) (*model.Application, error) {
	var app model.Application
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", advisoryLockKey("application-get-or-create", name)).Error; err != nil {
			return err
		}
		if err := tx.Where("name = ?", name).First(&app).Error; err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			app = model.Application{Name: name}
			return tx.Create(&app).Error
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &app, nil
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
