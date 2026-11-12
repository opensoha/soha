package auth

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestManagedNASRADIUSPermissionsAreAssignableAndScoped(t *testing.T) {
	var c struct {
		Permissions []struct {
			Key, Domain, Resource, Action, RiskLevel, Status, DisplayName string
			ScopeKinds                                                    []string
			Assignable                                                    bool
			LegacyAliases                                                 []string
		}
	}
	if e := json.Unmarshal(PermissionCatalogJSON(), &c); e != nil {
		t.Fatal(e)
	}
	found := map[string]bool{}
	for _, p := range c.Permissions {
		if p.Key == "network_access.nas_bindings.delete" {
			t.Fatal("unapproved delete grant")
		}
		for _, action := range []string{"view", "create", "update"} {
			key := "network_access.nas_bindings." + action
			if p.Key != key {
				continue
			}
			risk := "mutate"
			if action == "view" {
				risk = "read"
			}
			if p.Domain != "network_access" || p.Resource != "nas_bindings" || p.Action != action || p.Status != "active" || !p.Assignable || p.RiskLevel != risk || !slices.Contains(p.ScopeKinds, "resource") || len(p.LegacyAliases) != 0 {
				t.Fatalf("invalid managed NAS permission: %+v", p)
			}
			found[key] = true
		}
	}
	if len(found) != 3 {
		t.Fatalf("managed NAS catalog missing keys: %v", found)
	}
}
