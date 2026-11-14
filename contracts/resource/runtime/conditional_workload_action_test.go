package runtime

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	contractresource "github.com/opensoha/soha/contracts/resource"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/utils/ptr"
)

var conditionalWorkloadCases = []struct{ kind, action, resource string }{
	{"Deployment", "restart", "deployments"}, {"Deployment", "scale", "deployments"}, {"Deployment", "rollback", "deployments"},
	{"StatefulSet", "restart", "statefulsets"}, {"StatefulSet", "scale", "statefulsets"}, {"DaemonSet", "restart", "daemonsets"},
}

func conditionalWorkloadFixture(kind string) (*dynamicfake.FakeDynamicClient, *unstructured.Unstructured) {
	current := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "apps/v1", "kind": kind, "metadata": map[string]any{"name": "app", "namespace": "test", "uid": "reviewed", "resourceVersion": "rv1", "finalizers": []any{"test.invalid/retain"}}, "spec": map[string]any{"replicas": int64(1), "selector": map[string]any{"matchLabels": map[string]any{"app": "demo"}}, "template": map[string]any{"metadata": map[string]any{"labels": map[string]any{"app": "demo"}, "annotations": map[string]any{"retained": "yes"}}, "spec": map[string]any{"containers": []any{map[string]any{"name": "app", "image": "example.invalid/current"}}}}}}}
	revision := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "apps/v1", "kind": "ReplicaSet", "metadata": map[string]any{"name": "app-old", "namespace": "test", "labels": map[string]any{"app": "demo"}, "annotations": map[string]any{"deployment.kubernetes.io/revision": "2"}, "ownerReferences": []any{map[string]any{"apiVersion": "apps/v1", "kind": "Deployment", "name": "app", "uid": "reviewed", "controller": true}}}, "spec": map[string]any{"template": map[string]any{"metadata": map[string]any{"labels": map[string]any{"app": "demo", "pod-template-hash": "old"}}, "spec": map[string]any{"containers": []any{map[string]any{"name": "app", "image": "example.invalid/previous"}}}}}}}
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(k8sruntime.NewScheme(), map[schema.GroupVersionResource]string{
		{Group: "apps", Version: "v1", Resource: "deployments"}: "DeploymentList", {Group: "apps", Version: "v1", Resource: "statefulsets"}: "StatefulSetList", {Group: "apps", Version: "v1", Resource: "daemonsets"}: "DaemonSetList", {Group: "apps", Version: "v1", Resource: "replicasets"}: "ReplicaSetList",
	}, current, revision)
	return client, current
}

func conditionalWorkloadInput(action string) contractresource.ConditionalWorkloadActionInput {
	input := contractresource.ConditionalWorkloadActionInput{Preconditions: contractresource.ResourceMutationPreconditions{ExpectedUID: "reviewed", ExpectedResourceVersion: "rv1"}}
	if action == "scale" {
		input.Replicas = ptr.To(int32(3))
	}
	if action == "rollback" {
		input.Revision = "2"
	}
	return input
}

func TestConditionalWorkloadActionsPinIdentityAndPreserveMetadata(t *testing.T) {
	for _, test := range conditionalWorkloadCases {
		for _, dryRun := range []bool{false, true} {
			t.Run(test.kind+"/"+test.action+fmt.Sprint(dryRun), func(t *testing.T) {
				client, current := conditionalWorkloadFixture(test.kind)
				input := conditionalWorkloadInput(test.action)
				input.DryRun = dryRun
				writes := 0
				client.PrependReactor("update", test.resource, func(action clienttesting.Action) (bool, k8sruntime.Object, error) {
					writes++
					update, ok := action.(clienttesting.UpdateActionImpl)
					if !ok {
						t.Fatal("expected a Kubernetes update action")
					}
					desired, ok := update.GetObject().(*unstructured.Unstructured)
					if !ok || desired == nil {
						t.Fatal("expected an unstructured workload update")
					}
					if desired.GetUID() != current.GetUID() || desired.GetResourceVersion() != "rv1" || len(desired.GetFinalizers()) != 1 {
						t.Fatal("workload update lost caller identity or finalizers")
					}
					if dryRun {
						if len(update.GetUpdateOptions().DryRun) != 1 {
							t.Fatal("dry-run option missing")
						}
						return true, desired.DeepCopy(), nil
					}
					return false, nil, nil
				})
				result, err := ApplyConditionalWorkloadAction(t.Context(), client, "test", test.kind, "app", test.action, input, time.Date(2026, 10, 24, 0, 0, 0, 1, time.UTC))
				if err != nil || result.Applied == dryRun || result.DryRun != dryRun || result.UID != "reviewed" || result.Preconditions != input.Preconditions || writes != 1 {
					t.Fatalf("receipt=%+v error=%v writes=%d", result, err, writes)
				}
				live, err := client.Resource(schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: test.resource}).Namespace("test").Get(t.Context(), "app", metav1.GetOptions{})
				if err != nil {
					t.Fatal(err)
				}
				if dryRun {
					if !reflect.DeepEqual(current.Object, live.Object) {
						t.Fatal("dry-run changed live workload")
					}
					return
				}
				assertConditionalWorkloadEffect(t, test.action, live)
			})
		}
	}
}

func assertConditionalWorkloadEffect(t *testing.T, action string, live *unstructured.Unstructured) {
	t.Helper()
	switch action {
	case "restart":
		value, _, _ := unstructured.NestedString(live.Object, "spec", "template", "metadata", "annotations", "kubectl.kubernetes.io/restartedAt")
		if value == "" {
			t.Fatal("restart annotation missing")
		}
	case "scale":
		value, _, _ := unstructured.NestedInt64(live.Object, "spec", "replicas")
		if value != 3 {
			t.Fatal("replica count did not change")
		}
	case "rollback":
		if _, found, _ := unstructured.NestedString(live.Object, "spec", "template", "metadata", "labels", "pod-template-hash"); found {
			t.Fatal("rollback retained pod-template-hash")
		}
		containers, found, err := unstructured.NestedSlice(live.Object, "spec", "template", "spec", "containers")
		if err != nil || !found || len(containers) != 1 {
			t.Fatal("rollback did not preserve one container")
		}
		container, ok := containers[0].(map[string]any)
		if !ok || container["image"] != "example.invalid/previous" {
			t.Fatal("rollback did not use the original owned revision")
		}
	}
}

func TestConditionalWorkloadRejectsChangedIdentityAndInvalidActions(t *testing.T) {
	for _, scenario := range []string{"uid", "version", "missing guard", "owner", "namespace", "negative", "unexpected revision", "missing replicas", "unknown action", "unsupported kind", "missing revision", "signed revision"} {
		t.Run(scenario, func(t *testing.T) {
			client, current := conditionalWorkloadFixture("Deployment")
			input := conditionalWorkloadInput("scale")
			kind, action, namespace := "Deployment", "scale", "test"
			switch scenario {
			case "uid":
				input.Preconditions.ExpectedUID = "replaced"
			case "version":
				input.Preconditions.ExpectedResourceVersion = "previous"
			case "missing guard":
				input.Preconditions.ExpectedUID = ""
			case "owner":
				current.SetAnnotations(map[string]string{"argocd.argoproj.io/tracking-id": "external"})
				_ = client.Tracker().Update(schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}, current, "test")
			case "namespace":
				namespace = ""
			case "negative":
				input.Replicas = ptr.To(int32(-1))
			case "unexpected revision":
				input.Revision = "2"
			case "missing replicas":
				input.Replicas = nil
			case "unknown action":
				action = "delete"
			case "unsupported kind":
				kind = "DaemonSet"
			case "missing revision":
				action = "rollback"
				input.Replicas = nil
			case "signed revision":
				action = "rollback"
				input.Replicas = nil
				input.Revision = "+2"
			}
			_, err := ApplyConditionalWorkloadAction(t.Context(), client, namespace, kind, "app", action, input, time.Now())
			if err == nil {
				t.Fatal("invalid or changed workload request accepted")
			}
			for _, call := range client.Actions() {
				if call.GetVerb() == "update" || call.GetVerb() == "patch" || call.GetVerb() == "delete" {
					t.Fatal("rejected request wrote to Kubernetes")
				}
			}
		})
	}
}

func TestConditionalWorkloadConcurrentChangeDoesNotRefetchAndRetry(t *testing.T) {
	for _, test := range conditionalWorkloadCases {
		t.Run(test.kind+"/"+test.action, func(t *testing.T) {
			client, _ := conditionalWorkloadFixture(test.kind)
			writes := 0
			client.PrependReactor("update", test.resource, func(action clienttesting.Action) (bool, k8sruntime.Object, error) {
				writes++
				update, ok := action.(clienttesting.UpdateAction)
				if !ok {
					t.Fatal("expected a Kubernetes update action")
				}
				desired, ok := update.GetObject().(*unstructured.Unstructured)
				if !ok || desired == nil {
					t.Fatal("expected an unstructured workload update")
				}
				if desired.GetUID() != "reviewed" || desired.GetResourceVersion() != "rv1" {
					t.Fatal("caller preconditions were replaced after read")
				}
				return true, nil, apierrors.NewConflict(schema.GroupResource{Group: "apps", Resource: test.resource}, "app", fmt.Errorf("concurrent replacement or update"))
			})
			_, err := ApplyConditionalWorkloadAction(t.Context(), client, "test", test.kind, "app", test.action, conditionalWorkloadInput(test.action), time.Now())
			if !apierrors.IsConflict(err) || writes != 1 {
				t.Fatalf("error=%v writes=%d", err, writes)
			}
		})
	}
}

func TestConditionalRollbackRequiresAnOwnedUnambiguousRevision(t *testing.T) {
	for _, scenario := range []string{"missing", "non-controller", "wrong-uid", "other-namespace", "duplicate", "page-bound"} {
		t.Run(scenario, func(t *testing.T) {
			client, _ := conditionalWorkloadFixture("Deployment")
			input := conditionalWorkloadInput("rollback")
			gvr := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "replicasets"}
			object, err := client.Tracker().Get(gvr, "test", "app-old")
			if err != nil {
				t.Fatal(err)
			}
			original, ok := object.(*unstructured.Unstructured)
			if !ok || original == nil {
				t.Fatal("expected an unstructured rollback revision")
			}
			revision := original.DeepCopy()
			pages := 0
			switch scenario {
			case "missing":
				input.Revision = "9"
			case "non-controller":
				owners := revision.GetOwnerReferences()
				owners[0].Controller = ptr.To(false)
				revision.SetOwnerReferences(owners)
				_ = client.Tracker().Update(gvr, revision, "test")
			case "wrong-uid":
				owners := revision.GetOwnerReferences()
				owners[0].UID = "other-deployment"
				revision.SetOwnerReferences(owners)
				_ = client.Tracker().Update(gvr, revision, "test")
			case "other-namespace":
				_ = client.Tracker().Delete(gvr, "test", "app-old")
				revision.SetNamespace("other")
				_ = client.Tracker().Add(revision)
			case "duplicate":
				revision.SetName("app-duplicate")
				_ = client.Tracker().Add(revision)
			case "page-bound":
				client.PrependReactor("list", "replicasets", func(clienttesting.Action) (bool, k8sruntime.Object, error) {
					pages++
					list := &unstructured.UnstructuredList{}
					list.SetAPIVersion("apps/v1")
					list.SetKind("ReplicaSetList")
					list.SetContinue("more")
					return true, list, nil
				})
			}
			_, err = ApplyConditionalWorkloadAction(t.Context(), client, "test", "Deployment", "app", "rollback", input, time.Now())
			if err == nil {
				t.Fatal("unowned, absent or ambiguous rollback revision accepted")
			}
			if scenario == "page-bound" && pages != 100 {
				t.Fatalf("unbounded revision lookup: %d pages", pages)
			}
			for _, call := range client.Actions() {
				if call.GetVerb() == "update" || call.GetVerb() == "patch" || call.GetVerb() == "delete" {
					t.Fatal("invalid rollback revision wrote a workload")
				}
			}
		})
	}
}
