package auth_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"controlplane/internal/auth"
)

// memCache is a minimal shared in-memory cache.Cache standing in for Redis, so
// two AuthService instances can act as two replicas sharing one cache.
type memCache struct {
	mu       sync.Mutex
	versions map[string]int64
	values   map[string]string
}

func newMemCache() *memCache {
	return &memCache{versions: map[string]int64{}, values: map[string]string{}}
}

func (c *memCache) Get(_ context.Context, key string) (string, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.values[key]
	return v, ok, nil
}
func (c *memCache) Set(_ context.Context, key, value string, _ time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.values[key] = value
	return nil
}
func (c *memCache) GetVersion(_ context.Context, key string) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if v, ok := c.versions[key]; ok {
		return v, nil
	}
	return 1, nil
}
func (c *memCache) BumpVersion(_ context.Context, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if v, ok := c.versions[key]; ok {
		c.versions[key] = v + 1
	} else {
		c.versions[key] = 2
	}
	return nil
}

// A key verified (and cached) on one replica must stop working on that replica
// as soon as ANOTHER replica changes the client, not after the cache TTL.
func TestAuthenticateServiceAPIKey_CacheInvalidatedAcrossReplicas(t *testing.T) {
	clients := newFakeServiceClientRepository()
	shared := newMemCache()
	newReplica := func() *auth.AuthService {
		return auth.NewAuthService(newFakeUserRepository(), clients, newFakeGroupRepository(),
			newFakePolicyRepository(), newFakeBrandingRepository(), shared, time.Minute)
	}
	replicaA, replicaB := newReplica(), newReplica()
	ctx := context.Background()

	creds, err := replicaA.CreateServiceClient(ctx, "cached-svc")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ { // second call is served from replica B's cache
		if c, err := replicaB.AuthenticateServiceAPIKey(ctx, creds.APIKey); err != nil || c == nil {
			t.Fatalf("auth %d on B: client=%v err=%v", i, c, err)
		}
	}

	if _, err := replicaA.ToggleServiceClient(ctx, creds.Client.ID); err != nil { // disables it
		t.Fatal(err)
	}
	if c, err := replicaB.AuthenticateServiceAPIKey(ctx, creds.APIKey); err != nil || c != nil {
		t.Fatalf("disabled client still authenticates on replica B: client=%v err=%v", c, err)
	}
}
