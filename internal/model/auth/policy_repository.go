package model

import (
	"context"
	"time"
)

// Policy is a singleton (id=1) settings row for login/registration policies,
// extensible with more columns later without a schema redesign.
type Policy struct {
	ID                             int16     `gorm:"column:id;primaryKey"`
	SelfRegistrationAllowedDomains string    `gorm:"column:self_registration_allowed_domains"`
	CreatedAt                      time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt                      time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

func (Policy) TableName() string { return "policies" }

// PolicyRepository is the persistence boundary for the singleton Policy row.
type PolicyRepository interface {
	Get(ctx context.Context) (*Policy, error)
	Update(ctx context.Context, policy *Policy) error
}
