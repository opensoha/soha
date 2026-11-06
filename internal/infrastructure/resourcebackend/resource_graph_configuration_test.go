package resourcebackend

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	cr "github.com/opensoha/soha-contracts/resource"
	shared "github.com/opensoha/soha-contracts/resource/runtime"
	"github.com/opensoha/soha/internal/platform/apperrors"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	nodev1 "k8s.io/api/node/v1"
	policyv1 "k8s.io/api/policy/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	kclient "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/metadata"
	metadatafake "k8s.io/client-go/metadata/fake"
	clienttesting "k8s.io/client-go/testing"
)

func configurationGraphAdapter(ctx context.Context, typed kclient.Interface, meta metadata.Interface, namespace, kind, name string) (cr.ResourceGraph, error) {
	return directResourceGraph(ctx, typed, meta, "cluster-a", namespace, kind, name)
}

func configurationAdapterObjects() []runtime.Object {
	meta := metav1.ObjectMeta{Name: "api", Namespace: "team-a", UID: "api-uid", Labels: map[string]string{"app": "api"}}
	class := "sandbox"
	spec := corev1.PodSpec{PriorityClassName: "critical", RuntimeClassName: &class, Containers: []corev1.Container{{Name: "api", EnvFrom: []corev1.EnvFromSource{
		{ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "settings"}}},
		{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "credentials"}}},
	}}}}
	return []runtime.Object{
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: "team-a", UID: "config-uid"}, Data: map[string]string{"private": "must-not-appear"}},
		&corev1.Pod{ObjectMeta: meta, Spec: spec},
		&appsv1.Deployment{ObjectMeta: meta, Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: spec}}},
		&autoscalingv2.HorizontalPodAutoscaler{ObjectMeta: meta, Spec: autoscalingv2.HorizontalPodAutoscalerSpec{ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{APIVersion: "apps/v1", Kind: "Deployment", Name: "api"}}},
		&policyv1.PodDisruptionBudget{ObjectMeta: meta, Spec: policyv1.PodDisruptionBudgetSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "api"}}}},
		&corev1.ResourceQuota{ObjectMeta: meta}, &corev1.LimitRange{ObjectMeta: meta},
		&schedulingv1.PriorityClass{ObjectMeta: metav1.ObjectMeta{Name: "critical"}},
		&nodev1.RuntimeClass{ObjectMeta: metav1.ObjectMeta{Name: "sandbox"}, Handler: "sandbox"},
		&coordinationv1.Lease{ObjectMeta: meta},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "team-a"}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "admission", Namespace: "team-a"}},
		&admissionv1.MutatingWebhookConfiguration{ObjectMeta: metav1.ObjectMeta{Name: "mutator"}, Webhooks: []admissionv1.MutatingWebhook{{Name: "mutate.example.com", ClientConfig: admissionv1.WebhookClientConfig{Service: &admissionv1.ServiceReference{Namespace: "team-a", Name: "admission"}}}}},
		&admissionv1.ValidatingWebhookConfiguration{ObjectMeta: metav1.ObjectMeta{Name: "validator"}, Webhooks: []admissionv1.ValidatingWebhook{{Name: "validate.example.com", ClientConfig: admissionv1.WebhookClientConfig{Service: &admissionv1.ServiceReference{Namespace: "team-a", Name: "admission"}}}}},
	}
}

func configurationMetadataClient(t *testing.T) *metadatafake.FakeMetadataClient {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := metav1.AddMetaToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return metadatafake.NewSimpleMetadataClient(scheme,
		&metav1.PartialObjectMetadata{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"}, ObjectMeta: metav1.ObjectMeta{Name: "credentials", Namespace: "team-a", UID: "secret-uid"}},
		&metav1.PartialObjectMetadata{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Namespace"}, ObjectMeta: metav1.ObjectMeta{Name: "team-a"}},
		&metav1.PartialObjectMetadata{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Service"}, ObjectMeta: metav1.ObjectMeta{Name: "admission", Namespace: "team-a"}},
	)
}

func TestConfigurationGraphAdapterParity(t *testing.T) {
	for _, root := range []struct{ kind, name, namespace string }{
		{"ConfigMap", "settings", "team-a"}, {"Secret", "credentials", "team-a"},
		{"ResourceQuota", "api", "team-a"}, {"LimitRange", "api", "team-a"},
		{"HorizontalPodAutoscaler", "api", "team-a"}, {"PodDisruptionBudget", "api", "team-a"},
		{"PriorityClass", "critical", ""}, {"RuntimeClass", "sandbox", ""}, {"Lease", "api", "team-a"},
		{"MutatingWebhookConfiguration", "mutator", ""}, {"ValidatingWebhookConfiguration", "validator", ""},
	} {
		t.Run(root.kind, func(t *testing.T) {
			client := fake.NewClientset(configurationAdapterObjects()...)
			meta := configurationMetadataClient(t)
			got, err := configurationGraphAdapter(context.Background(), client, meta, root.namespace, strings.ToLower(root.kind), root.name)
			if err != nil {
				t.Fatal(err)
			}
			want, err := shared.BuildKubernetesResourceGraphWithMetadata(context.Background(), fake.NewClientset(configurationAdapterObjects()...), configurationMetadataClient(t), cr.ResourceRef{ClusterID: "cluster-a", Namespace: root.namespace, Kind: root.kind, Name: root.name})
			if err != nil {
				t.Fatal(err)
			}
			if got.RootID != want.RootID || !reflect.DeepEqual(got.Nodes, want.Nodes) || !reflect.DeepEqual(got.Edges, want.Edges) || !reflect.DeepEqual(got.Evidence, want.Evidence) || !reflect.DeepEqual(got.Warnings, want.Warnings) {
				t.Fatalf("adapter diverged: got=%+v want=%+v", got, want)
			}
			if len(got.Nodes) == 0 {
				t.Fatal("configuration root omitted")
			}
			for _, action := range client.Actions() {
				if action.GetResource().Resource == "secrets" {
					t.Fatal("typed Secret read")
				}
			}
			for _, action := range meta.Actions() {
				if action.GetVerb() != "get" {
					t.Fatalf("metadata scan: %+v", action)
				}
			}
			payload, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(payload), "must-not-appear") {
				t.Fatal("ConfigMap payload escaped projection")
			}
			if root.kind == "Secret" {
				if len(meta.Actions()) != 1 || meta.Actions()[0].GetResource().Resource != "secrets" || meta.Actions()[0].GetNamespace() != "team-a" {
					t.Fatalf("Secret root read was not metadata named get: %+v", meta.Actions())
				}
				for _, node := range got.Nodes {
					if node.ID == got.RootID && node.Resource.UID != "secret-uid" {
						t.Fatal("Secret metadata identity lost")
					}
				}
			}
		})
	}
}

func TestConfigurationGraphAdapterSecretFailuresAndScope(t *testing.T) {
	for _, tc := range []struct {
		name    string
		failure error
		want    error
	}{
		{"forbidden", apierrors.NewForbidden(schema.GroupResource{Resource: "secrets"}, "credentials", errors.New("private-provider-detail")), apperrors.ErrAccessDenied},
		{"missing", apierrors.NewNotFound(schema.GroupResource{Resource: "secrets"}, "credentials"), apperrors.ErrNotFound},
		{"cancelled", context.Canceled, context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			meta := configurationMetadataClient(t)
			meta.PrependReactor("get", "secrets", func(clienttesting.Action) (bool, runtime.Object, error) { return true, nil, tc.failure })
			client := fake.NewClientset()
			_, err := configurationGraphAdapter(context.Background(), client, meta, "team-a", "Secret", "credentials")
			if !errors.Is(err, tc.want) {
				t.Fatalf("error=%v want=%v", err, tc.want)
			}
			if len(client.Actions()) != 0 || len(meta.Actions()) != 1 {
				t.Fatalf("root failure continued reads: typed=%+v metadata=%+v", client.Actions(), meta.Actions())
			}
		})
	}
	for _, root := range []struct{ kind, namespace string }{{"Secret", ""}, {"ConfigMap", ""}, {"ResourceQuota", ""}, {"Lease", ""}, {"PriorityClass", "team-a"}, {"RuntimeClass", "team-a"}, {"MutatingWebhookConfiguration", "team-a"}, {"ValidatingWebhookConfiguration", "team-a"}} {
		t.Run(root.kind+"-invalid-scope", func(t *testing.T) {
			client := fake.NewClientset()
			meta := configurationMetadataClient(t)
			_, err := configurationGraphAdapter(context.Background(), client, meta, root.namespace, root.kind, "api")
			if !errors.Is(err, apperrors.ErrInvalidArgument) || len(client.Actions()) != 0 || len(meta.Actions()) != 0 {
				t.Fatalf("invalid scope reached provider: err=%v typed=%+v metadata=%+v", err, client.Actions(), meta.Actions())
			}
		})
	}
}
