package repository_test

import (
	"context"
	"fmt"
	"testing"

	model "controlplane/internal/model/config"
	repo "controlplane/internal/repository/config"
	"controlplane/internal/testutil"
)

// Only the newest MaxConfigVersions versions of an entry are kept; every new
// version prunes the older ones, and numbering keeps increasing.
func TestUpsertEntryAndRecordVersion_KeepsOnlyLastVersions(t *testing.T) {
	gdb := testutil.OpenDB(t)
	testutil.TruncateAll(t, gdb)
	ctx := context.Background()
	configs := repo.NewConfigRepository(gdb)

	total := model.MaxConfigVersions + 3
	var entryID string
	for i := 1; i <= total; i++ {
		entry, _, err := configs.UpsertEntryAndRecordVersion(ctx, model.UpsertEntryParams{
			Service: "svc", Environment: "prod", Key: "K",
			EncryptedValue: fmt.Sprintf("v%d", i), ConfigType: "string",
		})
		if err != nil {
			t.Fatalf("upsert %d: %v", i, err)
		}
		entryID = entry.ID.String()
	}

	var versions []int
	if err := gdb.Raw("SELECT version FROM config_entry_versions WHERE config_entry_id = ? ORDER BY version DESC", entryID).Scan(&versions).Error; err != nil {
		t.Fatal(err)
	}
	if len(versions) != model.MaxConfigVersions {
		t.Fatalf("kept %d versions, want %d: %v", len(versions), model.MaxConfigVersions, versions)
	}
	if versions[0] != total || versions[len(versions)-1] != total-model.MaxConfigVersions+1 {
		t.Fatalf("unexpected retained versions: %v", versions)
	}
}
