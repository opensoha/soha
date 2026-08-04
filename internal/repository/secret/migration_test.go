package secret

import (
	"os"
	"strings"
	"testing"
)

func TestVaultKV2MigrationKeepsExactlyOneVersionSource(t *testing.T) {
	baseline, err := os.ReadFile("../../../migrations/postgres/0001_init.sql")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(baseline), "source_type text DEFAULT 'local'") {
		t.Fatal("0001 baseline must not duplicate the incremental Vault migration")
	}

	raw, err := os.ReadFile("../../../migrations/postgres/0039_vault_kv2_secret_versions.sql")
	if err != nil {
		t.Fatal(err)
	}
	migration := string(raw)
	for _, required := range []string{
		"ALTER COLUMN ciphertext DROP NOT NULL",
		"source_type TEXT NOT NULL DEFAULT 'local'",
		"source_type = 'local' AND ciphertext IS NOT NULL",
		"source_type = 'vault_kv2' AND ciphertext IS NULL",
		"vault_version > 0",
	} {
		if !strings.Contains(migration, required) {
			t.Fatalf("migration missing %q", required)
		}
	}
}
