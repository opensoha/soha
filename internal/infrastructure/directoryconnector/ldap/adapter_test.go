package ldap

import (
	domain "github.com/opensoha/soha/internal/domain/directorysync"
	"testing"
)

func TestDNHelpersAndMetadata(t *testing.T) {
	dn := "ou=Engineering,dc=example,dc=com"
	if got := parentDN(dn); got != "dc=example,dc=com" {
		t.Fatalf("parentDN=%q", got)
	}
	if got := firstRDNValue(dn); got != "Engineering" {
		t.Fatalf("firstRDNValue=%q", got)
	}
	connection := domain.Connection{Metadata: map[string]any{"baseDN": "dc=example,dc=com", "startTLS": true}}
	if metadata(connection, "baseDN", "") != "dc=example,dc=com" || !metadataBool(connection, "startTLS") {
		t.Fatal("metadata helpers failed")
	}
}
