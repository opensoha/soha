package menu

import (
	"os"
	"strings"
	"testing"
)

func TestComputeWorkbenchSectionMigrationIsLimitedToLegacyBuiltinRows(t *testing.T) {
	raw, err := os.ReadFile("../../../migrations/postgres/0044_compute_workbench_menu_sections.sql")
	if err != nil {
		t.Fatal(err)
	}
	migration := string(raw)
	for _, expected := range []string{
		"('compute-workbench', '/compute')",
		"('compute-workbench-overview', '/compute/overview')",
		"('compute-workbench-tasks-operations', '/compute/tasks/operations')",
		"menu.section = 'ops'",
	} {
		if !strings.Contains(migration, expected) {
			t.Fatalf("compute workbench section migration missing %q", expected)
		}
	}
}

func TestObservabilityExploreMigrationHidesLegacySignalMenus(t *testing.T) {
	raw, err := os.ReadFile("../../../migrations/postgres/0045_observability_explore_menu.sql")
	if err != nil {
		t.Fatal(err)
	}
	migration := string(raw)
	for _, expected := range []string{
		"monitoring-workbench-metrics",
		"monitoring-workbench-traces",
		"monitoring-workbench-logs",
		"SET enabled = false",
	} {
		if !strings.Contains(migration, expected) {
			t.Fatalf("observability menu migration missing %q", expected)
		}
	}
}

func TestMenuRouteAlignmentMigrationRemovesObsoleteAndAddsCanonicalMenus(t *testing.T) {
	raw, err := os.ReadFile("../../../migrations/postgres/0046_menu_route_alignment.sql")
	if err != nil {
		t.Fatal(err)
	}
	migration := string(raw)
	for _, expected := range []string{
		"compute-workbench-tasks-sync",
		"compute-workbench-tasks-build",
		"virtualization-workbench-operations",
		"docker-workbench-operations",
		"cluster-resources-namespaces",
		"monitoring-workbench-explore",
		"monitoring-workbench-alerting",
		"ai-workbench-knowledge-pipelines",
		"ai-workbench-production-operations",
		"ON CONFLICT (id) DO NOTHING",
	} {
		if !strings.Contains(migration, expected) {
			t.Fatalf("menu route alignment migration missing %q", expected)
		}
	}
}

func TestBuiltinMenuPermissionMigrationRemovesOnlyKnownRoleBindings(t *testing.T) {
	raw, err := os.ReadFile("../../../migrations/postgres/0048_builtin_menu_permission_visibility.sql")
	if err != nil {
		t.Fatal(err)
	}
	migration := string(raw)
	for _, expected := range []string{
		"DELETE FROM menu_role_bindings",
		"'compute-workbench'",
		"'docker-workbench-projects'",
		"'access-directory-sync'",
		"'settings-runtime-configuration'",
	} {
		if !strings.Contains(migration, expected) {
			t.Fatalf("built-in menu permission migration missing %q", expected)
		}
	}
	if strings.Contains(migration, "DELETE FROM menu_role_bindings;") {
		t.Fatal("built-in menu permission migration must preserve custom menu bindings")
	}
}
