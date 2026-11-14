package resourcebackend

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	cr "github.com/opensoha/soha/contracts/resource"
	shared "github.com/opensoha/soha/contracts/resource/runtime"
	"github.com/opensoha/soha/internal/platform/apperrors"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	kclient "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

func graphAdapter(ctx context.Context, client kclient.Interface, kind, name string) (cr.ResourceGraph, error) {
	return directResourceGraph(ctx, client, nil, "cluster-a", "team-a", kind, name)
}

// Both connection adapters use these same typed fixture objects and compare their
// nodes/edges to the shared collector; no connection-local relationship rules remain.
func graphAdapterObjects() []runtime.Object {
	meta := metav1.ObjectMeta{Name: "api", Namespace: "team-a", UID: "deployment-uid"}
	control := true
	labels := map[string]string{"app": "api"}
	spec := corev1.PodSpec{Containers: []corev1.Container{{Name: "api", EnvFrom: []corev1.EnvFromSource{
		{ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "api-config"}}},
		{ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "kube-root-ca.crt"}}},
		{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "api-secret"}}},
	}}}, Volumes: []corev1.Volume{{Name: "data", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "api-data"}}}}}
	template := corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: labels}, Spec: spec}
	return []runtime.Object{
		&appsv1.Deployment{ObjectMeta: meta, Spec: appsv1.DeploymentSpec{Template: template}},
		&appsv1.StatefulSet{ObjectMeta: meta, Spec: appsv1.StatefulSetSpec{Template: template}},
		&appsv1.DaemonSet{ObjectMeta: meta, Spec: appsv1.DaemonSetSpec{Template: template}},
		&appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "api-rs", Namespace: "team-a", UID: "rs-uid", OwnerReferences: []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "Deployment", Name: "api", UID: "deployment-uid", Controller: &control}}}, Spec: appsv1.ReplicaSetSpec{Template: template}},
		&corev1.ReplicationController{ObjectMeta: meta, Spec: corev1.ReplicationControllerSpec{Template: &template}},
		&batchv1.Job{ObjectMeta: meta, Spec: batchv1.JobSpec{Template: template}},
		&batchv1.CronJob{ObjectMeta: meta, Spec: batchv1.CronJobSpec{JobTemplate: batchv1.JobTemplateSpec{Spec: batchv1.JobSpec{Template: template}}}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "api-pod", Namespace: "team-a", Labels: labels, OwnerReferences: []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "ReplicaSet", Name: "api-rs", UID: "rs-uid", Controller: &control}}}, Spec: spec},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "debug", Namespace: "team-a"}, Spec: spec},
		&corev1.Service{ObjectMeta: meta, Spec: corev1.ServiceSpec{Selector: labels}},
		&networkingv1.Ingress{ObjectMeta: meta, Spec: networkingv1.IngressSpec{DefaultBackend: &networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{Name: "api"}}}},
		&autoscalingv2.HorizontalPodAutoscaler{ObjectMeta: meta, Spec: autoscalingv2.HorizontalPodAutoscalerSpec{ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{APIVersion: "apps/v1", Kind: "Deployment", Name: "api"}}},
		&policyv1.PodDisruptionBudget{ObjectMeta: meta, Spec: policyv1.PodDisruptionBudgetSpec{Selector: &metav1.LabelSelector{MatchLabels: labels}}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "api-config", Namespace: "team-a"}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "kube-root-ca.crt", Namespace: "team-a"}},
		&corev1.Event{
			ObjectMeta:     metav1.ObjectMeta{Name: "scheduled", Namespace: "team-a", CreationTimestamp: metav1.NewTime(time.Date(2026, time.November, 5, 10, 0, 0, 0, time.UTC))},
			InvolvedObject: corev1.ObjectReference{Kind: "Pod", Namespace: "team-a", Name: "api-pod"}, Reason: "Scheduled", Message: "Pod scheduled",
		},
	}
}

func TestResourceGraphAdapterUsesSharedCollector(t *testing.T) {
	for _, root := range []struct{ kind, name string }{
		{"Deployment", "api"}, {"StatefulSet", "api"}, {"DaemonSet", "api"}, {"ReplicaSet", "api-rs"},
		{"ReplicationController", "api"}, {"Job", "api"}, {"CronJob", "api"}, {"Pod", "api-pod"},
	} {
		t.Run(root.kind, func(t *testing.T) {
			client := fake.NewClientset(graphAdapterObjects()...)
			got, err := graphAdapter(context.Background(), client, strings.ToLower(root.kind), root.name)
			if err != nil {
				t.Fatal(err)
			}
			want, err := shared.BuildKubernetesResourceGraph(context.Background(), client, cr.ResourceRef{ClusterID: "cluster-a", Namespace: "team-a", Kind: root.kind, Name: root.name})
			if err != nil {
				t.Fatal(err)
			}
			if got.RootID != want.RootID || !reflect.DeepEqual(got.Nodes, want.Nodes) || !reflect.DeepEqual(got.Edges, want.Edges) || !reflect.DeepEqual(got.Evidence, want.Evidence) {
				t.Fatalf("adapter diverged: got=%+v want=%+v", got, want)
			}
			for _, node := range got.Nodes {
				if node.Resource.ClusterID != "cluster-a" {
					t.Fatal("cluster identity changed")
				}
			}
			for _, action := range client.Actions() {
				if action.GetResource().Resource == "secrets" {
					t.Fatal("Secret was read")
				}
			}
		})
	}
}

func TestResourceGraphAdapterRetainsDependenciesAndEvidence(t *testing.T) {
	for _, root := range []struct {
		kind, name string
		kinds      []string
	}{
		{"Deployment", "api", []string{"Deployment", "ReplicaSet", "Pod", "Service", "ConfigMap", "Secret", "PersistentVolumeClaim", "HorizontalPodAutoscaler"}},
		{"Pod", "debug", []string{"ConfigMap", "Secret", "PersistentVolumeClaim"}},
	} {
		t.Run(root.kind+root.name, func(t *testing.T) {
			graph, err := graphAdapter(context.Background(), fake.NewClientset(graphAdapterObjects()...), root.kind, root.name)
			if err != nil {
				t.Fatal(err)
			}
			found := map[string]bool{}
			ca := false
			for _, n := range graph.Nodes {
				found[n.Resource.Kind] = true
				if n.Resource.Kind == "ConfigMap" && n.Resource.Name == "kube-root-ca.crt" {
					ca = true
				}
			}
			for _, kind := range root.kinds {
				if !found[kind] {
					t.Fatalf("missing %s: %+v", kind, graph)
				}
			}
			// Declared configuration references, including the actual root CA, are complete.
			if root.kind != "Service" && !ca {
				t.Fatal("actual root CA reference omitted")
			}
			if root.name != "debug" && (len(graph.Evidence) != 1 || graph.Evidence[0].Type != "kubernetes-event" || graph.Evidence[0].ObservedAt != "2026-11-05T10:00:00Z") {
				t.Fatalf("lost event evidence: %+v", graph.Evidence)
			}
		})
	}
}

func TestResourceGraphAdapterFailures(t *testing.T) {
	for _, tc := range []struct {
		name, kind, object string
		want               error
	}{
		{"missing", "Pod", "absent", apperrors.ErrNotFound}, {"invalid scope", "Pod", "", apperrors.ErrInvalidArgument}, {"unsupported", "CustomKind", "api", apperrors.ErrUnsupportedOperation},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := graphAdapter(context.Background(), fake.NewClientset(), tc.kind, tc.object)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error=%v want=%v", err, tc.want)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := graphAdapter(ctx, fake.NewClientset(graphAdapterObjects()...), "Pod", "api-pod")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	client := fake.NewClientset(graphAdapterObjects()...)
	client.PrependReactor("list", "services", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "services"}, "", errors.New("private provider detail"))
	})
	graph, err := graphAdapter(context.Background(), client, "Pod", "api-pod")
	if err != nil || len(graph.Warnings) == 0 || len(graph.Nodes) == 0 {
		t.Fatalf("partial source failure=%+v %v", graph, err)
	}
	if strings.Contains(strings.Join(graph.Warnings, " "), "private provider detail") {
		t.Fatal("provider detail leaked")
	}
}
