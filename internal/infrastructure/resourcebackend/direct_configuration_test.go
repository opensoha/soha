package resourcebackend

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	domainresource "github.com/opensoha/soha/internal/domain/resource"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	nodev1 "k8s.io/api/node/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestConfigurationMappings(t *testing.T) {
	t.Parallel()

	created := metav1.NewTime(time.Now().Add(-30 * time.Minute))
	immutable := true
	configMap := mapConfigMap(corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "app-config", Namespace: "team-a", CreationTimestamp: created}, Immutable: &immutable, Data: map[string]string{"app.yaml": "debug: false"}, BinaryData: map[string][]byte{"cert": []byte("bytes")}})
	if configMap.DataEntries != 1 {
		t.Fatalf("configMap = %#v", configMap)
	}
	secret := mapSecret(corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "app-secret", Namespace: "team-a", CreationTimestamp: created}, Immutable: &immutable, Type: corev1.SecretTypeTLS, Data: map[string][]byte{"tls.crt": []byte("cert"), "tls.key": []byte("key")}})
	if secret.Type != "kubernetes.io/tls" || secret.DataEntries != 2 || secret.Immutable == nil || !*secret.Immutable {
		t.Fatalf("secret = %#v", secret)
	}
	ingressClass := mapIngressClass(networkingv1.IngressClass{ObjectMeta: metav1.ObjectMeta{Name: "nginx", CreationTimestamp: created, Annotations: map[string]string{"ingressclass.kubernetes.io/is-default-class": "TRUE"}}, Spec: networkingv1.IngressClassSpec{Controller: "k8s.io/ingress-nginx", Parameters: &networkingv1.IngressClassParametersReference{Kind: "IngressParameters", Name: "nginx-default"}}})
	if !ingressClass.IsDefault || ingressClass.Parameters != "IngressParameters/nginx-default" || ingressClass.Controller != "k8s.io/ingress-nginx" {
		t.Fatalf("ingressClass = %#v", ingressClass)
	}
	preemption := corev1.PreemptNever
	priorityClass := mapPriorityClass(schedulingv1.PriorityClass{ObjectMeta: metav1.ObjectMeta{Name: "critical", CreationTimestamp: created}, Value: 1000, GlobalDefault: true, PreemptionPolicy: &preemption, Description: "critical workloads"})
	if priorityClass.Value != 1000 || !priorityClass.GlobalDefault || priorityClass.PreemptionPolicy != "Never" {
		t.Fatalf("priorityClass = %#v", priorityClass)
	}
	runtimeClass := mapRuntimeClass(nodev1.RuntimeClass{ObjectMeta: metav1.ObjectMeta{Name: "kata", CreationTimestamp: created}, Handler: "kata-qemu"})
	if runtimeClass.Name != "kata" || runtimeClass.Handler != "kata-qemu" {
		t.Fatalf("runtimeClass = %#v", runtimeClass)
	}
}

func TestLightweightConfigurationTableMappings(t *testing.T) {
	t.Parallel()
	table := metav1.Table{
		ColumnDefinitions: []metav1.TableColumnDefinition{{Name: "Name"}, {Name: "Data"}, {Name: "Webhooks"}},
		Rows:              []metav1.TableRow{{Cells: []any{"app-config", int64(3), int64(2)}, Object: tableTestMetadata("app-config", "team-a")}},
	}
	configMaps, err := mapConfigMapTable(table)
	if err != nil || len(configMaps) != 1 || configMaps[0].Name != "app-config" || configMaps[0].Namespace != "team-a" || configMaps[0].DataEntries != 3 {
		t.Fatalf("mapConfigMapTable() = %#v, %v", configMaps, err)
	}
	webhooks, err := mapMutatingWebhookConfigurationTable(table)
	if err != nil || len(webhooks) != 1 || webhooks[0].Name != "app-config" || webhooks[0].Webhooks != 2 {
		t.Fatalf("mapMutatingWebhookConfigurationTable() = %#v, %v", webhooks, err)
	}
}

func TestMapSecretTableDoesNotRequireSecretData(t *testing.T) {
	t.Parallel()
	created := metav1.NewTime(time.Now().Add(-30 * time.Minute))
	table := metav1.Table{
		ColumnDefinitions: []metav1.TableColumnDefinition{
			{Name: "Name", Type: "string"},
			{Name: "Type", Type: "string"},
			{Name: "Data", Type: "integer"},
			{Name: "Age", Type: "string"},
		},
		Rows: []metav1.TableRow{{
			Cells:  []any{"registry-token", "kubernetes.io/dockerconfigjson", int64(1), "30m"},
			Object: runtime.RawExtension{Raw: []byte(`{"apiVersion":"meta.k8s.io/v1","kind":"PartialObjectMetadata","metadata":{"name":"registry-token","namespace":"team-a","creationTimestamp":"` + created.Format(time.RFC3339) + `"}}`)},
		}},
	}

	items, err := mapSecretTable(table)
	if err != nil {
		t.Fatalf("mapSecretTable() error = %v", err)
	}
	if len(items) != 1 || items[0].Name != "registry-token" || items[0].Namespace != "team-a" || items[0].Type != "kubernetes.io/dockerconfigjson" || items[0].DataEntries != 1 || items[0].Immutable != nil {
		t.Fatalf("mapSecretTable() = %#v", items)
	}
}

func TestCollectConfigReferenceTasks(t *testing.T) {
	t.Parallel()
	tasks := []func() ([]domainresource.ConfigReferenceView, error){
		func() ([]domainresource.ConfigReferenceView, error) {
			return []domainresource.ConfigReferenceView{{Kind: "Pod", Name: "api"}}, nil
		},
		func() ([]domainresource.ConfigReferenceView, error) {
			return []domainresource.ConfigReferenceView{{Kind: "Deployment", Name: "api"}}, nil
		},
	}
	items, err := collectConfigReferenceTasks(tasks)
	if err != nil {
		t.Fatalf("collectConfigReferenceTasks() error = %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("collectConfigReferenceTasks() = %#v, want 2 items", items)
	}
}

func TestAdvancedConfigurationMappings(t *testing.T) {
	t.Parallel()
	created := metav1.NewTime(time.Now().Add(-30 * time.Minute))
	quota := mapResourceQuota(corev1.ResourceQuota{ObjectMeta: metav1.ObjectMeta{Name: "compute", Namespace: "team-a", CreationTimestamp: created}, Spec: corev1.ResourceQuotaSpec{Scopes: []corev1.ResourceQuotaScope{corev1.ResourceQuotaScopeBestEffort}}, Status: corev1.ResourceQuotaStatus{Hard: corev1.ResourceList{corev1.ResourcePods: resource.MustParse("10")}, Used: corev1.ResourceList{corev1.ResourcePods: resource.MustParse("2")}}})
	if !reflect.DeepEqual(quota.Scopes, []string{"BestEffort"}) || quota.Hard["pods"] != "10" || quota.Used["pods"] != "2" {
		t.Fatalf("quota = %#v", quota)
	}
	holder, duration := "controller-a", int32(30)
	acquire := metav1.NewMicroTime(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	renew := metav1.NewMicroTime(time.Date(2026, 1, 2, 3, 5, 5, 0, time.UTC))
	lease := mapLease(coordinationv1.Lease{ObjectMeta: metav1.ObjectMeta{Name: "leader", Namespace: "kube-system", CreationTimestamp: created}, Spec: coordinationv1.LeaseSpec{HolderIdentity: &holder, LeaseDurationSeconds: &duration, AcquireTime: &acquire, RenewTime: &renew}})
	if lease.HolderIdentity != "controller-a" || lease.LeaseDurationSeconds != 30 || lease.AcquireTime != "2026-01-02T03:04:05Z" || lease.RenewTime != "2026-01-02T03:05:05Z" {
		t.Fatalf("lease = %#v", lease)
	}
}

func TestConfigurationDetailMappings(t *testing.T) {
	t.Parallel()
	path, port := "/admit", int32(8443)
	view := mapAdmissionWebhook("pods.example.io", admissionregistrationv1.WebhookClientConfig{
		Service:  &admissionregistrationv1.ServiceReference{Namespace: "system", Name: "admission", Path: &path, Port: &port},
		CABundle: []byte("private-ca"),
	}, nil, nil, nil, nil, nil, []string{"v1"}, nil, nil)
	if !view.CABundleConfigured || view.ServiceNamespace != "system" || view.ServiceName != "admission" || view.ServicePath != "/admit" || view.ServicePort != 8443 {
		t.Fatalf("webhook = %#v", view)
	}
	payload, err := json.Marshal(view)
	if err != nil || strings.Contains(string(payload), "private-ca") {
		t.Fatalf("webhook JSON leaked CA bytes: %s, %v", payload, err)
	}

	quota := mapResourceQuotaDetail(corev1.ResourceQuota{ObjectMeta: metav1.ObjectMeta{Name: "compute", Namespace: "team-a", Labels: map[string]string{"tier": "prod"}}, Status: corev1.ResourceQuotaStatus{Hard: corev1.ResourceList{corev1.ResourcePods: resource.MustParse("10")}}})
	if quota.Hard["pods"] != "10" || quota.Labels["tier"] != "prod" {
		t.Fatalf("quota detail = %#v", quota)
	}
	limit := mapLimitRangeDetail(corev1.LimitRange{Spec: corev1.LimitRangeSpec{Limits: []corev1.LimitRangeItem{{Type: corev1.LimitTypeContainer, DefaultRequest: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m")}, MaxLimitRequestRatio: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("4")}}}}})
	if len(limit.Rules) != 1 || limit.Rules[0].DefaultRequest["cpu"] != "100m" || limit.Rules[0].MaxLimitRequestRatio["cpu"] != "4" {
		t.Fatalf("limit detail = %#v", limit)
	}
}
