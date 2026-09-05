package repository

import (
	"context"

	"github.com/google/uuid"
	"gorm.io/gorm"

	model "controlplane/internal/model/auth"
)

type gormGroupRepository struct {
	db *gorm.DB
}

func NewGroupRepository(db *gorm.DB) *gormGroupRepository {
	return &gormGroupRepository{db: db}
}

var _ model.GroupRepository = (*gormGroupRepository)(nil)

func (r *gormGroupRepository) List(ctx context.Context, q string) ([]model.Group, error) {
	query := r.db.WithContext(ctx).Order("name ASC")
	if q != "" {
		query = query.Where("name ILIKE ?", "%"+q+"%")
	}
	var groups []model.Group
	if err := query.Find(&groups).Error; err != nil {
		return nil, err
	}
	return groups, nil
}

func (r *gormGroupRepository) FindByID(ctx context.Context, id uuid.UUID) (*model.Group, error) {
	var group model.Group
	if err := r.db.WithContext(ctx).First(&group, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &group, nil
}

func (r *gormGroupRepository) Create(ctx context.Context, group *model.Group) error {
	return r.db.WithContext(ctx).Create(group).Error
}

func (r *gormGroupRepository) Update(ctx context.Context, group *model.Group) error {
	return r.db.WithContext(ctx).Save(group).Error
}

func (r *gormGroupRepository) Delete(ctx context.Context, group *model.Group) error {
	return r.db.WithContext(ctx).Delete(group).Error
}

func (r *gormGroupRepository) ListApplicationIDs(ctx context.Context, groupID uuid.UUID) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	err := r.db.WithContext(ctx).Table("group_applications").
		Where("group_id = ?", groupID).
		Pluck("application_id", &ids).Error
	if err != nil {
		return nil, err
	}
	return ids, nil
}

func (r *gormGroupRepository) SetApplications(ctx context.Context, groupID uuid.UUID, applicationIDs []uuid.UUID) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Table("group_applications").Where("group_id = ?", groupID).Delete(nil).Error; err != nil {
			return err
		}
		for _, appID := range applicationIDs {
			if err := tx.Table("group_applications").Create(map[string]any{
				"group_id":       groupID,
				"application_id": appID,
			}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *gormGroupRepository) ListByUserID(ctx context.Context, userID uuid.UUID) ([]model.Group, error) {
	var groups []model.Group
	err := r.db.WithContext(ctx).
		Joins("JOIN user_groups ON user_groups.group_id = groups.id").
		Where("user_groups.user_id = ?", userID).
		Find(&groups).Error
	if err != nil {
		return nil, err
	}
	return groups, nil
}

func (r *gormGroupRepository) SetUserGroups(ctx context.Context, userID uuid.UUID, groupIDs []uuid.UUID) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Table("user_groups").Where("user_id = ?", userID).Delete(nil).Error; err != nil {
			return err
		}
		for _, groupID := range groupIDs {
			if err := tx.Table("user_groups").Create(map[string]any{
				"user_id":  userID,
				"group_id": groupID,
			}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
