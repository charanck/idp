package repository_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	model "controlplane/internal/model/auth"
	repo "controlplane/internal/repository/auth"
	"controlplane/internal/testutil"
)

// Exercises the batched membership/allow-list queries against real Postgres
// (the embedded-struct scan in ListByUserIDs isn't covered by fakes).
func TestGroupRepositoryBatchLookups(t *testing.T) {
	gdb := testutil.OpenDB(t)
	testutil.TruncateAll(t, gdb)
	ctx := context.Background()
	groups := repo.NewGroupRepository(gdb)

	u1, u2, u3 := uuid.New(), uuid.New(), uuid.New()
	for i, id := range []uuid.UUID{u1, u2, u3} {
		if err := gdb.Exec(`INSERT INTO users (id, password, username, email, is_active) VALUES (?, 'x', ?, ?, true)`,
			id, "batch-user-"+string(rune('a'+i)), "batch-"+string(rune('a'+i))+"@example.com").Error; err != nil {
			t.Fatalf("insert user: %v", err)
		}
	}
	g1 := &model.Group{Name: "batch-g1", Permissions: []byte(`["configs"]`)}
	g2 := &model.Group{Name: "batch-g2", Permissions: []byte(`[]`)}
	for _, g := range []*model.Group{g1, g2} {
		if err := groups.Create(ctx, g); err != nil {
			t.Fatalf("create group: %v", err)
		}
		t.Cleanup(func() { gdb.Exec("DELETE FROM groups WHERE id = ?", g.ID) })
	}
	if err := groups.SetUserGroups(ctx, u1, []uuid.UUID{g1.ID, g2.ID}); err != nil {
		t.Fatal(err)
	}
	if err := groups.SetUserGroups(ctx, u2, []uuid.UUID{g2.ID}); err != nil {
		t.Fatal(err)
	}

	byUser, err := groups.ListByUserIDs(ctx, []uuid.UUID{u1, u2, u3})
	if err != nil {
		t.Fatalf("ListByUserIDs: %v", err)
	}
	if len(byUser[u1]) != 2 || len(byUser[u2]) != 1 || len(byUser[u3]) != 0 {
		t.Fatalf("unexpected memberships: u1=%d u2=%d u3=%d", len(byUser[u1]), len(byUser[u2]), len(byUser[u3]))
	}
	if byUser[u2][0].ID != g2.ID || byUser[u2][0].Name != "batch-g2" {
		t.Fatalf("group columns not scanned: %+v", byUser[u2][0])
	}

	app := uuid.New()
	if err := gdb.Exec(`INSERT INTO applications (id, name) VALUES (?, 'batch-app')`, app).Error; err != nil {
		t.Fatal(err)
	}
	if err := groups.SetApplications(ctx, g1.ID, []uuid.UUID{app}); err != nil {
		t.Fatal(err)
	}
	apps, err := groups.ListApplicationIDsByGroups(ctx, []uuid.UUID{g1.ID, g2.ID})
	if err != nil {
		t.Fatalf("ListApplicationIDsByGroups: %v", err)
	}
	if len(apps[g1.ID]) != 1 || apps[g1.ID][0] != app || len(apps[g2.ID]) != 0 {
		t.Fatalf("unexpected allow-lists: %+v", apps)
	}
}
