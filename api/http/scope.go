package http

import (
	"context"
	"net/http"
	"slices"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	configmodel "controlplane/internal/model/config"
)

// ApplicationFinder resolves an Application by exact name. Satisfied by both
// *config.ConfigService and *config.FeatureFlagService.
type ApplicationFinder interface {
	GetApplicationByName(ctx context.Context, name string) (*configmodel.Application, error)
}

// ClientApplicationScoper returns a service client's config/flag S2S read
// scope (empty = unrestricted). Satisfied by *auth.AuthService.
type ClientApplicationScoper interface {
	ServiceClientApplicationIDs(ctx context.Context, id uuid.UUID) ([]uuid.UUID, error)
}

// checkApplicationScope returns a 404 if the client is scoped to a non-empty
// set of Applications and service isn't one of them - indistinguishable from
// an unrecognized service name. A service name that doesn't resolve to any
// Application at all is left to the caller's normal empty-result handling.
func checkApplicationScope(ctx context.Context, apps ApplicationFinder, scoper ClientApplicationScoper, clientID uuid.UUID, service string) error {
	allowedIDs, err := scoper.ServiceClientApplicationIDs(ctx, clientID)
	if err != nil {
		return err
	}
	if len(allowedIDs) == 0 {
		return nil
	}

	app, err := apps.GetApplicationByName(ctx, service)
	if err != nil {
		return err
	}
	if app == nil {
		return nil
	}

	if slices.Contains(allowedIDs, app.ID) {
		return nil
	}
	return echo.NewHTTPError(http.StatusNotFound)
}
