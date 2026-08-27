package model

import (
	"context"

	configmodel "controlplane/internal/model/config"
)

// Repository is the persistence seam for the dashboard's aggregate counts and
// recent-activity queries, spanning both the config and auth models.
type Repository interface {
	CountApplications(ctx context.Context) (int64, error)
	CountEnvironments(ctx context.Context) (int64, error)
	CountConfigEntries(ctx context.Context, isSecret bool) (int64, error)
	CountActiveFeatureFlags(ctx context.Context) (int64, error)
	CountServiceClients(ctx context.Context) (int64, error)
	RecentConfigEntries(ctx context.Context, limit int) ([]configmodel.ConfigEntry, error)
}
