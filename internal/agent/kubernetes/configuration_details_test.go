package kubernetes

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	apiresource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
)

func TestGetHorizontalPodAutoscalerDetail(t *testing.T) {
	utilization := int32(70)
	current := int32(55)
	client := &Client{typed: kubernetesfake.NewSimpleClientset(&autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "infra", Labels: map[string]string{"app": "api"}},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Kind: "Deployment", Name: "api"},
			MaxReplicas:    5,
			Metrics: []autoscalingv2.MetricSpec{{
				Type: autoscalingv2.ResourceMetricSourceType,
				Resource: &autoscalingv2.ResourceMetricSource{
					Name: corev1.ResourceCPU,
					Target: autoscalingv2.MetricTarget{
						Type:               autoscalingv2.UtilizationMetricType,
						AverageUtilization: &utilization,
					},
				},
			}},
		},
		Status: autoscalingv2.HorizontalPodAutoscalerStatus{
			CurrentMetrics: []autoscalingv2.MetricStatus{{
				Type: autoscalingv2.ResourceMetricSourceType,
				Resource: &autoscalingv2.ResourceMetricStatus{
					Name:    corev1.ResourceCPU,
					Current: autoscalingv2.MetricValueStatus{AverageUtilization: &current},
				},
			}},
		},
	})}

	detail, err := client.GetHorizontalPodAutoscalerDetail(context.Background(), "infra", "api")
	if err != nil {
		t.Fatal(err)
	}
	if detail.MinReplicas != 1 || len(detail.Metrics) != 1 || detail.Metrics[0].Target != "70%" || detail.Metrics[0].Current != "55%" {
		t.Fatalf("detail = %#v", detail)
	}
}

func TestGetPodDisruptionBudgetDetailListsSelectedPodsAndResolvesCommonOwner(t *testing.T) {
	controller := true
	deploymentUID := types.UID("deployment-uid")
	replicaSetUID := types.UID("replicaset-uid")
	objects := []runtime.Object{
		&policyv1.PodDisruptionBudget{
			ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "infra"},
			Spec: policyv1.PodDisruptionBudgetSpec{Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "api"},
				MatchExpressions: []metav1.LabelSelectorRequirement{{
					Key: "environment", Operator: metav1.LabelSelectorOpIn, Values: []string{"prod"},
				}},
			}},
		},
		&appsv1.ReplicaSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: "api-rs", Namespace: "infra", UID: replicaSetUID,
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: "apps/v1", Kind: "Deployment", Name: "api", UID: deploymentUID, Controller: &controller,
				}},
			},
		},
	}
	for _, name := range []string{"api-1", "api-2"} {
		objects = append(objects, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: "infra", Labels: map[string]string{"app": "api", "environment": "prod"},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "apps/v1", Kind: "ReplicaSet", Name: "api-rs", UID: replicaSetUID, Controller: &controller,
			}},
		}})
	}
	objects = append(objects, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name: "api-dev", Namespace: "infra", Labels: map[string]string{"app": "api", "environment": "dev"},
	}})
	fakeClient := kubernetesfake.NewSimpleClientset(objects...)
	client := &Client{typed: fakeClient}

	detail, err := client.GetPodDisruptionBudgetDetail(context.Background(), "infra", "api")
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Pods) != 2 || !strings.Contains(detail.Selector, "environment in (prod)") {
		t.Fatalf("selected pods/selector = %#v / %q", detail.Pods, detail.Selector)
	}
	if detail.Workload == nil || detail.Workload.Kind != "Deployment" || detail.Workload.Name != "api" {
		t.Fatalf("workload = %#v", detail.Workload)
	}
}

func TestGetPodDisruptionBudgetDetailDistinguishesAbsentAndEmptySelector(t *testing.T) {
	const namespace = "infra"
	fakeClient := kubernetesfake.NewSimpleClientset(
		&policyv1.PodDisruptionBudget{ObjectMeta: metav1.ObjectMeta{Name: "absent", Namespace: namespace}},
		&policyv1.PodDisruptionBudget{
			ObjectMeta: metav1.ObjectMeta{Name: "empty", Namespace: namespace},
			Spec:       policyv1.PodDisruptionBudgetSpec{Selector: &metav1.LabelSelector{}},
		},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod", Namespace: namespace}},
	)
	client := &Client{typed: fakeClient}
	absent, err := client.GetPodDisruptionBudgetDetail(context.Background(), namespace, "absent")
	if err != nil || len(absent.Pods) != 0 || absent.Workload != nil {
		t.Fatalf("absent selector detail = %#v, %v", absent, err)
	}
	for _, action := range fakeClient.Actions() {
		if action.Matches("list", "pods") {
			t.Fatal("absent selector listed namespace Pods")
		}
	}
	empty, err := client.GetPodDisruptionBudgetDetail(context.Background(), namespace, "empty")
	if err != nil || len(empty.Pods) != 1 {
		t.Fatalf("empty selector detail = %#v, %v", empty, err)
	}
}

func TestGetWebhookDetailsExposeConfigurationWithoutCABytes(t *testing.T) {
	path := "/admit"
	port := int32(9443)
	url := "https://validator.example/admit"
	client := &Client{typed: kubernetesfake.NewSimpleClientset(
		&admissionregistrationv1.MutatingWebhookConfiguration{
			ObjectMeta: metav1.ObjectMeta{Name: "mutator"},
			Webhooks: []admissionregistrationv1.MutatingWebhook{{
				Name: "mutator.example",
				ClientConfig: admissionregistrationv1.WebhookClientConfig{
					CABundle: []byte("private-ca-bytes"),
					Service: &admissionregistrationv1.ServiceReference{
						Name: "mutator", Namespace: "system", Path: &path, Port: &port,
					},
				},
			}},
		},
		&admissionregistrationv1.ValidatingWebhookConfiguration{
			ObjectMeta: metav1.ObjectMeta{Name: "validator"},
			Webhooks: []admissionregistrationv1.ValidatingWebhook{{
				Name:         "validator.example",
				ClientConfig: admissionregistrationv1.WebhookClientConfig{URL: &url},
			}},
		},
	)}

	mutating, err := client.GetMutatingWebhookConfigurationDetail(context.Background(), "mutator")
	if err != nil {
		t.Fatal(err)
	}
	validating, err := client.GetValidatingWebhookConfigurationDetail(context.Background(), "validator")
	if err != nil {
		t.Fatal(err)
	}
	if !mutating.Webhooks[0].CABundleConfigured || mutating.Webhooks[0].ServiceNamespace != "system" || mutating.Webhooks[0].ServicePort != 9443 {
		t.Fatalf("mutating detail = %#v", mutating)
	}
	if validating.Webhooks[0].URL != url {
		t.Fatalf("validating detail = %#v", validating)
	}
	raw, err := json.Marshal(mutating)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "private-ca-bytes") {
		t.Fatalf("response exposes CA bytes: %s", raw)
	}
}

func TestGetQuotaAndLimitRangeDetails(t *testing.T) {
	client := &Client{typed: kubernetesfake.NewSimpleClientset(
		&corev1.ResourceQuota{
			ObjectMeta: metav1.ObjectMeta{Name: "team", Namespace: "infra", Labels: map[string]string{"team": "platform"}},
			Status: corev1.ResourceQuotaStatus{
				Hard: corev1.ResourceList{corev1.ResourcePods: apiresource.MustParse("10")},
				Used: corev1.ResourceList{corev1.ResourcePods: apiresource.MustParse("3")},
			},
		},
		&corev1.LimitRange{
			ObjectMeta: metav1.ObjectMeta{Name: "defaults", Namespace: "infra"},
			Spec: corev1.LimitRangeSpec{Limits: []corev1.LimitRangeItem{{
				Type:    corev1.LimitTypeContainer,
				Default: corev1.ResourceList{corev1.ResourceCPU: apiresource.MustParse("500m")},
			}}},
		},
	)}

	quota, err := client.GetResourceQuotaDetail(context.Background(), "infra", "team")
	if err != nil {
		t.Fatal(err)
	}
	limitRange, err := client.GetLimitRangeDetail(context.Background(), "infra", "defaults")
	if err != nil {
		t.Fatal(err)
	}
	if quota.Hard["pods"] != "10" || quota.Used["pods"] != "3" || quota.Labels["team"] != "platform" {
		t.Fatalf("quota = %#v", quota)
	}
	if len(limitRange.Rules) != 1 || limitRange.Rules[0].Default["cpu"] != "500m" {
		t.Fatalf("limit range = %#v", limitRange)
	}
}
