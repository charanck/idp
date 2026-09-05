package repository

import (
	"context"

	"gorm.io/gorm"

	model "controlplane/internal/model/auth"
)

type gormPolicyRepository struct {
	db *gorm.DB
}

func NewPolicyRepository(db *gorm.DB) *gormPolicyRepository {
	return &gormPolicyRepository{db: db}
}

var _ model.PolicyRepository = (*gormPolicyRepository)(nil)

func (r *gormPolicyRepository) Get(ctx context.Context) (*model.Policy, error) {
	var policy model.Policy
	if err := r.db.WithContext(ctx).First(&policy, "id = ?", 1).Error; err != nil {
		return nil, err
	}
	return &policy, nil
}

func (r *gormPolicyRepository) Update(ctx context.Context, policy *model.Policy) error {
	policy.ID = 1
	return r.db.WithContext(ctx).Save(policy).Error
}
