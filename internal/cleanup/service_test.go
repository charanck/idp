package cleanup_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"controlplane/internal/cleanup"
)

type fakePruner struct {
	calledBefore time.Time
	called       bool
	err          error
}

func (f *fakePruner) DeleteOlderThan(ctx context.Context, before time.Time) error {
	f.called = true
	f.calledBefore = before
	return f.err
}

func (f *fakePruner) DeleteExpired(ctx context.Context, before time.Time) error {
	f.called = true
	f.calledBefore = before
	return f.err
}

func TestService_Run_PrunesAllThreeWithCorrectCutoffs(t *testing.T) {
	notifications := &fakePruner{}
	activities := &fakePruner{}
	authCodes := &fakePruner{}
	svc := cleanup.NewService(notifications, activities, authCodes)

	now := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	if err := svc.Run(context.Background(), now); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !notifications.called {
		t.Fatal("expected notifications to be pruned")
	}
	if want := now.Add(-cleanup.NotificationRetention); !notifications.calledBefore.Equal(want) {
		t.Fatalf("notification cutoff = %v, want %v", notifications.calledBefore, want)
	}

	if !activities.called {
		t.Fatal("expected activities to be pruned")
	}
	if want := now.Add(-cleanup.ActivityRetention); !activities.calledBefore.Equal(want) {
		t.Fatalf("activity cutoff = %v, want %v", activities.calledBefore, want)
	}

	if !authCodes.called {
		t.Fatal("expected auth codes to be pruned")
	}
	if !authCodes.calledBefore.Equal(now) {
		t.Fatalf("auth code cutoff = %v, want %v", authCodes.calledBefore, now)
	}
}

func TestService_Run_StopsOnFirstError(t *testing.T) {
	notifications := &fakePruner{err: errors.New("boom")}
	activities := &fakePruner{}
	authCodes := &fakePruner{}
	svc := cleanup.NewService(notifications, activities, authCodes)

	if err := svc.Run(context.Background(), time.Now()); err == nil {
		t.Fatal("expected error to propagate")
	}
	if activities.called {
		t.Fatal("activities should not be pruned once notifications pruning fails")
	}
}
