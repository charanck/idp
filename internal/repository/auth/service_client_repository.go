package repository

import (
	"context"

	"github.com/google/uuid"
	"gorm.io/gorm"

	model "controlplane/internal/model/auth"
)

type gormServiceClientRepository struct {
	db *gorm.DB
}

func NewServiceClientRepository(db *gorm.DB) *gormServiceClientRepository {
	return &gormServiceClientRepository{db: db}
}

var _ model.ServiceClientRepository = (*gormServiceClientRepository)(nil)

func (r *gormServiceClientRepository) FindByName(ctx context.Context, name string) (*model.ServiceClient, error) {
	var client model.ServiceClient
	if err := r.db.WithContext(ctx).Where("name = ?", name).First(&client).Error; err != nil {
		return nil, err
	}
	return &client, nil
}

func (r *gormServiceClientRepository) FindByAPIKeyIDActive(ctx context.Context, apiKeyID string) (*model.ServiceClient, error) {
	var client model.ServiceClient
	err := r.db.WithContext(ctx).Where("is_active = ? AND api_key_id = ?", true, apiKeyID).First(&client).Error
	if err != nil {
		return nil, err
	}
	return &client, nil
}

func (r *gormServiceClientRepository) FindByID(ctx context.Context, id uuid.UUID) (*model.ServiceClient, error) {
	var client model.ServiceClient
	if err := r.db.WithContext(ctx).First(&client, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &client, nil
}

func (r *gormServiceClientRepository) FindActiveByID(ctx context.Context, id uuid.UUID) (*model.ServiceClient, error) {
	var client model.ServiceClient
	err := r.db.WithContext(ctx).Where("id = ? AND is_active = ?", id, true).First(&client).Error
	if err != nil {
		return nil, err
	}
	return &client, nil
}

func (r *gormServiceClientRepository) Create(ctx context.Context, client *model.ServiceClient) error {
	return r.db.WithContext(ctx).Create(client).Error
}

func (r *gormServiceClientRepository) Update(ctx context.Context, client *model.ServiceClient) error {
	return r.db.WithContext(ctx).Save(client).Error
}

func (r *gormServiceClientRepository) Delete(ctx context.Context, client *model.ServiceClient) error {
	return r.db.WithContext(ctx).Delete(client).Error
}

func (r *gormServiceClientRepository) List(ctx context.Context, q string, isActive *bool) ([]model.ServiceClient, error) {
	query := r.db.WithContext(ctx).Order("created_at DESC")
	if q != "" {
		query = query.Where("name ILIKE ?", "%"+q+"%")
	}
	if isActive != nil {
		query = query.Where("is_active = ?", *isActive)
	}
	var clients []model.ServiceClient
	if err := query.Find(&clients).Error; err != nil {
		return nil, err
	}
	return clients, nil
}
