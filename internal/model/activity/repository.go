package model

import (
	"context"
	"time"
)

// ListFilter filters Repository.List.
type ListFilter struct {
	Resource string
	Type     string
	UserLike string // case-insensitive substring match on user_email
}

// Repository is the persistence seam for the append-only activity log.
type Repository interface {
	Create(ctx context.Context, activity *Activity) error
	List(ctx context.Context, filter ListFilter) ([]Activity, error)
	DistinctResources(ctx context.Context) ([]string, error)
	DistinctTypes(ctx context.Context) ([]string, error)
	// DeleteOlderThan hard-deletes activity entries recorded before the
	// given time, for the monthly retention cleanup (internal/cleanup).
	DeleteOlderThan(ctx context.Context, before time.Time) error
}
