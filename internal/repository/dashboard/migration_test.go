package dashboard

import (
	"os"
	"strings"
	"testing"
)

func TestDashboardMigrationDefinesDurableJSONPanels(t *testing.T) {
	raw, err := os.ReadFile("../../../migrations/postgres/0040_observability_dashboards.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	text := string(raw)
	for _, expected := range []string{"CREATE TABLE IF NOT EXISTS observability_dashboards", "panels JSONB", "tags JSONB"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("migration missing %q", expected)
		}
	}
}

func TestDashboardImportIRMigrationPreservesSourceAndWarnings(t *testing.T) {
	raw, err := os.ReadFile("../../../migrations/postgres/0047_observability_dashboard_import_ir.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	text := string(raw)
	for _, expected := range []string{"source_format TEXT", "variables JSONB", "data_source_bindings JSONB", "import_warnings JSONB", "raw_json JSONB"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("migration missing %q", expected)
		}
	}
}
