package activity_test

import (
	"context"
	"testing"

	"controlplane/internal/activity"
	model "controlplane/internal/model/activity"
)

func TestLog_AttachesRequestInfoFromContext(t *testing.T) {
	repo := newFakeActivityRepository()
	logger := activity.NewLogger(repo)

	ctx := activity.WithRequestInfo(context.Background(), activity.RequestInfo{
		IPAddress: "10.0.0.1",
		UserEmail: "admin@example.com",
	})

	logger.LogCreate(ctx, "config", "abc-123", "API_URL", nil)

	rows, err := repo.List(context.Background(), model.ListFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 activity row, got %d", len(rows))
	}
	row := rows[0]
	if row.UserEmail == nil || *row.UserEmail != "admin@example.com" {
		t.Fatalf("expected user email from context, got %v", row.UserEmail)
	}
	if row.IPAddress == nil || *row.IPAddress != "10.0.0.1" {
		t.Fatalf("expected ip address from context, got %v", row.IPAddress)
	}
	if row.ResourceID != "abc-123" || row.Resource != "config" {
		t.Fatalf("unexpected resource fields: %+v", row)
	}
}

func TestList_FiltersByResourceAndType(t *testing.T) {
	repo := newFakeActivityRepository()
	logger := activity.NewLogger(repo)
	ctx := context.Background()

	logger.LogCreate(ctx, "config", "1", "A", nil)
	logger.LogUpdate(ctx, "config", "1", "A", nil)
	logger.LogCreate(ctx, "flag", "2", "B", nil)

	configRows, err := repo.List(ctx, model.ListFilter{Resource: "config"})
	if err != nil {
		t.Fatalf("List by resource: %v", err)
	}
	if len(configRows) != 2 {
		t.Fatalf("expected 2 config rows, got %d", len(configRows))
	}

	createRows, err := repo.List(ctx, model.ListFilter{Type: "create"})
	if err != nil {
		t.Fatalf("List by type: %v", err)
	}
	if len(createRows) != 2 {
		t.Fatalf("expected 2 create rows, got %d", len(createRows))
	}

	combined, err := repo.List(ctx, model.ListFilter{Resource: "config", Type: "update"})
	if err != nil {
		t.Fatalf("List by resource+type: %v", err)
	}
	if len(combined) != 1 {
		t.Fatalf("expected 1 combined row, got %d", len(combined))
	}
}

func TestDistinctResourcesAndTypes_Deduplicate(t *testing.T) {
	repo := newFakeActivityRepository()
	logger := activity.NewLogger(repo)
	ctx := context.Background()

	logger.LogCreate(ctx, "config", "1", "A", nil)
	logger.LogUpdate(ctx, "config", "1", "A", nil)
	logger.LogCreate(ctx, "user", "2", "B", nil)

	resources, err := logger.DistinctResources(ctx)
	if err != nil {
		t.Fatalf("DistinctResources: %v", err)
	}
	if len(resources) != 2 {
		t.Fatalf("expected 2 deduplicated resources, got %v", resources)
	}

	types, err := logger.DistinctTypes(ctx)
	if err != nil {
		t.Fatalf("DistinctTypes: %v", err)
	}
	if len(types) != 2 {
		t.Fatalf("expected 2 deduplicated types, got %v", types)
	}
}
