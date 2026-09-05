package auth

import (
	"encoding/json"

	"github.com/google/uuid"

	model "controlplane/internal/model/auth"
)

// Module keys are the fixed vocabulary a Group's Permissions may contain.
const (
	ModuleApplications         = "applications"
	ModuleEnvironments         = "environments"
	ModuleConfigs              = "configs"
	ModuleFlags                = "flags"
	ModuleServiceClients       = "service_clients"
	ModuleUsers                = "users"
	ModuleGroups               = "groups"
	ModuleOAuthProviders       = "oauth_providers"
	ModulePolicies             = "policies"
	ModuleNotificationSettings = "notification_settings"
	ModuleActivityLog          = "activity_log"
)

// AllModules is the fixed vocabulary of module keys assignable to a Group,
// in display order.
var AllModules = []string{
	ModuleApplications,
	ModuleEnvironments,
	ModuleConfigs,
	ModuleFlags,
	ModuleServiceClients,
	ModuleUsers,
	ModuleGroups,
	ModuleOAuthProviders,
	ModulePolicies,
	ModuleNotificationSettings,
	ModuleActivityLog,
}

// EffectivePermissions is the union of a user's groups: the set of module
// keys they can access, plus which Applications they may see/manage.
// UnrestrictedApps is true (AllowedAppIDs is meaningless) if any one of the
// user's groups has an empty Application allow-list.
type EffectivePermissions struct {
	Modules          map[string]bool
	UnrestrictedApps bool
	AllowedAppIDs    map[uuid.UUID]bool
}

func (p EffectivePermissions) HasModule(module string) bool {
	return p.Modules[module]
}

// AllowedApplicationIDs returns the sorted allow-list and whether it's
// unrestricted, for callers building an "IN (...)" filter.
func (p EffectivePermissions) AllowedApplicationIDs() (ids []uuid.UUID, unrestricted bool) {
	if p.UnrestrictedApps {
		return nil, true
	}
	for id := range p.AllowedAppIDs {
		ids = append(ids, id)
	}
	return ids, false
}

// ComputeEffectivePermissions unions the given groups' permissions and
// Application allow-lists (groupApplicationIDs is keyed by Group.ID; a group
// missing from that map, or with an empty slice, is unrestricted).
func ComputeEffectivePermissions(groups []model.Group, groupApplicationIDs map[uuid.UUID][]uuid.UUID) EffectivePermissions {
	perms := EffectivePermissions{
		Modules:       make(map[string]bool),
		AllowedAppIDs: make(map[uuid.UUID]bool),
	}
	for _, g := range groups {
		var modules []string
		if len(g.Permissions) > 0 {
			_ = json.Unmarshal(g.Permissions, &modules)
		}
		for _, m := range modules {
			perms.Modules[m] = true
		}

		appIDs := groupApplicationIDs[g.ID]
		if len(appIDs) == 0 {
			perms.UnrestrictedApps = true
			continue
		}
		for _, id := range appIDs {
			perms.AllowedAppIDs[id] = true
		}
	}
	return perms
}
