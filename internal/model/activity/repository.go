package model

import "context"

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
}
