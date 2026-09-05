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

func (r *gormServiceClientRepository) ListApplicationIDs(ctx context.Context, clientID uuid.UUID) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	err := r.db.WithContext(ctx).Table("service_client_applications").
		Where("service_client_id = ?", clientID).
		Pluck("application_id", &ids).Error
	if err != nil {
		return nil, err
	}
	return ids, nil
}

func (r *gormServiceClientRepository) SetApplications(ctx context.Context, clientID uuid.UUID, applicationIDs []uuid.UUID) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Table("service_client_applications").Where("service_client_id = ?", clientID).Delete(nil).Error; err != nil {
			return err
		}
		for _, appID := range applicationIDs {
			if err := tx.Table("service_client_applications").Create(map[string]any{
				"service_client_id": clientID,
				"application_id":    appID,
			}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *gormServiceClientRepository) ListRedirectURIs(ctx context.Context, clientID uuid.UUID) ([]string, error) {
	var uris []string
	err := r.db.WithContext(ctx).Table("service_client_redirect_uris").
		Where("service_client_id = ?", clientID).
		Pluck("redirect_uri", &uris).Error
	if err != nil {
		return nil, err
	}
	return uris, nil
}

func (r *gormServiceClientRepository) SetRedirectURIs(ctx context.Context, clientID uuid.UUID, redirectURIs []string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Table("service_client_redirect_uris").Where("service_client_id = ?", clientID).Delete(nil).Error; err != nil {
			return err
		}
		for _, uri := range redirectURIs {
			if err := tx.Table("service_client_redirect_uris").Create(map[string]any{
				"id":                uuid.New(),
				"service_client_id": clientID,
				"redirect_uri":      uri,
			}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *gormServiceClientRepository) ListAllowedGroupIDs(ctx context.Context, clientID uuid.UUID) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	err := r.db.WithContext(ctx).Table("service_client_allowed_groups").
		Where("service_client_id = ?", clientID).
		Pluck("group_id", &ids).Error
	if err != nil {
		return nil, err
	}
	return ids, nil
}

func (r *gormServiceClientRepository) SetAllowedGroups(ctx context.Context, clientID uuid.UUID, groupIDs []uuid.UUID) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Table("service_client_allowed_groups").Where("service_client_id = ?", clientID).Delete(nil).Error; err != nil {
			return err
		}
		for _, groupID := range groupIDs {
			if err := tx.Table("service_client_allowed_groups").Create(map[string]any{
				"service_client_id": clientID,
				"group_id":          groupID,
			}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
