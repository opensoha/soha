package resource

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"

	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestImportSourceDigestTracksConfigurationAndOwnershipNotStatus(t *testing.T) {
	t.Parallel()
	const original = `{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"namespace":"team","name":"api","uid":"uid-1","resourceVersion":"1","generation":1,"ownerReferences":[{"apiVersion":"example.com/v1","kind":"Widget","name":"root","uid":"parent-1","controller":true}]},"spec":{"replicas":2,"credential":"synthetic-secret"},"status":{"readyReplicas":1}}`
	base, err := sourceSnapshot("cluster", "team", "apps/v1", "Deployment", "api", "", "", original)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, before, after string
		changed             bool
	}{
		{"status", `"readyReplicas":1`, `"readyReplicas":2`, false},
		{"resource-version", `"resourceVersion":"1"`, `"resourceVersion":"2"`, false},
		{"generation", `"generation":1`, `"generation":2`, false},
		{"spec", `"replicas":2`, `"replicas":3`, true},
		{"owner-uid", `parent-1`, `parent-2`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			current, err := sourceSnapshot("cluster", "team", "apps/v1", "Deployment", "api", "", "", strings.Replace(original, tc.before, tc.after, 1))
			if err != nil {
				t.Fatal(err)
			}
			if (base.SpecDigest != current.SpecDigest) != tc.changed {
				t.Fatalf("digest changed = %v, want %v", base.SpecDigest != current.SpecDigest, tc.changed)
			}
		})
	}
	encoded, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "synthetic-secret") || strings.Contains(string(encoded), "readyReplicas") {
		t.Fatalf("snapshot leaked configuration/status: %s", encoded)
	}
	if base.Resource.UID != "uid-1" || len(base.Owners) != 1 || base.Owners[0].Controller == nil || !*base.Owners[0].Controller {
		t.Fatalf("source evidence = %#v", base)
	}
}

type importSourceDirect struct {
	DirectCustomResource
	content string
	reads   int
}

func (d *importSourceDirect) ResolveCRD(context.Context, string, string) (domainresource.CRDResourceDefinition, error) {
	return domainresource.CRDResourceDefinition{Group: "example.com", Version: "v1", Kind: "Widget", Resource: "widgets", Namespaced: true}, nil
}
func (d *importSourceDirect) GetCustomResourceYAML(_ context.Context, _ string, definition domainresource.CRDResourceDefinition, namespace, name string) (domainresource.ResourceYAMLView, error) {
	d.reads++
	if definition.Resource != "widgets" || namespace != "team" || name != "root" {
		return domainresource.ResourceYAMLView{}, apperrors.ErrInvalidArgument
	}
	return domainresource.ResourceYAMLView{Content: d.content}, nil
}

func TestImportedCRSourceReusesScopeSensitiveReadAndUIDFence(t *testing.T) {
	const content = `{"apiVersion":"example.com/v1","kind":"Widget","metadata":{"namespace":"team","name":"root","uid":"uid-1","generation":1},"spec":{"password":"synthetic-secret"}}`
	direct := &importSourceDirect{content: content}
	service := New(Dependencies{DirectCustom: direct, Audit: noopResourceAuditRecorder{}, Connections: stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{ID: "cluster", ConnectionMode: domaincluster.ConnectionModeDirectKubeconfig}}}, Authorizer: exactPermissionAuthorizer{appaccess.PermPlatformExtensionsView: true}, Permissions: &recordingDenyRuntimePermissions{}})
	source, err := service.Runtime().GetImportSource(context.Background(), domainidentity.Principal{}, "cluster", "team", "CustomResource", "root", "widgets.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if direct.reads != 1 || source.Resource.UID != "uid-1" || source.Plural != "widgets" {
		t.Fatalf("source=%+v reads=%d", source, direct.reads)
	}
	if _, err := service.Runtime().GetApplicationSourceYAML(context.Background(), domainidentity.Principal{}, source); !errors.Is(err, apperrors.ErrAccessDenied) || direct.reads != 1 {
		t.Fatalf("sensitive read=%v reads=%d", err, direct.reads)
	}
	service = New(Dependencies{DirectCustom: direct, Audit: noopResourceAuditRecorder{}, Connections: stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{ID: "cluster", ConnectionMode: domaincluster.ConnectionModeDirectKubeconfig}}}, Authorizer: exactPermissionAuthorizer{appaccess.PermPlatformExtensionsView: true}, Permissions: allowWorkloadPermission{}})
	item, err := service.Runtime().GetApplicationSourceYAML(context.Background(), domainidentity.Principal{}, source)
	if err != nil || !strings.Contains(item.Content, "synthetic-secret") {
		t.Fatalf("authorized YAML=%+v %v", item, err)
	}
	direct.content = strings.Replace(content, "uid-1", "replacement", 1)
	item, err = service.Runtime().GetApplicationSourceYAML(context.Background(), domainidentity.Principal{}, source)
	if !errors.Is(err, apperrors.ErrConflict) || item.Content != "" || strings.Contains(err.Error(), "synthetic-secret") {
		t.Fatalf("replacement disclosed YAML: %+v %v", item, err)
	}
}

func TestCRSourceDigestUsesPublicRevisionWithoutHashingSecrets(t *testing.T) {
	const original = `{"apiVersion":"example.com/v1","kind":"Widget","metadata":{"namespace":"team","name":"root","uid":"uid-1","generation":1,"resourceVersion":"1","annotations":{"credential":"low-entropy-secret"},"ownerReferences":[{"apiVersion":"example.com/v1","kind":"Widget","name":"parent","uid":"owner-1"}]},"spec":{"password":"low-entropy-secret"},"status":{"credential":"low-entropy-secret"}}`
	base, err := sourceSnapshot("cluster", "team", "example.com/v1", "Widget", "root", "widgets.example.com", "widgets", original)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, before, after string
		changed             bool
	}{
		{"secret-content", "low-entropy-secret", "another-secret", false},
		{"status-resource-version", `"resourceVersion":"1"`, `"resourceVersion":"2"`, false},
		{"spec-revision", `"generation":1`, `"generation":2`, true},
		{"owner-uid", "owner-1", "owner-2", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			current, err := sourceSnapshot("cluster", "team", "example.com/v1", "Widget", "root", "widgets.example.com", "widgets", strings.ReplaceAll(original, tc.before, tc.after))
			if err != nil || (base.SpecDigest != current.SpecDigest) != tc.changed {
				t.Fatalf("revision digest changed=%v expected=%v error=%v", base.SpecDigest != current.SpecDigest, tc.changed, err)
			}
		})
	}
}

func TestSourceNativeObservationAndExternalOwnership(t *testing.T) {
	const original = `{"metadata":{"annotations":{"argocd.argoproj.io/tracking-id":"synthetic-secret"},"generation":2},"spec":{"replicas":2},"status":{"observedGeneration":2,"readyReplicas":2,"updatedReplicas":2,"availableReplicas":2}}`
	if !externalImportManager([]byte(original)) {
		t.Fatal("missed GitOps authority")
	}
	counts := sourceReplicaObservation([]byte(original), "Deployment", 2)
	if counts == nil || counts.Desired != 2 || counts.Ready != 2 {
		t.Fatalf("counts=%+v", counts)
	}
	if sourceReplicaObservation([]byte(original), "Deployment", 3) != nil || sourceReplicaObservation([]byte(original), "Widget", 2) != nil {
		t.Fatal("invented health for stale or unknown controller")
	}
}

func TestImportSourceRejectsMissingIdentityAndWrongTarget(t *testing.T) {
	t.Parallel()
	const original = `{"apiVersion":"example.com/v1","kind":"Widget","metadata":{"namespace":"team","name":"root","uid":"uid-1","generation":1},"spec":{"replicas":1}}`
	for _, tc := range []struct{ name, content string }{
		{"uid", strings.Replace(original, `"uid":"uid-1"`, `"uid":""`, 1)},
		{"name", strings.Replace(original, `"name":"root"`, `"name":"other"`, 1)},
		{"namespace", strings.Replace(original, `"namespace":"team"`, `"namespace":"other"`, 1)},
		{"group", strings.Replace(original, `example.com/v1`, `other.com/v1`, 1)},
		{"kind", strings.Replace(original, `Widget`, `OtherWidget`, 1)},
		{"generation", strings.Replace(original, `"generation":1`, `"generation":0`, 1)},
		{"invalid", "["},
		{"null", "null"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := sourceSnapshot("cluster", "team", "example.com/v1", "Widget", "root", "widgets.example.com", "widgets", tc.content)
			if !errors.Is(err, apperrors.ErrConflict) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}
