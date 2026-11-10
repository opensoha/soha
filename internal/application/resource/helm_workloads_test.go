package resource

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	agentinfra "github.com/opensoha/soha/internal/infrastructure/agent"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const observationManifest = "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: api\n"

func observationWorkload(kind, name, namespace, owner string) string {
	return fmt.Sprintf(`{"apiVersion":"apps/v1","kind":%q,"metadata":{"name":%q,"namespace":%q,"uid":"uid-1","generation":1,"labels":{"app.kubernetes.io/managed-by":"Helm"},"annotations":{"meta.helm.sh/release-name":%q,"meta.helm.sh/release-namespace":"platform"}},"spec":{"replicas":1,"template":{"spec":{"containers":[{"name":"api","env":[{"name":"TOKEN","value":"synthetic-observation-secret"}]}]}}},"status":{"observedGeneration":1,"replicas":1,"updatedReplicas":1,"readyReplicas":1,"availableReplicas":1}}`, kind, name, namespace, owner)
}

type helmObservationDirect struct {
	DirectHelm
	DirectWorkloads
	manifest         string
	content          string
	status           string
	manifestRevision string
	reads            atomic.Int32
}

func (d *helmObservationDirect) GetHelmReleaseDetail(context.Context, string, string, string) (domainresource.HelmReleaseDetailView, error) {
	return domainresource.HelmReleaseDetailView{Name: "release", Namespace: "platform", Status: d.status, Revision: "1"}, nil
}
func (d *helmObservationDirect) GetHelmReleaseManifest(context.Context, string, string, string, string) (domainresource.HelmReleaseManifestView, error) {
	return domainresource.HelmReleaseManifestView{Revision: d.manifestRevision, Content: d.manifest}, nil
}
func (d *helmObservationDirect) GetDeploymentYAML(context.Context, string, string, string) (domainresource.ResourceYAMLView, error) {
	d.reads.Add(1)
	return domainresource.ResourceYAMLView{Content: d.content}, nil
}

type observationPermissions struct{}

func (observationPermissions) Authorize(_ context.Context, _ domainidentity.Principal, key string) error {
	if key == appaccess.PermPlatformHelmValuesView {
		return apperrors.ErrAccessDenied
	}
	return nil
}
func observationService(d *helmObservationDirect) *Service {
	return New(Dependencies{DirectHelm: d, DirectWorkloads: d, Audit: noopResourceAuditRecorder{}, Connections: stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{ID: "cluster", ConnectionMode: domaincluster.ConnectionModeDirectKubeconfig}}}, Authorizer: allowAllResourceAuthorizer{}, Permissions: observationPermissions{}})
}

func TestHelmWorkloadObservationDoesNotGrantRawManifestOrLeakSecrets(t *testing.T) {
	d := &helmObservationDirect{status: "deployed", manifestRevision: "1", manifest: observationManifest + "---\napiVersion: v1\nkind: Secret\nmetadata:\n  name: private\nstringData:\n  token: synthetic-observation-secret\n", content: observationWorkload("Deployment", "api", "platform", "release")}
	s := observationService(d)
	items, err := s.Runtime().GetHelmReleaseWorkloadSources(context.Background(), domainidentity.Principal{}, "cluster", "platform", "release")
	if err != nil || len(items) != 1 || items[0].Resource.UID != "uid-1" || d.reads.Load() != 1 {
		t.Fatalf("observation=%+v %v", items, err)
	}
	raw, _ := json.Marshal(items)
	if strings.Contains(string(raw), "synthetic-observation-secret") || strings.Contains(string(raw), "private") {
		t.Fatal("structured observation disclosed manifest data")
	}
	if _, err := s.Helm().GetHelmReleaseManifest(context.Background(), domainidentity.Principal{}, "cluster", "platform", "release", ""); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("raw manifest permission was weakened: %v", err)
	}
}

func TestHelmObservationRejectsForeignOwnershipAndUnstableRelease(t *testing.T) {
	for _, change := range []string{"owner", "namespace", "uid", "status", "revision"} {
		t.Run(change, func(t *testing.T) {
			d := &helmObservationDirect{status: "deployed", manifestRevision: "1", manifest: observationManifest, content: observationWorkload("Deployment", "api", "platform", "release")}
			switch change {
			case "owner":
				d.content = observationWorkload("Deployment", "api", "platform", "other")
			case "namespace":
				d.content = observationWorkload("Deployment", "api", "other", "release")
			case "uid":
				d.content = strings.ReplaceAll(d.content, "uid-1", "")
			case "status":
				d.status = "failed"
			case "revision":
				d.manifestRevision = "2"
			}
			if items, err := observationService(d).Runtime().GetHelmReleaseWorkloadSources(context.Background(), domainidentity.Principal{}, "cluster", "platform", "release"); err == nil || len(items) != 0 {
				t.Fatalf("unsafe observation accepted: %+v %v", items, err)
			}
		})
	}
}

func TestHelmObservationPreservesHelmAndWorkloadViewChecks(t *testing.T) {
	for _, helmOnly := range []bool{false, true} {
		d := &helmObservationDirect{status: "deployed", manifestRevision: "1", manifest: observationManifest, content: observationWorkload("Deployment", "api", "platform", "release")}
		permissions := exactPermissionAuthorizer{}
		if helmOnly {
			permissions[appaccess.PermPlatformHelmView] = true
		}
		s := New(Dependencies{DirectHelm: d, DirectWorkloads: d, Audit: noopResourceAuditRecorder{}, Connections: stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{ID: "cluster", ConnectionMode: domaincluster.ConnectionModeDirectKubeconfig}}}, Authorizer: permissions, Permissions: observationPermissions{}})
		if _, err := s.Runtime().GetHelmReleaseWorkloadSources(context.Background(), domainidentity.Principal{}, "cluster", "platform", "release"); !errors.Is(err, apperrors.ErrAccessDenied) || d.reads.Load() != 0 {
			t.Fatalf("view denial did not fence provider reads: %v (%d)", err, d.reads.Load())
		}
	}
}

func TestAgentHelmObservationUsesExistingReadOnlyEndpoints(t *testing.T) {
	var reads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Query().Get("namespace") != "platform" {
			http.Error(w, "unexpected scope", http.StatusForbidden)
			return
		}
		reads.Add(1)
		var data any
		switch r.URL.Path {
		case "/api/v1/platform/helm/releases/release/detail":
			data = domainresource.HelmReleaseDetailView{Name: "release", Namespace: "platform", Status: "deployed", Revision: "1"}
		case "/api/v1/platform/helm/releases/release/manifest":
			data = domainresource.HelmReleaseManifestView{Revision: "1", Content: observationManifest}
		case "/api/v1/platform/workloads/deployments/api/yaml":
			data = domainresource.ResourceYAMLView{Content: observationWorkload("Deployment", "api", "platform", "release")}
		default:
			http.Error(w, "unexpected request", http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	defer server.Close()
	s := New(Dependencies{Agents: testAgentClients(agentinfra.NewRegistry(0)), Connections: stubConnectionResolver{connection: agentConnection(server.URL)}, Authorizer: allowAllResourceAuthorizer{}, Permissions: observationPermissions{}, Audit: noopResourceAuditRecorder{}})
	items, err := s.Runtime().GetHelmReleaseWorkloadSources(context.Background(), domainidentity.Principal{}, "agent-cluster", "platform", "release")
	if err != nil || len(items) != 1 || items[0].Resource.ClusterID != "agent-cluster" || reads.Load() != 3 {
		t.Fatalf("agent observation=%+v %v (%d reads)", items, err, reads.Load())
	}
}

func TestHelmWorkloadManifestIsBoundedAndDoesNotClaimUnrelatedResources(t *testing.T) {
	content := observationManifest + "---\napiVersion: apps/v1\nkind: StatefulSet\nmetadata:\n  name: db\n  namespace: platform\n---\napiVersion: apps/v1\nkind: DaemonSet\nmetadata:\n  name: daemon\n---\n" + strings.Replace(observationManifest, "name: api", "name: outside\n  namespace: other", 1) + "---\n" + strings.Replace(observationManifest, "name: api", "name: hook\n  annotations:\n    helm.sh/hook: post-install", 1) + "---\n" + observationManifest
	items, err := helmWorkloadRefs(content, "platform")
	if err != nil || len(items) != 3 {
		t.Fatalf("manifest inventory=%+v %v", items, err)
	}
	for _, input := range []string{strings.Repeat("x", 8*1024*1024+1), "apiVersion: [synthetic-observation-secret", strings.Repeat(observationManifest+"---\n", 1001)} {
		if _, err := helmWorkloadRefs(input, "platform"); err == nil || strings.Contains(err.Error(), "synthetic-observation-secret") {
			t.Fatal("manifest limit or error redaction failed")
		}
	}
}
