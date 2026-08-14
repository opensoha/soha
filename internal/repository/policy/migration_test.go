package policy

import (
	"os"
	"strings"
	"testing"
)

func TestIndependentResourceCreationPermissionMigrationRetiresLegacyGrant(t *testing.T) {
	raw, err := os.ReadFile("../../../migrations/postgres/0049_independent_resource_creation_permission.sql")
	if err != nil {
		t.Fatal(err)
	}
	migration := string(raw)
	for _, expected := range []string{
		"permission_keys::jsonb - 'platform.resource.create'",
		"THEN '[\"platform.resource-creation.use\"]'::jsonb",
		"scope = 'system'",
		"id IN ('admin', 'ops')",
		"UPDATE personal_access_tokens",
		"UPDATE service_account_tokens",
		"DELETE FROM mcp_tool_grants",
	} {
		if !strings.Contains(migration, expected) {
			t.Fatalf("resource creation permission migration missing %q", expected)
		}
	}
	if strings.Contains(migration, "platform.resource.create\"]'::jsonb ||") {
		t.Fatal("legacy resource create grant must not be mapped to the new entry permission")
	}
}
