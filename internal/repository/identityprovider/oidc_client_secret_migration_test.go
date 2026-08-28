package identityprovider

import (
	"os"
	"strings"
	"testing"
)

func TestOIDCClientSecretMigrationAddsEncryptedMaterialColumns(t *testing.T) {
	raw, err := os.ReadFile("../../../migrations/postgres/0052_identity_oidc_client_secret_ciphertext.sql")
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, column := range []string{"client_secret_ciphertext text", "client_secret_hash_at_encryption text"} {
		if !strings.Contains(text, "ADD COLUMN IF NOT EXISTS "+column) {
			t.Fatalf("OIDC Client Secret migration does not add %q", column)
		}
	}
	if strings.Contains(text, "client_secret_plaintext") {
		t.Fatal("OIDC Client Secret migration must not add a plaintext column")
	}
}
