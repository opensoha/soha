package identityprovider

import (
	"os"
	"strings"
	"testing"
)

func TestRedirectURIRegexMigrationAddsPersistenceColumn(t *testing.T) {
	raw, err := os.ReadFile("../../../migrations/postgres/0042_identity_oidc_redirect_uri_regexes.sql")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "redirect_uri_regexes jsonb NOT NULL DEFAULT '[]'::jsonb") {
		t.Fatalf("redirect URI regex migration does not add the expected JSONB column")
	}
}
