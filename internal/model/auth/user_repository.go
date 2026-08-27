package model

import (
	"context"

	"github.com/google/uuid"
)

// UserRepository is the persistence boundary for User. Methods return gorm's
// raw error (including gorm.ErrRecordNotFound) rather than swallowing "not
// found" into (nil, nil) - callers decide how to translate that.
type UserRepository interface {
	FindByEmail(ctx context.Context, email string) (*User, error)
	FindActiveByEmail(ctx context.Context, email string) (*User, error)
	FindByID(ctx context.Context, id uuid.UUID) (*User, error)
	FindActiveByID(ctx context.Context, id uuid.UUID) (*User, error)
	Create(ctx context.Context, user *User) error
	Update(ctx context.Context, user *User) error
	UpdatePassword(ctx context.Context, id uuid.UUID, hashedPassword string) error
	Delete(ctx context.Context, user *User) error
	List(ctx context.Context, q string, isStaff *bool) ([]User, error)
	CountByUsername(ctx context.Context, username string) (int64, error)
}
