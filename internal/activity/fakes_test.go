package activity_test

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	model "controlplane/internal/model/activity"
)

type fakeActivityRepository struct {
	mu         sync.Mutex
	activities []model.Activity
}

func newFakeActivityRepository() *fakeActivityRepository {
	return &fakeActivityRepository{}
}

func (f *fakeActivityRepository) Create(ctx context.Context, a *model.Activity) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	if a.Timestamp.IsZero() {
		a.Timestamp = time.Now()
	}
	f.activities = append(f.activities, *a)
	return nil
}

func (f *fakeActivityRepository) List(ctx context.Context, filter model.ListFilter) ([]model.Activity, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	var out []model.Activity
	for _, a := range f.activities {
		if filter.Resource != "" && a.Resource != filter.Resource {
			continue
		}
		if filter.Type != "" && a.Type != filter.Type {
			continue
		}
		if filter.UserLike != "" {
			if a.UserEmail == nil || !strings.Contains(strings.ToLower(*a.UserEmail), strings.ToLower(filter.UserLike)) {
				continue
			}
		}
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Timestamp.After(out[j].Timestamp) })
	return out, nil
}

func (f *fakeActivityRepository) DistinctResources(ctx context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	seen := map[string]bool{}
	for _, a := range f.activities {
		seen[a.Resource] = true
	}
	var out []string
	for r := range seen {
		out = append(out, r)
	}
	sort.Strings(out)
	return out, nil
}

func (f *fakeActivityRepository) DistinctTypes(ctx context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	seen := map[string]bool{}
	for _, a := range f.activities {
		seen[a.Type] = true
	}
	var out []string
	for t := range seen {
		out = append(out, t)
	}
	sort.Strings(out)
	return out, nil
}

var _ model.Repository = (*fakeActivityRepository)(nil)
