package api_test

import (
	"context"
	"errors"
	"time"

	"controlplane/internal/auth"
	"controlplane/internal/config"
)

type fakeAPIKeyAuthenticator struct {
	client *auth.ServiceClient
	err    error
}

func (f *fakeAPIKeyAuthenticator) AuthenticateServiceAPIKey(ctx context.Context, apiKey string) (*auth.ServiceClient, error) {
	return f.client, f.err
}

type fakeRateLimiter struct {
	limited bool
	err     error
}

func (f *fakeRateLimiter) IsRateLimited(ctx context.Context, key, clientIP string, limit int, window time.Duration) (bool, error) {
	return f.limited, f.err
}

type fakeConfigLister struct {
	configs []config.ClientConfig
	err     error
}

func (f *fakeConfigLister) ListConfigsForClient(ctx context.Context, service, environment, clientEncryptionKey string) ([]config.ClientConfig, error) {
	return f.configs, f.err
}

type fakeFeatureFlagLister struct {
	flags []config.FeatureFlag
	err   error
}

func (f *fakeFeatureFlagLister) ListFlags(ctx context.Context, service, environment string) ([]config.FeatureFlag, error) {
	return f.flags, f.err
}

var errFakeService = errors.New("fake service error")
