package docker

import (
	"os"
	"strings"
	"testing"
)

func TestHostAgentInstallationMigrationStoresOnlyHashesAndCiphertext(t *testing.T) {
	raw, err := os.ReadFile("../../../migrations/postgres/0050_docker_host_agent_installations.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	migration := string(raw)
	for _, expected := range []string{
		"download_token_hash text NOT NULL",
		"enrollment_token_hash text",
		"agent_token_ciphertext text",
		"runtime_token_hash text",
		"WHERE runtime_token_hash IS NOT NULL AND revoked_at IS NULL",
	} {
		if !strings.Contains(migration, expected) {
			t.Fatalf("migration missing %q", expected)
		}
	}
	for _, forbidden := range []string{"download_token text", "enrollment_token text", "runtime_token text", "agent_token text"} {
		if strings.Contains(migration, forbidden) {
			t.Fatalf("migration stores plaintext credential column %q", forbidden)
		}
	}
}
