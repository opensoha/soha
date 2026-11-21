package hostedruntime

import (
	"errors"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	ktesting "k8s.io/client-go/testing"
)

const (
	testRevision = "rev_0123456789abcdef0123456789abcdef"
	testImage    = "registry.example.com/app@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

func TestNewAndServiceNameValidation(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	if _, err := New(nil, "team"); err == nil {
		t.Fatal("nil client accepted")
	}
	var typedNil *dynamicfake.FakeDynamicClient
	if _, err := New(typedNil, "team"); err == nil {
		t.Fatal("typed nil client accepted")
	}
	for _, namespace := range []string{"", "Team", "team_namespace", strings.Repeat("a", 64)} {
		if _, err := New(client, namespace); err == nil {
			t.Fatalf("invalid namespace %q accepted", namespace)
		}
	}
	adapter, err := New(client, "team-a")
	if err != nil {
		t.Fatal(err)
	}
	for _, revision := range []string{"", "rev_", "rev_0123", "REV_0123456789abcdef0123456789abcdef", "rev_0123456789ABCDEF0123456789abcdef", "rev_0123456789abcdef0123456789abcdeg"} {
		if _, err := adapter.ServiceName(revision); err == nil {
			t.Fatalf("invalid revision %q accepted", revision)
		}
	}
	name, err := adapter.ServiceName(testRevision)
	if err != nil || name != "soha-app-0123456789abcdef0123456789abcdef" {
		t.Fatalf("unexpected service name %q: %v", name, err)
	}
}

func TestPrepareCreatesStrictKnativeService(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	adapter, err := New(client, "team-a")
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.Prepare(t.Context(), Spec{RevisionID: testRevision, Image: testImage, EnvironmentSecret: "app-env"}); err != nil {
		t.Fatal(err)
	}
	service, err := client.Resource(knativeServiceGVR).Namespace("team-a").Get(t.Context(), "soha-app-0123456789abcdef0123456789abcdef", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if service.GetNamespace() != "team-a" || service.GetLabels()[managedByLabel] != managedByValue || service.GetLabels()[revisionIDLabel] != testRevision || service.GetLabels()[visibilityLabel] != clusterLocal {
		t.Fatalf("unexpected service identity: %#v", service.Object)
	}
	traffic, _, _ := unstructured.NestedSlice(service.Object, "spec", "traffic")
	if len(traffic) != 1 || traffic[0].(map[string]any)["latestRevision"] != true ||
		traffic[0].(map[string]any)["percent"] != int64(100) {
		t.Fatalf("unexpected traffic target: %#v", traffic)
	}
	template, _, _ := unstructured.NestedMap(service.Object, "spec", "template", "metadata")
	labels := template["labels"].(map[string]any)
	annotations := template["annotations"].(map[string]any)
	if labels[managedByLabel] != managedByValue || labels[revisionIDLabel] != testRevision {
		t.Fatalf("unexpected template labels: %#v", labels)
	}
	if annotations["autoscaling.knative.dev/min-scale"] != "0" || annotations["autoscaling.knative.dev/max-scale"] != "2" || annotations["autoscaling.knative.dev/scale-down-delay"] != "10m" {
		t.Fatalf("unexpected autoscaling: %#v", annotations)
	}
	pod, _, _ := unstructured.NestedMap(service.Object, "spec", "template", "spec")
	if pod["automountServiceAccountToken"] != false || pod["containerConcurrency"] != int64(10) || pod["timeoutSeconds"] != int64(300) || pod["responseStartTimeoutSeconds"] != int64(60) || pod["idleTimeoutSeconds"] != int64(60) {
		t.Fatalf("unexpected pod controls: %#v", pod)
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(pod, "terminationGracePeriodSeconds"); found {
		t.Fatal("termination grace period was supplied")
	}
	if _, found, _ := unstructured.NestedString(pod, "serviceAccountName"); found {
		t.Fatal("service account was supplied")
	}
	containers, _, _ := unstructured.NestedSlice(pod, "containers")
	if len(containers) != 1 {
		t.Fatalf("unexpected containers: %#v", containers)
	}
	container := containers[0].(map[string]any)
	if container["image"] != testImage {
		t.Fatalf("unexpected container: %#v", container)
	}
	limits, _, _ := unstructured.NestedStringMap(container, "resources", "limits")
	if limits["cpu"] != "500m" || limits["memory"] != "512Mi" {
		t.Fatalf("unexpected resource limits: %#v", limits)
	}
	ports, _, _ := unstructured.NestedSlice(container, "ports")
	if len(ports) != 1 || ports[0].(map[string]any)["containerPort"] != int64(8080) {
		t.Fatalf("unexpected ports: %#v", ports)
	}
	probe, _, _ := unstructured.NestedMap(container, "readinessProbe", "httpGet")
	if probe["path"] != "/healthz" || probe["port"] != int64(8080) {
		t.Fatalf("unexpected readiness probe: %#v", probe)
	}
	security, _, _ := unstructured.NestedMap(container, "securityContext")
	if security["allowPrivilegeEscalation"] != false || security["readOnlyRootFilesystem"] != true || security["runAsNonRoot"] != true || security["runAsUser"] != int64(65532) || security["seccompProfile"].(map[string]any)["type"] != "RuntimeDefault" {
		t.Fatalf("unexpected container security: %#v", security)
	}
	capabilities, _, _ := unstructured.NestedSlice(security, "capabilities", "drop")
	if len(capabilities) != 1 || capabilities[0] != "ALL" {
		t.Fatalf("unexpected capabilities: %#v", capabilities)
	}
	envFrom, _, _ := unstructured.NestedSlice(container, "envFrom")
	if len(envFrom) != 1 || envFrom[0].(map[string]any)["secretRef"].(map[string]any)["name"] != "app-env" {
		t.Fatalf("unexpected environment secret: %#v", envFrom)
	}
	volumes, _, _ := unstructured.NestedSlice(pod, "volumes")
	if len(volumes) != 1 {
		t.Fatalf("unexpected volumes: %#v", volumes)
	}
	volume := volumes[0].(map[string]any)
	if volume["name"] != "tmp" || volume["emptyDir"].(map[string]any)["medium"] != "Memory" || volume["emptyDir"].(map[string]any)["sizeLimit"] != "32Mi" {
		t.Fatalf("unexpected /tmp volume: %#v", volume)
	}
	pod["serviceAccountName"] = "default"
	pod["enableServiceLinks"] = true
	pod["dnsPolicy"] = "ClusterFirst"
	pod["schedulerName"] = "default-scheduler"
	pod["restartPolicy"] = "Always"
	if err := unstructured.SetNestedMap(service.Object, pod, "spec", "template", "spec"); err != nil {
		t.Fatal(err)
	}
	if err := client.Tracker().Update(knativeServiceGVR, service, "team-a"); err != nil {
		t.Fatal(err)
	}
	if err := adapter.Prepare(t.Context(), Spec{RevisionID: testRevision, Image: testImage, EnvironmentSecret: "app-env"}); err != nil {
		t.Fatalf("matching duplicate with admission defaults rejected: %v", err)
	}
}

func TestPrepareRejectsInvalidAndConflictingInput(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	adapter, err := New(client, "team-a")
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range []Spec{
		{RevisionID: "rev_0123", Image: testImage},
		{RevisionID: testRevision, Image: "registry.example.com/app:latest"},
		{RevisionID: testRevision, Image: strings.Replace(testImage, "sha256:", "sha512:", 1)},
		{RevisionID: testRevision, Image: strings.Replace(testImage, "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", strings.Repeat("g", 64), 1)},
		{RevisionID: testRevision, Image: testImage, EnvironmentSecret: "Bad_Name"},
	} {
		if err := adapter.Prepare(t.Context(), spec); err == nil {
			t.Fatalf("invalid spec accepted: %#v", spec)
		}
	}
	if err := adapter.Prepare(t.Context(), Spec{RevisionID: testRevision, Image: testImage}); err != nil {
		t.Fatal(err)
	}
	service, err := client.Resource(knativeServiceGVR).Namespace("team-a").Get(t.Context(), "soha-app-0123456789abcdef0123456789abcdef", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	changed := service.DeepCopy()
	container := changed.Object["spec"].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any)["containers"].([]any)[0].(map[string]any)
	container["securityContext"].(map[string]any)["allowPrivilegeEscalation"] = true
	if err := client.Tracker().Update(knativeServiceGVR, changed, "team-a"); err != nil {
		t.Fatal(err)
	}
	if err := adapter.Prepare(t.Context(), Spec{RevisionID: testRevision, Image: testImage}); err == nil {
		t.Fatal("security drift was reconciled")
	}
	foreign := service.DeepCopy()
	foreign.SetLabels(map[string]string{managedByLabel: "someone-else", revisionIDLabel: testRevision})
	if err := client.Tracker().Update(knativeServiceGVR, foreign, "team-a"); err != nil {
		t.Fatal(err)
	}
	if err := adapter.Prepare(t.Context(), Spec{RevisionID: testRevision, Image: testImage}); err == nil {
		t.Fatal("foreign service was taken over")
	}
}

func TestReadyUsesCurrentGenerationAndTerminalCondition(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	adapter, err := New(client, "team-a")
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.Prepare(t.Context(), Spec{RevisionID: testRevision, Image: testImage}); err != nil {
		t.Fatal(err)
	}
	name := "soha-app-0123456789abcdef0123456789abcdef"
	updateStatus := func(generation, observed int64, conditionStatus, reason string, extra ...any) {
		service, err := client.Resource(knativeServiceGVR).Namespace("team-a").Get(t.Context(), name, metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		service.SetGeneration(generation)
		condition := map[string]any{"type": "Ready", "status": conditionStatus}
		if reason != "" {
			condition["reason"] = reason
		}
		if err := unstructured.SetNestedSlice(service.Object, append([]any{condition}, extra...), "status", "conditions"); err != nil {
			t.Fatal(err)
		}
		if err := unstructured.SetNestedField(service.Object, observed, "status", "observedGeneration"); err != nil {
			t.Fatal(err)
		}
		if err := client.Tracker().Update(knativeServiceGVR, service, "team-a"); err != nil {
			t.Fatal(err)
		}
	}
	updateStatus(2, 1, "True", "")
	if ready, err := adapter.Ready(t.Context(), testRevision); err != nil || ready {
		t.Fatalf("stale generation ready=%v err=%v", ready, err)
	}
	updateStatus(2, 2, "Unknown", "Deploying")
	if ready, err := adapter.Ready(t.Context(), testRevision); err != nil || ready {
		t.Fatalf("pending condition ready=%v err=%v", ready, err)
	}
	updateStatus(2, 2, "True", "")
	if ready, err := adapter.Ready(t.Context(), testRevision); err != nil || !ready {
		t.Fatalf("current ready condition ready=%v err=%v", ready, err)
	}
	failed := map[string]any{"type": "ConfigurationsReady", "status": "False", "reason": "RevisionFailed"}
	updateStatus(2, 1, "False", "RevisionMissing", failed)
	if ready, err := adapter.Ready(t.Context(), testRevision); err != nil || ready {
		t.Fatalf("stale failure ready=%v err=%v", ready, err)
	}
	updateStatus(2, 2, "False", "RevisionMissing", failed)
	if ready, err := adapter.Ready(t.Context(), testRevision); !errors.Is(err, ErrRevisionFailed) || ready {
		t.Fatalf("terminal revision failure ready=%v err=%v", ready, err)
	}
	updateStatus(2, 2, "False", "RevisionMissing")
	if ready, err := adapter.Ready(t.Context(), testRevision); err == nil || errors.Is(err, ErrRevisionFailed) || ready {
		t.Fatalf("route failure ready=%v err=%v", ready, err)
	}
}

func TestDeleteIsOwnedAndUIDGuarded(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	adapter, err := New(client, "team-a")
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.Delete(t.Context(), testRevision); err != nil {
		t.Fatalf("missing service was not idempotent: %v", err)
	}
	if err := adapter.Prepare(t.Context(), Spec{RevisionID: testRevision, Image: testImage}); err != nil {
		t.Fatal(err)
	}
	name := "soha-app-0123456789abcdef0123456789abcdef"
	service, err := client.Resource(knativeServiceGVR).Namespace("team-a").Get(t.Context(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	service.SetUID(types.UID("revision-uid"))
	if err := client.Tracker().Update(knativeServiceGVR, service, "team-a"); err != nil {
		t.Fatal(err)
	}
	var deleted metav1.DeleteOptions
	client.PrependReactor("delete", "services", func(action ktesting.Action) (bool, runtime.Object, error) {
		deleted = action.(interface{ GetDeleteOptions() metav1.DeleteOptions }).GetDeleteOptions()
		return false, nil, nil
	})
	if err := adapter.Delete(t.Context(), testRevision); err != nil {
		t.Fatal(err)
	}
	if deleted.Preconditions == nil || deleted.Preconditions.UID == nil || *deleted.Preconditions.UID != types.UID("revision-uid") {
		t.Fatalf("missing UID precondition: %#v", deleted)
	}
	if _, err := client.Resource(knativeServiceGVR).Namespace("team-a").Get(t.Context(), name, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("owned service was not deleted: %v", err)
	}
}

func TestDeleteRejectsForeignService(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	adapter, err := New(client, "team-a")
	if err != nil {
		t.Fatal(err)
	}
	foreign := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "serving.knative.dev/v1",
		"kind":       "Service",
		"metadata": map[string]any{
			"name":      "soha-app-0123456789abcdef0123456789abcdef",
			"namespace": "team-a",
			"labels":    map[string]any{managedByLabel: "someone-else", revisionIDLabel: testRevision},
		},
	}}
	foreign.SetUID(types.UID("foreign-uid"))
	if _, err := client.Resource(knativeServiceGVR).Namespace("team-a").Create(t.Context(), foreign, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := adapter.Delete(t.Context(), testRevision); err == nil {
		t.Fatal("foreign service was deleted")
	}
}
