package resource

import (
	"context"
	"errors"
	cr "github.com/opensoha/soha-contracts/resource"
	appaccess "github.com/opensoha/soha/internal/application/access"
	dc "github.com/opensoha/soha/internal/domain/cluster"
	di "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"strings"
	"testing"
)

type relationDirectFixture struct {
	DirectGenericResource
	queries []cr.RelationQuery
	onRead  func()
}

func (f *relationDirectFixture) RelationSnapshot(_ context.Context, cluster string, input cr.RelationSnapshotRequest) (cr.RelationSnapshot, error) {
	f.queries = input.Queries
	out := cr.RelationSnapshot{Version: cr.RelationSnapshotFeature}
	for _, q := range input.Queries {
		r := relationFailure(q.ID, "available", "")
		if q.Source.Kind == "Pod" {
			r.Objects = []cr.RelationObject{{Resource: cr.ResourceRef{ClusterID: cluster, APIVersion: "v1", Kind: "Pod", Namespace: q.Source.Namespace, Name: "child", UID: "child-uid"}, Owners: []cr.RelationOwner{{APIVersion: "example.com/v1", Kind: "Widget", Name: "root", UID: "uid-1"}}}}
		}
		out.Sources = append(out.Sources, r)
	}
	if f.onRead != nil {
		f.onRead()
	}
	return out, nil
}

func TestImportedRelationsAuthorizeCollectionsAndRecheckRootUID(t *testing.T) {
	const content = `{"apiVersion":"example.com/v1","kind":"Widget","metadata":{"namespace":"team","name":"root","uid":"uid-1","generation":1},"spec":{"password":"synthetic-secret"}}`
	direct := &importSourceDirect{content: content}
	snap := &relationDirectFixture{}
	s := New(Dependencies{DirectCustom: direct, DirectGeneric: snap, Audit: noopResourceAuditRecorder{}, Connections: stubConnectionResolver{connection: dc.Connection{Summary: dc.Summary{ID: "cluster", ConnectionMode: dc.ConnectionModeDirectKubeconfig}}}, Authorizer: exactPermissionAuthorizer{appaccess.PermPlatformExtensionsView: true, "platform.pods.view": true}})
	source, err := s.Runtime().GetImportSource(context.Background(), di.Principal{}, "cluster", "team", "CustomResource", "root", "widgets.example.com")
	if err != nil {
		t.Fatal(err)
	}
	view, err := s.Runtime().GetApplicationResourceRelations(context.Background(), di.Principal{}, source, cr.RelationConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Nodes) != 2 || !view.Partial {
		t.Fatalf("missing generic child/coverage: %+v", view)
	}
	for _, q := range snap.queries {
		if q.Source.Kind != "Pod" && q.Source.Kind != "Widget" {
			t.Fatalf("unauthorized collection read: %+v", q)
		}
	}
	snap.onRead = func() { direct.content = strings.Replace(content, "uid-1", "replacement", 1) }
	_, err = s.Runtime().GetApplicationResourceRelations(context.Background(), di.Principal{}, source, cr.RelationConfig{})
	if !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("root replaced during observation: %v", err)
	}
}

func TestRelationSnapshotRejectsForeignAndUnrequestedEvidence(t *testing.T) {
	q := cr.RelationQuery{ID: "pods", Source: cr.RelationSource{APIVersion: "v1", Resource: "pods", Kind: "Pod", Namespace: "team"}}
	input := cr.RelationSnapshot{Version: cr.RelationSnapshotFeature, Sources: []cr.RelationSourceResult{{Coverage: cr.RelationCoverage{QueryID: "pods", State: "available", ReasonCode: "synthetic-secret"}, Objects: []cr.RelationObject{{Resource: cr.ResourceRef{ClusterID: "cluster", APIVersion: "v1", Kind: "Pod", Namespace: "foreign", Name: "hidden", UID: "uid"}}}}}}
	out := sanitizeRelationSnapshot(input, cr.RelationSnapshotRequest{Queries: []cr.RelationQuery{q}}, "cluster")
	if len(out.Sources[0].Objects) != 0 || out.Sources[0].Coverage.State != "partial" || out.Sources[0].Coverage.ReasonCode == "synthetic-secret" {
		t.Fatal("foreign source or untrusted reason passed through")
	}
}

func TestRelationIdentityRejectsUntrustedObjectPaths(t *testing.T) {
	q := cr.RelationQuery{Source: cr.RelationSource{APIVersion: "v1", Kind: "Pod", Namespace: "team"}}
	ref := cr.ResourceRef{ClusterID: "cluster", APIVersion: "v1", Kind: "Pod", Namespace: "team", UID: "uid", Name: "normal-pod"}
	if !validRelationIdentity(ref, q, "cluster") {
		t.Fatal("valid identity rejected")
	}
	for _, length := range []int{63, 64, 253} {
		ref.Name = strings.Repeat("a", length)
		if !validRelationIdentity(ref, q, "cluster") {
			t.Fatalf("valid API subdomain name rejected at length %d", length)
		}
	}
	for _, name := range []string{"../secrets", "pod/exec", "query?token=value", "", "pod..name", "pod.-name", "Pod", "-pod", "pod-", strings.Repeat("a", 254)} {
		ref.Name = name
		if validRelationIdentity(ref, q, "cluster") {
			t.Fatalf("untrusted path accepted: %q", name)
		}
	}
}

func TestAgentRelationSnapshotBindsOnlyRegisteredAgentIdentity(t *testing.T) {
	connection := dc.Connection{Summary: dc.Summary{ID: "registration", ConnectionMode: dc.ConnectionModeAgent}, Metadata: map[string]any{dc.MetadataAgentClusterID: "agent-native"}}
	query := cr.RelationQuery{ID: "pods", Source: cr.RelationSource{APIVersion: "v1", Resource: "pods", Kind: "Pod", Namespace: "team"}}
	for _, test := range []struct {
		name, cluster, namespace string
		allowed                  bool
	}{
		{"native", "agent-native", "team", true},
		{"registered", "registration", "team", true},
		{"foreign cluster", "other-agent", "team", false},
		{"foreign namespace", "agent-native", "other", false},
		{"missing identity", "", "team", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			snapshot := cr.RelationSnapshot{Version: cr.RelationSnapshotFeature, Sources: []cr.RelationSourceResult{{Coverage: cr.RelationCoverage{QueryID: "pods", State: "available"}, Objects: []cr.RelationObject{{Resource: cr.ResourceRef{ClusterID: test.cluster, APIVersion: "v1", Kind: "Pod", Namespace: test.namespace, Name: "child", UID: "pod-uid"}, Owners: []cr.RelationOwner{{APIVersion: "apps/v1", Kind: "ReplicaSet", Name: "parent", UID: "parent-uid"}}}}}}}
			out := sanitizeRelationSnapshot(bindAgentRelationSnapshot(snapshot, connection), cr.RelationSnapshotRequest{Queries: []cr.RelationQuery{query}}, connection.Summary.ID)
			if got := len(out.Sources[0].Objects); (got == 1) != test.allowed {
				t.Fatalf("wrong bound identity result: %+v", out)
			}
			if test.allowed {
				ref := out.Sources[0].Objects[0]
				if ref.Resource.ClusterID != "registration" || ref.Resource.UID != "pod-uid" || ref.Owners[0].UID != "parent-uid" {
					t.Fatalf("stable identity changed beyond cluster mapping: %+v", ref)
				}
			} else if out.Sources[0].Coverage.ReasonCode != "invalid_identity" {
				t.Fatal("foreign identity was not rejected")
			}
		})
	}
	// Unsynced connections must not silently adopt an arbitrary Agent-local ID.
	delete(connection.Metadata, dc.MetadataAgentClusterID)
	snapshot := cr.RelationSnapshot{Sources: []cr.RelationSourceResult{{Objects: []cr.RelationObject{{Resource: cr.ResourceRef{ClusterID: "agent-native"}}}}}}
	if bindAgentRelationSnapshot(snapshot, connection).Sources[0].Objects[0].Resource.ClusterID != "agent-native" {
		t.Fatal("unknown Agent identity rebound without a registered mapping")
	}
}
