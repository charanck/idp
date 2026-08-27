package repository

import (
	"context"

	"github.com/google/uuid"
	"gorm.io/gorm"

	model "controlplane/internal/model/auth"
)

type gormUserRepository struct {
	db *gorm.DB
}

func NewUserRepository(db *gorm.DB) *gormUserRepository {
	return &gormUserRepository{db: db}
}

var _ model.UserRepository = (*gormUserRepository)(nil)

func (r *gormUserRepository) FindByEmail(ctx context.Context, email string) (*model.User, error) {
	var user model.User
	if err := r.db.WithContext(ctx).Where("email = ?", email).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *gormUserRepository) FindActiveByEmail(ctx context.Context, email string) (*model.User, error) {
	var user model.User
	if err := r.db.WithContext(ctx).Where("email = ? AND is_active = ?", email, true).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *gormUserRepository) FindByID(ctx context.Context, id uuid.UUID) (*model.User, error) {
	var user model.User
	if err := r.db.WithContext(ctx).First(&user, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *gormUserRepository) FindActiveByID(ctx context.Context, id uuid.UUID) (*model.User, error) {
	var user model.User
	if err := r.db.WithContext(ctx).Where("id = ? AND is_active = ?", id, true).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *gormUserRepository) Create(ctx context.Context, user *model.User) error {
	return r.db.WithContext(ctx).Create(user).Error
}

func (r *gormUserRepository) Update(ctx context.Context, user *model.User) error {
	return r.db.WithContext(ctx).Save(user).Error
}

func (r *gormUserRepository) UpdatePassword(ctx context.Context, id uuid.UUID, hashedPassword string) error {
	return r.db.WithContext(ctx).Model(&model.User{}).Where("id = ?", id).Updates(map[string]any{
		"password":             hashedPassword,
		"force_password_reset": false,
	}).Error
}

func (r *gormUserRepository) Delete(ctx context.Context, user *model.User) error {
	return r.db.WithContext(ctx).Delete(user).Error
}

func (r *gormUserRepository) List(ctx context.Context, q string, isStaff *bool) ([]model.User, error) {
	query := r.db.WithContext(ctx).Order("created_at DESC")
	if q != "" {
		query = query.Where("email ILIKE ?", "%"+q+"%")
	}
	if isStaff != nil {
		query = query.Where("is_staff = ?", *isStaff)
	}
	var users []model.User
	if err := query.Find(&users).Error; err != nil {
		return nil, err
	}
	return users, nil
}

func (r *gormUserRepository) CountByUsername(ctx context.Context, username string) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.User{}).Where("username = ?", username).Count(&count).Error
	return count, err
}
