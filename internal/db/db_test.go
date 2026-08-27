// Schema facts like FK delete-rules aren't fakeable, so this test opens the
// real test Postgres instance (same t.Skip()-if-unset convention as
// testutil.OpenDB) rather than running as a unit test.
package db_test

import (
	"testing"

	"controlplane/internal/testutil"
)

func TestMigrate_EveryForeignKeyCascadesOnDelete(t *testing.T) {
	gdb := testutil.OpenDB(t)

	type row struct {
		TableName      string
		ConstraintName string
		DeleteRule     string
	}
	var rows []row
	err := gdb.Raw(`
		SELECT tc.table_name, tc.constraint_name, rc.delete_rule
		FROM information_schema.table_constraints tc
		JOIN information_schema.referential_constraints rc
			ON tc.constraint_name = rc.constraint_name
			AND tc.table_schema = rc.constraint_schema
		WHERE tc.constraint_type = 'FOREIGN KEY' AND tc.table_schema = 'public'
	`).Scan(&rows).Error
	if err != nil {
		t.Fatalf("querying foreign keys: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("expected at least one foreign key in the schema")
	}

	for _, r := range rows {
		if r.DeleteRule != "CASCADE" {
			t.Errorf("%s.%s has delete_rule %q, want CASCADE", r.TableName, r.ConstraintName, r.DeleteRule)
		}
	}
}
