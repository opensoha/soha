package resourcebackend

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	domainresource "github.com/opensoha/soha/internal/domain/resource"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
	k8sinfra "github.com/opensoha/soha/internal/infrastructure/kubernetes"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
)

func TestDirectQuickActionsRejectGitOpsOwners(t *testing.T) {
	ctx := context.Background()
	for _, action := range []struct {
		name string
		run  func(*Direct) error
	}{
		{"update Namespace", func(d *Direct) error {
			_, err := d.UpdateNamespace(ctx, "test", "app", domainresource.NamespaceUpsertInput{})
			return err
		}},
		{"delete Namespace", func(d *Direct) error { return d.DeleteNamespace(ctx, "test", "app") }},
		{"update Node", func(d *Direct) error {
			_, err := d.UpdateNode(ctx, "test", "app", domainresource.NodeUpdateInput{})
			return err
		}},
		{"cordon Node", func(d *Direct) error { return d.SetNodeUnschedulable(ctx, "test", "app", true) }},
		{"drain Node", func(d *Direct) error { return d.DrainNode(ctx, "test", "app", domainresource.NodeDrainInput{}) }},
		{"delete Node", func(d *Direct) error { return d.DeleteNode(ctx, "test", "app") }},
		{"restart Deployment", func(d *Direct) error { return d.RestartDeployment(ctx, "test", "demo", "app") }},
		{"scale Deployment", func(d *Direct) error { return d.ScaleDeployment(ctx, "test", "demo", "app", 2) }},
		{"rollback Deployment", func(d *Direct) error { return d.RollbackDeployment(ctx, "test", "demo", "app", "1") }},
		{"restart StatefulSet", func(d *Direct) error { return d.RestartStatefulSet(ctx, "test", "demo", "app") }},
		{"scale StatefulSet", func(d *Direct) error { return d.ScaleStatefulSet(ctx, "test", "demo", "app", 2) }},
		{"restart DaemonSet", func(d *Direct) error { return d.RestartDaemonSet(ctx, "test", "demo", "app") }},
		{"suspend CronJob", func(d *Direct) error { _, err := d.SetCronJobSuspend(ctx, "test", "demo", "app", true); return err }},
		{"update ConfigMap", func(d *Direct) error {
			_, err := d.UpdateConfigMapData(ctx, "test", "demo", "app", map[string]string{"key": "new"}, nil)
			return err
		}},
		{"update Secret", func(d *Direct) error {
			_, err := d.UpdateSecretData(ctx, "test", "demo", "app", map[string]string{"key": "new"})
			return err
		}},
		{"apply Service", func(d *Direct) error {
			_, err := d.ApplyResourceYAML(ctx, "test", "demo", "Service", "app", "apiVersion: v1\nkind: Service\nmetadata:\n  name: app\n")
			return err
		}},
		{"preview Service", func(d *Direct) error {
			_, err := d.DryRunResourceYAML(ctx, "test", "demo", "Service", "app", "apiVersion: v1\nkind: Service\nmetadata:\n  name: app\n")
			return err
		}},
		{"delete Service", func(d *Direct) error { return d.DeleteResource(ctx, "test", "demo", "Service", "app") }},
		{"create foreign ConfigMap", func(d *Direct) error {
			_, err := d.CreateResourceYAML(ctx, "test", "demo", "ConfigMap", "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: app\n  annotations:\n    argocd.argoproj.io/tracking-id: root:/ConfigMap:demo/app\n")
			return err
		}},
	} {
		t.Run(action.name, func(t *testing.T) {
			var writes atomic.Int32
			direct := directOwnershipFixture(t, true, &writes)
			if err := action.run(direct); err == nil || !strings.Contains(err.Error(), "external delivery owner") {
				t.Fatalf("expected owner rejection, got %v", err)
			}
			if writes.Load() != 0 {
				t.Fatalf("sent %d writes", writes.Load())
			}
		})
	}
}

func TestDirectNativeWritesPreserveObservedIdentity(t *testing.T) {
	var writes atomic.Int32
	direct := directOwnershipFixture(t, false, &writes)
	ctx := context.Background()
	if err := direct.RestartDeployment(ctx, "test", "demo", "app"); err != nil {
		t.Fatal(err)
	}
	if _, err := direct.ApplyResourceYAML(ctx, "test", "demo", "ConfigMap", "app", "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: app\ndata:\n  key: new\n"); err != nil {
		t.Fatal(err)
	}
	if err := direct.DeleteResource(ctx, "test", "demo", "Service", "app"); err != nil {
		t.Fatal(err)
	}
	if writes.Load() != 3 {
		t.Fatalf("writes = %d", writes.Load())
	}
}

func TestDirectCustomCreateRejectsArgoApplication(t *testing.T) {
	direct := &Direct{}
	_, err := direct.CreateCustomResourceYAML(context.Background(), "test", domainresource.CRDResourceDefinition{Group: "argoproj.io", Version: "v1alpha1", Kind: "Application", Resource: "applications", Namespaced: true}, "demo", "apiVersion: argoproj.io/v1alpha1\nkind: Application\nmetadata:\n  name: app\n")
	if err == nil || !strings.Contains(err.Error(), "frozen GitOps execution") {
		t.Fatalf("create: %v", err)
	}
}

func directOwnershipFixture(t *testing.T, owned bool, writes *atomic.Int32) *Direct {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(r.URL.Path, "/")
		resource := parts[len(parts)-2]
		kind := map[string]string{"deployments": "Deployment", "statefulsets": "StatefulSet", "daemonsets": "DaemonSet", "cronjobs": "CronJob", "configmaps": "ConfigMap", "secrets": "Secret", "services": "Service", "nodes": "Node", "namespaces": "Namespace", "pods": "Pod", "horizontalpodautoscalers": "HorizontalPodAutoscaler", "poddisruptionbudgets": "PodDisruptionBudget"}[resource]
		apiVersion := "v1"
		if strings.HasPrefix(r.URL.Path, "/apis/") {
			apiVersion = parts[2] + "/" + parts[3]
		}
		metadata := map[string]any{"name": "app", "namespace": "demo", "uid": "uid-app", "resourceVersion": "7"}
		if owned {
			metadata["annotations"] = map[string]string{"argocd.argoproj.io/tracking-id": "root:/" + kind + ":demo/app"}
		}
		object := map[string]any{"apiVersion": apiVersion, "kind": kind, "metadata": metadata}
		if r.Method != http.MethodGet {
			writes.Add(1)
			data, err := io.ReadAll(r.Body)
			if err != nil {
				t.Error(err)
			}
			decoded, _, err := scheme.Codecs.UniversalDeserializer().Decode(data, nil, nil)
			if err != nil {
				t.Error(err)
				http.Error(w, err.Error(), 400)
				return
			}
			body, err := k8sruntime.DefaultUnstructuredConverter.ToUnstructured(decoded)
			if err != nil {
				t.Error(err)
			}
			if r.Method == http.MethodDelete {
				identity, _ := body["preconditions"].(map[string]any)
				if identity["uid"] != "uid-app" || identity["resourceVersion"] != "7" {
					t.Errorf("delete lost identity: %+v", body)
				}
				object = map[string]any{"apiVersion": "v1", "kind": "Status", "status": "Success"}
			} else {
				identity, _ := body["metadata"].(map[string]any)
				if identity["uid"] != "uid-app" || identity["resourceVersion"] != "7" {
					t.Errorf("write lost identity: %+v", identity)
				}
				object = body
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(object)
	}))
	t.Cleanup(server.Close)
	kubeconfig := fmt.Sprintf("apiVersion: v1\nkind: Config\ncurrent-context: test\nclusters:\n- name: test\n  cluster:\n    server: %s\ncontexts:\n- name: test\n  context:\n    cluster: test\n    user: test\nusers:\n- name: test\n  user: {}\n", server.URL)
	manager := k8sinfra.NewManager([]cfgpkg.ClusterConfig{{ID: "test", KubeconfigData: kubeconfig}})
	return NewDirect(NewClusters(manager), nil)
}

func TestDirectInventoryDeletesPreserveObservedIdentity(t *testing.T) {
	for _, kind := range []string{"Node", "Namespace", "Pod"} {
		t.Run(kind, func(t *testing.T) {
			var writes atomic.Int32
			direct := directOwnershipFixture(t, false, &writes)
			var err error
			switch kind {
			case "Node":
				err = direct.DeleteNode(context.Background(), "test", "app")
			case "Namespace":
				err = direct.DeleteNamespace(context.Background(), "test", "app")
			case "Pod":
				err = direct.DeletePod(context.Background(), "test", "demo", "app")
			}
			if err != nil || writes.Load() != 1 {
				t.Fatalf("delete: %v, writes=%d", err, writes.Load())
			}
		})
	}
}

func TestDirectInventoryCannotIntroduceExternalOwner(t *testing.T) {
	for _, kind := range []string{"Node", "Namespace", "NamespaceCreate"} {
		t.Run(kind, func(t *testing.T) {
			var writes atomic.Int32
			direct := directOwnershipFixture(t, false, &writes)
			labels := map[string]string{"kustomize.toolkit.fluxcd.io/name": "external", "kustomize.toolkit.fluxcd.io/namespace": "flux-system"}
			var err error
			switch kind {
			case "Node":
				_, err = direct.UpdateNode(context.Background(), "test", "app", domainresource.NodeUpdateInput{Labels: labels})
			case "Namespace":
				_, err = direct.UpdateNamespace(context.Background(), "test", "app", domainresource.NamespaceUpsertInput{Labels: labels})
			case "NamespaceCreate":
				_, err = direct.CreateNamespace(context.Background(), "test", domainresource.NamespaceUpsertInput{Name: "app", Labels: labels})
			}
			if err == nil || writes.Load() != 0 {
				t.Fatalf("owner introduction: %v, writes=%d", err, writes.Load())
			}
		})
	}
}

func TestDirectHPAPDBYAMLLifecycle(t *testing.T) {
	for _, target := range []struct{ kind, apiVersion string }{{"HorizontalPodAutoscaler", "autoscaling/v2"}, {"PodDisruptionBudget", "policy/v1"}} {
		t.Run(target.kind, func(t *testing.T) {
			var writes atomic.Int32
			direct := directOwnershipFixture(t, false, &writes)
			ctx := context.Background()
			if _, err := direct.GetResourceYAML(ctx, "test", "demo", target.kind, "app"); err != nil {
				t.Fatal(err)
			}
			content := fmt.Sprintf("apiVersion: %s\nkind: %s\nmetadata:\n  name: app\n  namespace: demo\n", target.apiVersion, target.kind)
			if _, err := direct.DryRunResourceYAML(ctx, "test", "demo", target.kind, "app", content); err != nil {
				t.Fatal(err)
			}
			if _, err := direct.ApplyResourceYAML(ctx, "test", "demo", target.kind, "app", content); err != nil {
				t.Fatal(err)
			}
			if err := direct.DeleteResource(ctx, "test", "demo", target.kind, "app"); err != nil {
				t.Fatal(err)
			}
			if writes.Load() != 3 {
				t.Fatalf("writes = %d", writes.Load())
			}
		})
	}
}
