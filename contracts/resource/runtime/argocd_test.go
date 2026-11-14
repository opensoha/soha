package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/fake"
	ktesting "k8s.io/client-go/testing"
)

func argoFixture(t *testing.T) (*unstructured.Unstructured, *unstructured.Unstructured) {
	t.Helper()
	decode := func(raw string) *unstructured.Unstructured {
		var object unstructured.Unstructured
		if err := json.Unmarshal([]byte(raw), &object); err != nil {
			t.Fatal(err)
		}
		return &object
	}
	image := "registry.example/app@sha256:" + strings.Repeat("a", 64)
	commit := strings.Repeat("b", 40)
	app := decode(`{"apiVersion":"argoproj.io/v1alpha1","kind":"Application","metadata":{"name":"web","namespace":"demo","uid":"app-uid","resourceVersion":"12"},"spec":{"project":"demo-project","destination":{"server":"https://kubernetes.default.svc","namespace":"demo"},"source":{"repoURL":"https://git.example/team/config.git","targetRevision":"` + commit + `","path":"web","kustomize":{"namespace":"demo","images":["app=` + image + `"]}},"syncPolicy":{"syncOptions":["FailOnSharedResource=true"]}},"status":{"sync":{"status":"Synced","revision":"` + commit + `"},"health":{"status":"Healthy"},"operationState":{"phase":"Succeeded","finishedAt":"2026-09-14T00:00:00Z","syncResult":{"revision":"` + commit + `"},"operation":{"info":[{"name":"delivery.soha.io/argocd-operation","value":"run-1"}]}},"resources":[{"group":"apps","kind":"Deployment","namespace":"demo","name":"web"}]}}`)
	app.SetAnnotations(map[string]string{ArgoOwnerAnnotation: "binding-1", ArgoOperationAnnotation: "run-1"})
	source, _, _ := unstructured.NestedMap(app.Object, "spec", "source")
	_ = unstructured.SetNestedMap(app.Object, source, "status", "sync", "comparedTo", "source")
	deployment := decode(`{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"web","namespace":"demo","uid":"workload-uid","generation":3,"annotations":{"argocd.argoproj.io/tracking-id":"web:apps/Deployment:demo/web"}},"spec":{"replicas":1,"template":{"spec":{"containers":[{"name":"app","image":"` + image + `"}]}}},"status":{"observedGeneration":3,"replicas":1,"updatedReplicas":1,"availableReplicas":1}}`)
	return app, deployment
}

func argoProjectFixture() *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{"apiVersion": "argoproj.io/v1alpha1", "kind": "AppProject", "metadata": map[string]any{"name": "demo-project", "namespace": "demo"}, "spec": map[string]any{"sourceRepos": []any{"https://git.example/team/config.git"}, "destinations": []any{map[string]any{"namespace": "demo", "server": "https://kubernetes.default.svc"}}, "namespaceResourceWhitelist": []any{map[string]any{"group": "apps", "kind": "Deployment"}}}}}
}

func TestArgoImageOverrideForms(t *testing.T) {
	image := "registry.example:5000/app@sha256:" + strings.Repeat("a", 64)
	for _, value := range []string{image, "app=" + image} {
		name, parsed, err := ParseArgoImageOverride(value)
		if err != nil || parsed != image || name != "app" && name != "registry.example:5000/app" {
			t.Fatalf("override %q: %q %q %v", value, name, parsed, err)
		}
	}
	for _, value := range []string{"= " + image, "app:latest", "registry/app:tag@sha256:" + strings.Repeat("a", 64), image + "extra", "app=" + image + "=other"} {
		if _, _, err := ParseArgoImageOverride(value); err == nil {
			t.Fatalf("invalid override accepted: %s", value)
		}
	}
	app, child := argoFixture(t)
	plain := "registry.example/app@sha256:" + strings.Repeat("a", 64)
	_ = unstructured.SetNestedStringSlice(app.Object, []string{plain}, "spec", "source", "kustomize", "images")
	if err := ValidateArgoResources(app, []*unstructured.Unstructured{argoDesiredFixture(child)}); err != nil {
		t.Fatal(err)
	}
	_ = unstructured.SetNestedStringSlice(app.Object, []string{plain, "registry.example/app=" + plain}, "spec", "source", "kustomize", "images")
	if err := ValidateArgoApplication(app); err == nil {
		t.Fatal("duplicate plain and aliased image name accepted")
	}
}

func TestArgoOperationReplayAndOwnershipFences(t *testing.T) {
	for _, mode := range []string{"replay", "dry run", "other owner", "running", "unrestricted project"} {
		t.Run(mode, func(t *testing.T) {
			live, child := argoFixture(t)
			children := []*unstructured.Unstructured{argoDesiredFixture(child)}
			desired := live.DeepCopy()
			delete(desired.Object, "status")
			desired.SetUID("")
			desired.SetResourceVersion("")
			project := argoProjectFixture()
			opID := "run-2"
			options := metav1.PatchOptions{FieldManager: "binding-1"}
			switch mode {
			case "replay":
				opID = "run-1"
			case "dry run":
				options.DryRun = []string{metav1.DryRunAll}
			case "other owner":
				live.SetAnnotations(map[string]string{ArgoOwnerAnnotation: "other"})
			case "running":
				setArgoOperation(live, "binding-1", "other-run")
			case "unrestricted project":
				_ = unstructured.SetNestedSlice(project.Object, []any{map[string]any{"group": "*", "kind": "*"}}, "spec", "clusterResourceWhitelist")
			}
			client := fake.NewSimpleDynamicClient(runtime.NewScheme(), live, project)
			writes := 0
			client.PrependReactor("update", "applications", func(action ktesting.Action) (bool, runtime.Object, error) {
				writes++
				update, ok := action.(ktesting.UpdateAction)
				if !ok {
					t.Fatal("unexpected update action")
				}
				write, ok := update.GetObject().(*unstructured.Unstructured)
				if !ok {
					t.Fatal("unexpected update object")
				}
				if write.GetUID() != live.GetUID() || write.GetResourceVersion() != live.GetResourceVersion() || argoOperationID(write, "operation") != opID || argoString(write, "operation", "sync", "revision") != argoString(desired, "spec", "source", "targetRevision") {
					t.Fatal("write lost operation identity or optimistic lock")
				}
				updateOptions, ok := action.(interface{ GetUpdateOptions() metav1.UpdateOptions })
				if !ok {
					t.Fatal("unexpected update options")
				}
				if len(updateOptions.GetUpdateOptions().DryRun) != 1 {
					t.Fatal("preflight lost dry-run")
				}
				return true, write, nil
			})
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_, err := ApplyArgoApplication(ctx, client, desired, children, options, opID)
			if mode == "replay" || mode == "dry run" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("unsafe sync accepted")
			}
			if mode == "dry run" && writes != 1 || mode != "dry run" && writes != 0 {
				t.Fatalf("unexpected writes %d", writes)
			}
		})
	}
}

func TestArgoStopWaitsForControllerAndDoesNotStopReplacement(t *testing.T) {
	for _, mode := range []string{"confirmed", "replacement", "controller error"} {
		t.Run(mode, func(t *testing.T) {
			initial, _ := argoFixture(t)
			setArgoOperation(initial, "binding-1", "run-1")
			_ = unstructured.SetNestedField(initial.Object, "Running", "status", "operationState", "phase")
			live := initial.DeepCopy()
			if mode == "replacement" {
				live.SetUID("replacement")
			}
			client := fake.NewSimpleDynamicClient(runtime.NewScheme(), live)
			writes := 0
			client.PrependReactor("update", "applications", func(action ktesting.Action) (bool, runtime.Object, error) {
				writes++
				update, ok := action.(ktesting.UpdateAction)
				if !ok {
					t.Fatal("unexpected update action")
				}
				write, ok := update.GetObject().(*unstructured.Unstructured)
				if !ok {
					t.Fatal("unexpected update object")
				}
				if write.GetUID() != initial.GetUID() || write.GetResourceVersion() != initial.GetResourceVersion() || argoString(write, "status", "operationState", "phase") != "Terminating" || argoOperationID(write, "operation") != "run-1" {
					t.Fatal("termination lost native phase or identity fences")
				}
				delete(write.Object, "operation")
				phase := "Failed"
				if mode == "controller error" {
					phase = "Error"
				}
				_ = unstructured.SetNestedField(write.Object, phase, "status", "operationState", "phase")
				return true, write, client.Tracker().Update(argoApplications, write, "demo")
			})
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_, err := terminateArgoOperation(ctx, client.Resource(argoApplications).Namespace("demo"), initial, "run-1")
			if mode == "confirmed" && err != nil || mode != "confirmed" && err == nil {
				t.Fatalf("termination=%v", err)
			}
			if mode == "replacement" && writes != 0 || mode != "replacement" && writes != 1 {
				t.Fatalf("termination writes=%d", writes)
			}
		})
	}
	app, _ := argoFixture(t)
	client := fake.NewSimpleDynamicClient(runtime.NewScheme(), app)
	client.PrependReactor("get", "applications", func(ktesting.Action) (bool, runtime.Object, error) { return true, nil, errors.New("API unavailable") })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := waitArgoOperation(ctx, client.Resource(argoApplications).Namespace("demo"), app, "run-1")
	if !errors.Is(err, ErrArgoStopUnconfirmed) || errors.Is(err, context.Canceled) {
		t.Fatalf("unconfirmed stop was acknowledged: %v", err)
	}
}

func TestArgoApplicationRejectsMutableOrUnscopedInputs(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*unstructured.Unstructured)
	}{
		{"branch", func(app *unstructured.Unstructured) {
			_ = unstructured.SetNestedField(app.Object, "main", "spec", "source", "targetRevision")
		}},
		{"other namespace", func(app *unstructured.Unstructured) {
			_ = unstructured.SetNestedField(app.Object, "production", "spec", "destination", "namespace")
		}},
		{"implicit kustomize namespace", func(app *unstructured.Unstructured) {
			unstructured.RemoveNestedField(app.Object, "spec", "source", "kustomize", "namespace")
		}},
		{"duplicate image mapping", func(app *unstructured.Unstructured) {
			images, _, _ := unstructured.NestedStringSlice(app.Object, "spec", "source", "kustomize", "images")
			_ = unstructured.SetNestedStringSlice(app.Object, append(images, images[0]), "spec", "source", "kustomize", "images")
		}},
		{"external cluster", func(app *unstructured.Unstructured) {
			_ = unstructured.SetNestedField(app.Object, "https://external", "spec", "destination", "server")
		}},
		{"default project", func(app *unstructured.Unstructured) {
			_ = unstructured.SetNestedField(app.Object, "default", "spec", "project")
		}},
		{"automated", func(app *unstructured.Unstructured) {
			_ = unstructured.SetNestedMap(app.Object, map[string]any{}, "spec", "syncPolicy", "automated")
		}},
		{"mutable image", func(app *unstructured.Unstructured) {
			_ = unstructured.SetNestedStringSlice(app.Object, []string{"app=registry.example/app:latest"}, "spec", "source", "kustomize", "images")
		}},
		{"repository credentials", func(app *unstructured.Unstructured) {
			_ = unstructured.SetNestedField(app.Object, "https://token@git.example/config.git", "spec", "source", "repoURL")
		}},
		{"path traversal", func(app *unstructured.Unstructured) {
			_ = unstructured.SetNestedField(app.Object, "../outside", "spec", "source", "path")
		}},
		{"plugin", func(app *unstructured.Unstructured) {
			_ = unstructured.SetNestedMap(app.Object, map[string]any{}, "spec", "source", "plugin")
		}},
		{"ignored drift", func(app *unstructured.Unstructured) {
			_ = unstructured.SetNestedSlice(app.Object, []any{}, "spec", "ignoreDifferences")
		}},
		{"cascade", func(app *unstructured.Unstructured) {
			app.SetFinalizers([]string{"resources-finalizer.argocd.argoproj.io"})
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			app, _ := argoFixture(t)
			if err := ValidateArgoApplication(app); err != nil {
				t.Fatal(err)
			}
			test.change(app)
			if ValidateArgoApplication(app) == nil {
				t.Fatal("unsafe GitOps definition accepted")
			}
		})
	}
}

func TestArgoHealthRequiresCurrentOperationAndLiveOwnedDigest(t *testing.T) {
	for _, mode := range []string{"healthy", "old operation", "old commit", "old source", "running", "failed", "resource missing", "other owner", "old generation", "wrong image", "deleting", "extra container", "extra init", "extra resource", "wrong root owner", "spec drift", "progress inventory"} {
		t.Run(mode, func(t *testing.T) {
			app, workload := argoFixture(t)
			desired := argoDesiredFixture(app)
			children := []*unstructured.Unstructured{argoDesiredFixture(workload)}
			switch mode {
			case "extra container":
				containers, _, _ := unstructured.NestedSlice(workload.Object, "spec", "template", "spec", "containers")
				_ = unstructured.SetNestedSlice(workload.Object, append(containers, containers[0]), "spec", "template", "spec", "containers")
			case "extra init":
				containers, _, _ := unstructured.NestedSlice(workload.Object, "spec", "template", "spec", "containers")
				_ = unstructured.SetNestedSlice(workload.Object, containers, "spec", "template", "spec", "initContainers")
			case "extra resource":
				resources, _, _ := unstructured.NestedSlice(app.Object, "status", "resources")
				_ = unstructured.SetNestedSlice(app.Object, append(resources, map[string]any{"group": "", "kind": "ConfigMap", "name": "outside", "namespace": "demo"}), "status", "resources")
			case "wrong root owner":
				app.SetAnnotations(map[string]string{ArgoOwnerAnnotation: "other", ArgoOperationAnnotation: "run-1"})
			case "spec drift":
				_ = unstructured.SetNestedField(workload.Object, int64(2), "spec", "replicas")
			case "progress inventory":
				delete(app.Object, "status")
			case "old operation":
				annotations := app.GetAnnotations()
				annotations[ArgoOperationAnnotation] = "new-run"
				app.SetAnnotations(annotations)
			case "old commit":
				_ = unstructured.SetNestedField(app.Object, strings.Repeat("c", 40), "status", "sync", "revision")
			case "old source":
				_ = unstructured.SetNestedField(app.Object, "other", "status", "sync", "comparedTo", "source", "path")
			case "running":
				_ = unstructured.SetNestedField(app.Object, "Running", "status", "operationState", "phase")
			case "failed":
				_ = unstructured.SetNestedField(app.Object, "Failed", "status", "operationState", "phase")
			case "other owner":
				workload.SetAnnotations(map[string]string{"argocd.argoproj.io/tracking-id": "other:apps/Deployment:demo/web"})
			case "old generation":
				workload.SetGeneration(4)
			case "wrong image":
				_ = unstructured.SetNestedSlice(workload.Object, []any{map[string]any{"name": "app", "image": "registry.example/app:latest"}}, "spec", "template", "spec", "containers")
			case "deleting":
				now := metav1.Now()
				workload.SetDeletionTimestamp(&now)
			}
			objects := []runtime.Object{workload}
			if mode == "resource missing" {
				objects = nil
			}
			client := fake.NewSimpleDynamicClient(runtime.NewScheme(), objects...)
			observed, err := ObserveArgoApplication(context.Background(), client, desired, app, children, "binding-1")
			if mode == "healthy" {
				if err != nil || observed.Health != "healthy" || len(observed.Resources) != 1 || observed.Resources[0].GetUID() != "workload-uid" {
					t.Fatalf("observation=%+v err=%v", observed, err)
				}
			} else if observed.Health == "healthy" {
				t.Fatalf("false healthy: %+v", observed)
			}
			if mode == "progress inventory" && len(observed.Resources) != 1 {
				t.Fatal("progress lost child inventory")
			}
			for _, action := range client.Actions() {
				if action.GetVerb() != "get" {
					t.Fatal("observation wrote to cluster")
				}
			}
		})
	}
}

func argoDesiredFixture(live *unstructured.Unstructured) *unstructured.Unstructured {
	desired := live.DeepCopy()
	desired.SetUID("")
	desired.SetResourceVersion("")
	desired.SetAnnotations(nil)
	unstructured.RemoveNestedField(desired.Object, "metadata", "generation")
	delete(desired.Object, "status")
	return desired
}

func TestArgoAmbiguousWriteAndObservationFailureConfirmStop(t *testing.T) {
	for _, mode := range []string{"lost write response", "lost observation"} {
		t.Run(mode, func(t *testing.T) {
			live, workload := argoFixture(t)
			desired := argoDesiredFixture(live)
			children := []*unstructured.Unstructured{argoDesiredFixture(workload)}
			setArgoOperation(live, "binding-1", "run-2")
			_ = unstructured.SetNestedField(live.Object, "Running", "status", "operationState", "phase")
			_ = unstructured.SetNestedSlice(live.Object, []any{map[string]any{"name": ArgoOperationAnnotation, "value": "run-2"}}, "status", "operationState", "operation", "info")
			client := fake.NewSimpleDynamicClient(runtime.NewScheme(), argoProjectFixture())
			stops := 0
			client.PrependReactor("update", "applications", func(action ktesting.Action) (bool, runtime.Object, error) {
				update, ok := action.(ktesting.UpdateAction)
				if !ok {
					t.Fatal("unexpected update action")
				}
				write, ok := update.GetObject().(*unstructured.Unstructured)
				if !ok {
					t.Fatal("unexpected update object")
				}
				if argoString(write, "status", "operationState", "phase") != "Terminating" {
					t.Fatal("missing stop request")
				}
				stops++
				delete(write.Object, "operation")
				_ = unstructured.SetNestedField(write.Object, "Failed", "status", "operationState", "phase")
				return true, write, client.Tracker().Update(argoApplications, write, "demo")
			})
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			var err error
			if mode == "lost write response" {
				client.PrependReactor("create", "applications", func(ktesting.Action) (bool, runtime.Object, error) {
					if err := client.Tracker().Create(argoApplications, live, "demo"); err != nil {
						t.Fatal(err)
					}
					return true, nil, errors.New("write response lost")
				})
				_, err = ApplyArgoApplication(ctx, client, desired, children, metav1.PatchOptions{FieldManager: "binding-1"}, "run-2")
			} else {
				if err := client.Tracker().Create(argoApplications, live, "demo"); err != nil {
					t.Fatal(err)
				}
				reads := 0
				client.PrependReactor("get", "applications", func(ktesting.Action) (bool, runtime.Object, error) {
					reads++
					if reads == 1 {
						return true, nil, errors.New("observation unavailable")
					}
					return false, nil, nil
				})
				_, err = waitArgoOperation(ctx, client.Resource(argoApplications).Namespace("demo"), live, "run-2")
			}
			if err == nil || errors.Is(err, ErrArgoStopUnconfirmed) || stops != 1 {
				t.Fatalf("stop=%d error=%v", stops, err)
			}
			actual, err := client.Resource(argoApplications).Namespace("demo").Get(ctx, live.GetName(), metav1.GetOptions{})
			if err != nil || argoOperationActive(actual) {
				t.Fatal("failed task left an active sync")
			}
		})
	}
}
