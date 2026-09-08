package model

import (
	"context"

	"github.com/google/uuid"
)

// ApplicationDomainRepository is the persistence boundary for the
// application_domains join table: the hostnames a reverse proxy may forward
// (X-Forwarded-Host) that map to a given Application, used by the
// forward-auth verify endpoint to resolve which Application's Group
// allow-list gates a request. One Application may have several hosts; a
// host maps to at most one Application (enforced by a unique constraint).
type ApplicationDomainRepository interface {
	ListHosts(ctx context.Context, applicationID uuid.UUID) ([]string, error)
	SetHosts(ctx context.Context, applicationID uuid.UUID, hosts []string) error

	// FindApplicationIDByHost returns the Application ID mapped to host, or
	// uuid.Nil with no error if no Application claims that host.
	FindApplicationIDByHost(ctx context.Context, host string) (uuid.UUID, error)
}
