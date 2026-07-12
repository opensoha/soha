package resourcebackend

import (
	"reflect"
	"testing"
	"time"

	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	nodev1 "k8s.io/api/node/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestConfigurationMappings(t *testing.T) {
	t.Parallel()

	created := metav1.NewTime(time.Now().Add(-30 * time.Minute))
	immutable := true
	configMap := mapConfigMap(corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "app-config", Namespace: "team-a", CreationTimestamp: created}, Immutable: &immutable, Data: map[string]string{"app.yaml": "debug: false"}, BinaryData: map[string][]byte{"cert": []byte("bytes")}})
	if configMap.DataEntries != 1 || configMap.BinaryEntries != 1 || !configMap.Immutable {
		t.Fatalf("configMap = %#v", configMap)
	}
	secret := mapSecret(corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "app-secret", Namespace: "team-a", CreationTimestamp: created}, Immutable: &immutable, Type: corev1.SecretTypeTLS, Data: map[string][]byte{"tls.crt": []byte("cert"), "tls.key": []byte("key")}})
	if secret.Type != "kubernetes.io/tls" || secret.DataEntries != 2 || !secret.Immutable {
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
