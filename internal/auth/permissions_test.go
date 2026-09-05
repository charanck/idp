package auth_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"controlplane/internal/auth"
	authmodel "controlplane/internal/model/auth"
)

func permissionsJSON(t *testing.T, modules ...string) authmodel.Group {
	t.Helper()
	raw, err := json.Marshal(modules)
	if err != nil {
		t.Fatalf("marshal modules: %v", err)
	}
	return authmodel.Group{ID: uuid.New(), Permissions: raw}
}

func TestComputeEffectivePermissions_UnionsModulesAcrossGroups(t *testing.T) {
	g1 := permissionsJSON(t, auth.ModuleConfigs)
	g2 := permissionsJSON(t, auth.ModuleFlags, auth.ModuleUsers)

	perms := auth.ComputeEffectivePermissions([]authmodel.Group{g1, g2}, map[uuid.UUID][]uuid.UUID{})

	for _, module := range []string{auth.ModuleConfigs, auth.ModuleFlags, auth.ModuleUsers} {
		if !perms.HasModule(module) {
			t.Fatalf("expected union to include module %q", module)
		}
	}
	if perms.HasModule(auth.ModuleGroups) {
		t.Fatal("expected module not granted by any group to be absent")
	}
}

func TestComputeEffectivePermissions_UnrestrictedIfAnyGroupHasEmptyAppList(t *testing.T) {
	restricted := permissionsJSON(t, auth.ModuleConfigs)
	unrestricted := permissionsJSON(t, auth.ModuleConfigs)
	appID := uuid.New()

	perms := auth.ComputeEffectivePermissions(
		[]authmodel.Group{restricted, unrestricted},
		map[uuid.UUID][]uuid.UUID{restricted.ID: {appID}}, // unrestricted group missing from map = unrestricted
	)

	if !perms.UnrestrictedApps {
		t.Fatal("expected UnrestrictedApps=true since one group has an empty Application allow-list")
	}
	ids, unrestricted2 := perms.AllowedApplicationIDs()
	if !unrestricted2 || ids != nil {
		t.Fatalf("AllowedApplicationIDs() = (%v, %v), want (nil, true)", ids, unrestricted2)
	}
}

func TestComputeEffectivePermissions_RestrictedUnionsApplicationAllowLists(t *testing.T) {
	g1 := permissionsJSON(t, auth.ModuleConfigs)
	g2 := permissionsJSON(t, auth.ModuleConfigs)
	app1, app2 := uuid.New(), uuid.New()

	perms := auth.ComputeEffectivePermissions(
		[]authmodel.Group{g1, g2},
		map[uuid.UUID][]uuid.UUID{g1.ID: {app1}, g2.ID: {app2}},
	)

	if perms.UnrestrictedApps {
		t.Fatal("expected UnrestrictedApps=false when every group has a non-empty allow-list")
	}
	if !perms.AllowedAppIDs[app1] || !perms.AllowedAppIDs[app2] {
		t.Fatalf("expected both groups' applications allowed, got %+v", perms.AllowedAppIDs)
	}
}

func TestComputeEffectivePermissions_NoGroupsGrantsNothing(t *testing.T) {
	perms := auth.ComputeEffectivePermissions(nil, nil)

	if perms.HasModule(auth.ModuleConfigs) {
		t.Fatal("expected no modules granted with zero groups")
	}
	if perms.UnrestrictedApps {
		t.Fatal("expected UnrestrictedApps=false with zero groups")
	}
}
