package model

import (
	"context"

	"github.com/google/uuid"
)

// ApplicationRepository is the persistence boundary for Application rows.
// Not-found lookups return gorm's raw error (including gorm.ErrRecordNotFound)
// rather than swallowing it - callers decide how to translate that.
type ApplicationRepository interface {
	List(ctx context.Context, q string) ([]Application, error)
	FindByID(ctx context.Context, id uuid.UUID) (*Application, error)
	FindByName(ctx context.Context, name string) (*Application, error)
	Create(ctx context.Context, app *Application) error
	Update(ctx context.Context, app *Application) error
	Delete(ctx context.Context, app *Application) error
	ListDistinctNames(ctx context.Context) ([]string, error)
}
