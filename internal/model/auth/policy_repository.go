package model

import (
	"context"
	"time"
)

// Policy is a singleton (id=1) settings row for login/registration policies,
// extensible with more columns later without a schema redesign.
//
// Zero-value conventions (matching SelfRegistrationAllowedDomains' existing
// "empty = unrestricted" pattern): PasswordMaxAgeDays = 0 means passwords
// never expire; MaxFailedLoginAttempts = 0 disables account lockout;
// SessionIdleTimeoutMinutes = 0 disables the idle timeout; LoginIPAllowlist
// empty means unrestricted.
type Policy struct {
	ID                             int16     `gorm:"column:id;primaryKey"`
	SelfRegistrationAllowedDomains string    `gorm:"column:self_registration_allowed_domains"`
	PasswordMinLength              int       `gorm:"column:password_min_length"`
	PasswordRequireUpper           bool      `gorm:"column:password_require_upper"`
	PasswordRequireLower           bool      `gorm:"column:password_require_lower"`
	PasswordRequireDigit           bool      `gorm:"column:password_require_digit"`
	PasswordRequireSymbol          bool      `gorm:"column:password_require_symbol"`
	PasswordMaxAgeDays             int       `gorm:"column:password_max_age_days"`
	MaxFailedLoginAttempts         int       `gorm:"column:max_failed_login_attempts"`
	LockoutDurationMinutes         int       `gorm:"column:lockout_duration_minutes"`
	SessionIdleTimeoutMinutes      int       `gorm:"column:session_idle_timeout_minutes"`
	SSOOnly                        bool      `gorm:"column:sso_only"`
	LoginIPAllowlist               string    `gorm:"column:login_ip_allowlist"`
	CreatedAt                      time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt                      time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

func (Policy) TableName() string { return "policies" }

// PolicyRepository is the persistence boundary for the singleton Policy row.
type PolicyRepository interface {
	Get(ctx context.Context) (*Policy, error)
	Update(ctx context.Context, policy *Policy) error
}
