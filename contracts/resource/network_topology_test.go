package resource

import (
	"strings"
	"testing"
)

func TestTopologySourceQueriesCannotSelectArbitraryResourcesOrEscapeScope(t *testing.T) {
	valid := NetworkTopologySnapshotRequest{Queries: []NetworkTopologySourceQuery{{SourceID: "traefik.ingressroutes", Namespace: "team-a"}, {SourceID: "kubernetes.ingressclasses"}}}
	if err := ValidateNetworkTopologySnapshotRequest(valid); err != nil {
		t.Fatal(err)
	}
	for _, queries := range [][]NetworkTopologySourceQuery{
		nil,
		{{SourceID: "kubernetes.secrets", Namespace: "team-a"}},
		{{SourceID: "traefik.ingressroutes", Namespace: "../../secrets"}},
		{{SourceID: "kubernetes.ingressclasses", Namespace: "team-a"}},
		{{SourceID: "traefik.ingressroutes", Namespace: strings.Repeat("a", 64)}},
		{{SourceID: "traefik.ingressroutes"}, {SourceID: "traefik.ingressroutes"}},
		make([]NetworkTopologySourceQuery, NetworkTopologyMaxQueries+1),
	} {
		if err := ValidateNetworkTopologySnapshotRequest(NetworkTopologySnapshotRequest{Queries: queries}); err == nil {
			t.Fatalf("unsafe snapshot queries accepted: %+v", queries)
		}
	}
}

func TestTopologySourcesKeepSupportedVersionsAndDedicatedKindsExplicit(t *testing.T) {
	seen := map[string]bool{}
	for _, definition := range NetworkTopologySources() {
		if seen[definition.ID] || definition.Kind == "Secret" || definition.Resource == "secrets" || definition.Resource == "*" || definition.Group == "*" {
			t.Fatalf("unsafe source registry: %+v", definition)
		}
		seen[definition.ID] = true
	}
	for _, id := range []string{"gateway.tcproutes", "gateway.tlsroutes", "gateway.udproutes"} {
		d, ok := LookupNetworkTopologySource(id)
		if !ok || d.Version != "v1" || d.FallbackVersion != "v1alpha2" || !d.RequiresCustomResourceAccess() {
			t.Fatalf("Gateway L4 source must be versioned and explicitly authorized: %+v", d)
		}
	}
	grant, ok := LookupNetworkTopologySource("gateway.referencegrants")
	if !ok || grant.Version != "v1" || grant.FallbackVersion != "v1beta1" {
		t.Fatalf("ReferenceGrant served-version support: %+v", grant)
	}
	definitions := NetworkTopologySources()
	definitions[0].Kind = "Secret"
	if NetworkTopologySources()[0].Kind == "Secret" {
		t.Fatal("source allowlist is mutable by callers")
	}
}

func TestNetworkTopologyNamedQueriesRejectUnsafeIdentity(t *testing.T) {
	for _, query := range []NetworkTopologySourceQuery{
		{SourceID: "kubernetes.services", Namespace: "team", Name: "../secret"},
		{SourceID: "traefik.ingressroutes", Namespace: "team", Name: "route"},
		{SourceID: "gateway.tcproutes", Namespace: "team", Name: "route"},
		{SourceID: "kubernetes.services", Name: "service"},
		{SourceID: "kubernetes.services", Namespace: "team", Name: "service/name"},
		{SourceID: "kubernetes.services", Namespace: "team", Name: "service?watch=true"},
		{SourceID: "kubernetes.ingressclasses", Namespace: "team", Name: "class"},
	} {
		if err := ValidateNetworkTopologySnapshotRequest(NetworkTopologySnapshotRequest{Queries: []NetworkTopologySourceQuery{query}}); err == nil {
			t.Fatalf("unsafe named query accepted: %+v", query)
		}
	}
	valid := NetworkTopologySnapshotRequest{Queries: []NetworkTopologySourceQuery{{SourceID: "kubernetes.services", Namespace: "team", Name: "service"}, {SourceID: "kubernetes.services", Namespace: "team"}}}
	if err := ValidateNetworkTopologySnapshotRequest(valid); err != nil {
		t.Fatalf("named and list queries must coexist: %v", err)
	}
}
