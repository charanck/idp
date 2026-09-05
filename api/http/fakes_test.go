package http_test

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"controlplane/internal/config"
	authmodel "controlplane/internal/model/auth"
	configmodel "controlplane/internal/model/config"
)

type fakeAPIKeyAuthenticator struct {
	client *authmodel.ServiceClient
	err    error
}

func (f *fakeAPIKeyAuthenticator) AuthenticateServiceAPIKey(ctx context.Context, apiKey string) (*authmodel.ServiceClient, error) {
	return f.client, f.err
}

type fakeRateLimiter struct {
	limited bool
	err     error
}

func (f *fakeRateLimiter) IsRateLimited(ctx context.Context, key, clientIP string, limit int, window time.Duration) (bool, error) {
	return f.limited, f.err
}

type fakeUsageCounter struct {
	calls int
	err   error
}

func (f *fakeUsageCounter) Incr(ctx context.Context) error {
	f.calls++
	return f.err
}

type fakeConfigLister struct {
	configs []config.ClientConfig
	err     error
}

func (f *fakeConfigLister) ListConfigsForClient(ctx context.Context, service, environment, clientEncryptionKey string) ([]config.ClientConfig, error) {
	return f.configs, f.err
}

type fakeFeatureFlagLister struct {
	flags []configmodel.FeatureFlag
	err   error
}

func (f *fakeFeatureFlagLister) ListFlags(ctx context.Context, service, environment string) ([]configmodel.FeatureFlag, error) {
	return f.flags, f.err
}

type fakeApplicationFinder struct {
	apps map[string]*configmodel.Application
	err  error
}

func (f *fakeApplicationFinder) GetApplicationByName(ctx context.Context, name string) (*configmodel.Application, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.apps[name], nil
}

type fakeClientApplicationScoper struct {
	allowedIDs []uuid.UUID
	err        error
}

func (f *fakeClientApplicationScoper) ServiceClientApplicationIDs(ctx context.Context, id uuid.UUID) ([]uuid.UUID, error) {
	return f.allowedIDs, f.err
}

var errFakeService = errors.New("fake service error")
