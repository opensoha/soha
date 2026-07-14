package agentharness

import (
	"strings"
	"testing"
	"time"
)

func TestProjectRegistrySnapshotCarriesPinnedCatalogRevisionAndDigest(t *testing.T) {
	catalog := ProviderCatalog{Revision: 4, Providers: []ProviderDefinition{{
		SchemaVersion: "opensoha.dev/agent-provider-definition/v1", ID: "hermes", Kind: "hermes", PluginID: "agent.hermes", PluginVersion: "1.2.0",
		ProviderVersion: "1.2.0", AdapterProtocol: "opensoha.agent-provider.cli/v1", Runtime: RuntimeDefinition{Kind: "cli", Command: "hermes"}, Capabilities: []string{"root_cause"},
	}}}
	snapshot, err := ProjectRegistrySnapshot(catalog, FleetTarget{Platforms: []string{"linux"}}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Revision != 4 || !strings.HasPrefix(snapshot.Digest, "sha256:") || len(snapshot.Providers) != 1 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}
