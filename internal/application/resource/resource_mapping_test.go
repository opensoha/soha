package resource

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	batchv1 "k8s.io/api/batch/v1"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	nodev1 "k8s.io/api/node/v1"
	policyv1 "k8s.io/api/policy/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	domainaccess "github.com/soha/soha/internal/domain/access"
	k8sinfra "github.com/soha/soha/internal/infrastructure/kubernetes"
)

func TestListAcrossNamespaceNamesPreservesInputOrderAndLimitsConcurrency(t *testing.T) {
	t.Parallel()

	names := []string{"ns-a", "ns-b", "ns-c", "ns-d", "ns-e", "ns-f", "ns-g", "ns-h", "ns-i", "ns-j"}
	var active atomic.Int32
	var maxActive atomic.Int32
	var mu sync.Mutex
	visited := make(map[string]int)

	items, err := listAcrossNamespaceNames(context.Background(), &k8sinfra.Bundle{}, names, func(ctx context.Context, _ *k8sinfra.Bundle, namespace string) ([]string, error) {
		current := active.Add(1)
		for {
			previous := maxActive.Load()
			if current <= previous || maxActive.CompareAndSwap(previous, current) {
				break
			}
		}
		defer active.Add(-1)

		select {
		case <-time.After(time.Duration(len(names)-len(namespace)) * time.Millisecond):
		case <-ctx.Done():
			return nil, ctx.Err()
		}

		mu.Lock()
		visited[namespace]++
		mu.Unlock()
		return []string{namespace + "-1", namespace + "-2"}, nil
	})
	if err != nil {
		t.Fatalf("listAcrossNamespaceNames returned error: %v", err)
	}
	want := []string{
		"ns-a-1", "ns-a-2",
		"ns-b-1", "ns-b-2",
		"ns-c-1", "ns-c-2",
		"ns-d-1", "ns-d-2",
		"ns-e-1", "ns-e-2",
		"ns-f-1", "ns-f-2",
		"ns-g-1", "ns-g-2",
		"ns-h-1", "ns-h-2",
		"ns-i-1", "ns-i-2",
		"ns-j-1", "ns-j-2",
	}
	if !reflect.DeepEqual(items, want) {
		t.Fatalf("items = %#v, want %#v", items, want)
	}
	if got := maxActive.Load(); got > allNamespaceListParallelism {
		t.Fatalf("max concurrent list calls = %d, want <= %d", got, allNamespaceListParallelism)
	}
	for _, namespace := range names {
		if got := visited[namespace]; got != 1 {
			t.Fatalf("visited[%s] = %d, want 1", namespace, got)
		}
	}
}

func TestListAcrossNamespaceNamesReturnsFirstListError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("list failed")
	_, err := listAcrossNamespaceNames(context.Background(), &k8sinfra.Bundle{}, []string{"ns-a", "ns-b"}, func(_ context.Context, _ *k8sinfra.Bundle, namespace string) ([]string, error) {
		if namespace == "ns-b" {
			return nil, wantErr
		}
		return []string{namespace}, nil
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
}

func TestListAcrossNamespacesWithFallbackPrefersNamespaceAll(t *testing.T) {
	t.Parallel()

	namespaceNamesCalled := false
	items, err := listAcrossNamespacesWithFallback(context.Background(), &k8sinfra.Bundle{}, func(context.Context) ([]string, error) {
		namespaceNamesCalled = true
		return []string{"ns-a"}, nil
	}, func(_ context.Context, _ *k8sinfra.Bundle, namespace string) ([]string, error) {
		if namespace != metav1.NamespaceAll {
			t.Fatalf("namespace = %q, want NamespaceAll", namespace)
		}
		return []string{"all-a", "all-b"}, nil
	})
	if err != nil {
		t.Fatalf("listAcrossNamespacesWithFallback returned error: %v", err)
	}
	if namespaceNamesCalled {
		t.Fatal("namespace discovery was called after successful NamespaceAll list")
	}
	if !reflect.DeepEqual(items, []string{"all-a", "all-b"}) {
		t.Fatalf("items = %#v", items)
	}
}

func TestListAcrossNamespacesWithFallbackUsesNamespaceFanout(t *testing.T) {
	t.Parallel()

	allNamespacesErr := errors.New("namespace-all unsupported")
	namespaceNamesCalled := false
	items, err := listAcrossNamespacesWithFallback(context.Background(), &k8sinfra.Bundle{}, func(context.Context) ([]string, error) {
		namespaceNamesCalled = true
		return []string{"ns-a", "ns-b"}, nil
	}, func(_ context.Context, _ *k8sinfra.Bundle, namespace string) ([]string, error) {
		if namespace == metav1.NamespaceAll {
			return nil, allNamespacesErr
		}
		return []string{namespace}, nil
	})
	if err != nil {
		t.Fatalf("listAcrossNamespacesWithFallback returned error: %v", err)
	}
	if !namespaceNamesCalled {
		t.Fatal("namespace discovery was not called after NamespaceAll list failure")
	}
	if !reflect.DeepEqual(items, []string{"ns-a", "ns-b"}) {
		t.Fatalf("items = %#v", items)
	}
}

func TestListAcrossNamespacesWithFallbackReturnsNamespaceAllErrorWhenDiscoveryFails(t *testing.T) {
	t.Parallel()

	allNamespacesErr := errors.New("namespace-all failed")
	_, err := listAcrossNamespacesWithFallback(context.Background(), &k8sinfra.Bundle{}, func(context.Context) ([]string, error) {
		return nil, errors.New("namespace discovery failed")
	}, func(_ context.Context, _ *k8sinfra.Bundle, namespace string) ([]string, error) {
		if namespace == metav1.NamespaceAll {
			return nil, allNamespacesErr
		}
		return []string{namespace}, nil
	})
	if !errors.Is(err, allNamespacesErr) {
		t.Fatalf("err = %v, want %v", err, allNamespacesErr)
	}
}

func TestNetworkMappings(t *testing.T) {
	t.Parallel()

	actions := testDecision()
	created := metav1.NewTime(time.Now().Add(-time.Hour))
	portName := "http"
	portNumber := int32(8080)
	protocol := corev1.ProtocolTCP
	className := "nginx"

	service := mapService(corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "team-a", CreationTimestamp: created},
		Spec: corev1.ServiceSpec{
			Type:      corev1.ServiceTypeClusterIP,
			ClusterIP: "10.0.0.1",
			Selector:  map[string]string{"app": "api"},
			Ports: []corev1.ServicePort{
				{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP},
				{Port: 443, Protocol: corev1.ProtocolUDP},
			},
		},
	}, actions)
	if service.Name != "api" || service.Namespace != "team-a" || service.Type != "ClusterIP" || service.ClusterIP != "10.0.0.1" {
		t.Fatalf("service identity fields = %#v", service)
	}
	if !reflect.DeepEqual(service.Ports, []string{"http:80/tcp", "443/udp"}) {
		t.Fatalf("service.Ports = %#v", service.Ports)
	}
	if !reflect.DeepEqual(service.AllowedActions, []string{"list", "view", "update"}) {
		t.Fatalf("service.AllowedActions = %#v", service.AllowedActions)
	}

	ingress := mapIngress(networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Name: "edge", Namespace: "team-a", CreationTimestamp: created},
		Spec: networkingv1.IngressSpec{
			IngressClassName: &className,
			DefaultBackend:   &networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{Name: "default-svc"}},
			Rules: []networkingv1.IngressRule{
				{
					Host: "api.example.com",
					IngressRuleValue: networkingv1.IngressRuleValue{HTTP: &networkingv1.HTTPIngressRuleValue{Paths: []networkingv1.HTTPIngressPath{
						{Backend: networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{Name: "api"}}},
						{Backend: networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{Name: "default-svc"}}},
					}}},
				},
				{Host: "   "},
			},
		},
		Status: networkingv1.IngressStatus{LoadBalancer: networkingv1.IngressLoadBalancerStatus{Ingress: []networkingv1.IngressLoadBalancerIngress{
			{Hostname: "lb.example.com"},
			{IP: "192.0.2.10"},
		}}},
	}, actions)
	if ingress.ClassName != "nginx" {
		t.Fatalf("ingress.ClassName = %q, want nginx", ingress.ClassName)
	}
	if !reflect.DeepEqual(ingress.Hosts, []string{"api.example.com"}) {
		t.Fatalf("ingress.Hosts = %#v", ingress.Hosts)
	}
	if ingress.Address != "lb.example.com, 192.0.2.10" {
		t.Fatalf("ingress.Address = %q", ingress.Address)
	}
	if !reflect.DeepEqual(ingress.BackendServices, []string{"api", "default-svc"}) {
		t.Fatalf("ingress.BackendServices = %#v", ingress.BackendServices)
	}

	endpointSlice := mapEndpointSlice(discoveryv1.EndpointSlice{
		ObjectMeta:  metav1.ObjectMeta{Name: "api-abc", Namespace: "team-a", CreationTimestamp: created},
		AddressType: discoveryv1.AddressTypeIPv4,
		Endpoints:   []discoveryv1.Endpoint{{Addresses: []string{"10.1.0.1"}}, {Addresses: []string{"10.1.0.2"}}},
		Ports: []discoveryv1.EndpointPort{
			{Name: &portName, Port: &portNumber, Protocol: &protocol},
			{},
		},
	}, actions)
	if endpointSlice.AddressType != "IPv4" || endpointSlice.Endpoints != 2 {
		t.Fatalf("endpointSlice = %#v", endpointSlice)
	}
	if !reflect.DeepEqual(endpointSlice.Ports, []string{"http:8080/tcp"}) {
		t.Fatalf("endpointSlice.Ports = %#v", endpointSlice.Ports)
	}

	networkPolicy := mapNetworkPolicy(networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "deny-all", Namespace: "team-a", CreationTimestamp: created},
		Spec: networkingv1.NetworkPolicySpec{
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress},
			Ingress:     []networkingv1.NetworkPolicyIngressRule{{}},
			Egress:      []networkingv1.NetworkPolicyEgressRule{{}, {}},
		},
	}, actions)
	if !reflect.DeepEqual(networkPolicy.PolicyTypes, []string{"Ingress", "Egress"}) {
		t.Fatalf("networkPolicy.PolicyTypes = %#v", networkPolicy.PolicyTypes)
	}
	if networkPolicy.IngressRules != 1 || networkPolicy.EgressRules != 2 {
		t.Fatalf("networkPolicy rule counts = %#v", networkPolicy)
	}
}

func TestStorageMappings(t *testing.T) {
	t.Parallel()

	actions := testDecision()
	created := metav1.NewTime(time.Now().Add(-2 * time.Hour))
	storageClassName := "fast"
	filesystem := corev1.PersistentVolumeFilesystem
	reclaimDelete := corev1.PersistentVolumeReclaimDelete
	waitForFirstConsumer := storagev1.VolumeBindingWaitForFirstConsumer
	allowExpansion := true

	pvc := mapPersistentVolumeClaim(corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "data", Namespace: "team-a", CreationTimestamp: created},
		Spec: corev1.PersistentVolumeClaimSpec{
			VolumeName:       "pv-data",
			StorageClassName: &storageClassName,
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce, corev1.ReadOnlyMany},
			Resources: corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse("5Gi"),
			}},
			VolumeMode: &filesystem,
		},
		Status: corev1.PersistentVolumeClaimStatus{
			Phase: corev1.ClaimBound,
			Capacity: corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse("10Gi"),
			},
		},
	}, actions)
	if pvc.Name != "data" || pvc.Namespace != "team-a" || pvc.Status != "Bound" || pvc.VolumeName != "pv-data" || pvc.StorageClass != "fast" {
		t.Fatalf("pvc fields = %#v", pvc)
	}
	if !reflect.DeepEqual(pvc.AccessModes, []string{"ReadWriteOnce", "ReadOnlyMany"}) {
		t.Fatalf("pvc.AccessModes = %#v", pvc.AccessModes)
	}
	if pvc.Requested != "5Gi" {
		t.Fatalf("pvc.Requested = %q, want 5Gi", pvc.Requested)
	}

	pvDetail := mapPersistentVolumeDetail(corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: "pv-data", CreationTimestamp: created, Labels: map[string]string{"tier": "gold"}},
		Spec: corev1.PersistentVolumeSpec{
			StorageClassName:              "fast",
			PersistentVolumeReclaimPolicy: reclaimDelete,
			AccessModes:                   []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany},
			VolumeMode:                    &filesystem,
			ClaimRef:                      &corev1.ObjectReference{Namespace: "team-a", Name: "data"},
			Capacity: corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse("10Gi"),
			},
		},
		Status: corev1.PersistentVolumeStatus{Phase: corev1.VolumeBound},
	}, actions)
	if pvDetail.ClaimRef != "team-a/data" || pvDetail.Capacity != "10Gi" || pvDetail.ReclaimPolicy != "Delete" || pvDetail.VolumeMode != "Filesystem" {
		t.Fatalf("pvDetail = %#v", pvDetail)
	}
	if !reflect.DeepEqual(pvDetail.AccessModes, []string{"ReadWriteMany"}) {
		t.Fatalf("pvDetail.AccessModes = %#v", pvDetail.AccessModes)
	}
	if pvDetail.Labels["tier"] != "gold" {
		t.Fatalf("pvDetail.Labels = %#v", pvDetail.Labels)
	}

	storageClass := mapStorageClass(storagev1.StorageClass{
		ObjectMeta:           metav1.ObjectMeta{Name: "fast", CreationTimestamp: created},
		Provisioner:          "kubernetes.io/no-provisioner",
		Parameters:           map[string]string{"type": "ssd"},
		ReclaimPolicy:        &reclaimDelete,
		VolumeBindingMode:    &waitForFirstConsumer,
		AllowVolumeExpansion: &allowExpansion,
	}, actions)
	if storageClass.Name != "fast" || storageClass.Provisioner != "kubernetes.io/no-provisioner" {
		t.Fatalf("storageClass identity = %#v", storageClass)
	}
	if storageClass.ReclaimPolicy != "Delete" || storageClass.VolumeBindingMode != "WaitForFirstConsumer" || !storageClass.AllowVolumeExpansion {
		t.Fatalf("storageClass policy fields = %#v", storageClass)
	}
	if storageClass.Parameters["type"] != "ssd" {
		t.Fatalf("storageClass.Parameters = %#v", storageClass.Parameters)
	}
}

func TestConfigurationMappings(t *testing.T) {
	t.Parallel()

	actions := testDecision()
	created := metav1.NewTime(time.Now().Add(-30 * time.Minute))
	immutable := true

	configMap := mapConfigMap(corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "app-config", Namespace: "team-a", CreationTimestamp: created},
		Immutable:  &immutable,
		Data:       map[string]string{"app.yaml": "debug: false"},
		BinaryData: map[string][]byte{"cert": []byte("bytes")},
	}, actions)
	if configMap.DataEntries != 1 || configMap.BinaryEntries != 1 || !configMap.Immutable {
		t.Fatalf("configMap = %#v", configMap)
	}

	secret := mapSecret(corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "app-secret", Namespace: "team-a", CreationTimestamp: created},
		Immutable:  &immutable,
		Type:       corev1.SecretTypeTLS,
		Data:       map[string][]byte{"tls.crt": []byte("cert"), "tls.key": []byte("key")},
	}, actions)
	if secret.Type != "kubernetes.io/tls" || secret.DataEntries != 2 || !secret.Immutable {
		t.Fatalf("secret = %#v", secret)
	}

	ingressClass := mapIngressClass(networkingv1.IngressClass{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "nginx",
			CreationTimestamp: created,
			Annotations:       map[string]string{"ingressclass.kubernetes.io/is-default-class": "TRUE"},
		},
		Spec: networkingv1.IngressClassSpec{
			Controller: "k8s.io/ingress-nginx",
			Parameters: &networkingv1.IngressClassParametersReference{Kind: "IngressParameters", Name: "nginx-default"},
		},
	}, actions)
	if !ingressClass.IsDefault || ingressClass.Parameters != "IngressParameters/nginx-default" || ingressClass.Controller != "k8s.io/ingress-nginx" {
		t.Fatalf("ingressClass = %#v", ingressClass)
	}

	preemption := corev1.PreemptNever
	priorityClass := mapPriorityClass(schedulingv1.PriorityClass{
		ObjectMeta:       metav1.ObjectMeta{Name: "critical", CreationTimestamp: created},
		Value:            1000,
		GlobalDefault:    true,
		PreemptionPolicy: &preemption,
		Description:      "critical workloads",
	}, actions)
	if priorityClass.Value != 1000 || !priorityClass.GlobalDefault || priorityClass.PreemptionPolicy != "Never" {
		t.Fatalf("priorityClass = %#v", priorityClass)
	}

	runtimeClass := mapRuntimeClass(nodev1.RuntimeClass{
		ObjectMeta: metav1.ObjectMeta{Name: "kata", CreationTimestamp: created},
		Handler:    "kata-qemu",
	}, actions)
	if runtimeClass.Name != "kata" || runtimeClass.Handler != "kata-qemu" {
		t.Fatalf("runtimeClass = %#v", runtimeClass)
	}

	quota := mapResourceQuota(corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: "compute", Namespace: "team-a", CreationTimestamp: created},
		Spec:       corev1.ResourceQuotaSpec{Scopes: []corev1.ResourceQuotaScope{corev1.ResourceQuotaScopeBestEffort}},
		Status: corev1.ResourceQuotaStatus{
			Hard: corev1.ResourceList{corev1.ResourcePods: resource.MustParse("10")},
			Used: corev1.ResourceList{corev1.ResourcePods: resource.MustParse("2")},
		},
	}, actions)
	if !reflect.DeepEqual(quota.Scopes, []string{"BestEffort"}) || quota.Hard["pods"] != "10" || quota.Used["pods"] != "2" {
		t.Fatalf("quota = %#v", quota)
	}

	holder := "controller-a"
	duration := int32(30)
	acquire := metav1.NewMicroTime(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	renew := metav1.NewMicroTime(time.Date(2026, 1, 2, 3, 5, 5, 0, time.UTC))
	lease := mapLease(coordinationv1.Lease{
		ObjectMeta: metav1.ObjectMeta{Name: "leader", Namespace: "kube-system", CreationTimestamp: created},
		Spec: coordinationv1.LeaseSpec{
			HolderIdentity:       &holder,
			LeaseDurationSeconds: &duration,
			AcquireTime:          &acquire,
			RenewTime:            &renew,
		},
	}, actions)
	if lease.HolderIdentity != "controller-a" || lease.LeaseDurationSeconds != 30 {
		t.Fatalf("lease identity = %#v", lease)
	}
	if lease.AcquireTime != "2026-01-02T03:04:05Z" || lease.RenewTime != "2026-01-02T03:05:05Z" {
		t.Fatalf("lease times = %#v", lease)
	}
}

func TestWorkloadMappings(t *testing.T) {
	t.Parallel()

	actions := testDecision()
	created := metav1.NewTime(time.Now().Add(-90 * time.Minute))
	replicas := int32(3)
	parallelism := int32(2)
	completions := int32(4)
	completionMode := batchv1.IndexedCompletion
	suspend := true
	timeZone := "Asia/Shanghai"
	lastSchedule := metav1.NewTime(time.Date(2026, 6, 4, 1, 2, 3, 0, time.UTC))

	deploymentDetail := mapDeploymentDetail(appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "api",
			Namespace:         "team-a",
			CreationTimestamp: created,
			Labels:            map[string]string{"app": "api"},
			Annotations:       map[string]string{"deployment.kubernetes.io/revision": "7"},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "api"}},
			Strategy: appsv1.DeploymentStrategy{Type: appsv1.RollingUpdateDeploymentStrategyType},
			Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{
				{Name: "api", Image: "example/api:v1"},
			}}},
		},
		Status: appsv1.DeploymentStatus{
			ReadyReplicas:      2,
			UpdatedReplicas:    3,
			AvailableReplicas:  2,
			ObservedGeneration: 5,
			Conditions: []appsv1.DeploymentCondition{{
				Type:               appsv1.DeploymentProgressing,
				Status:             corev1.ConditionTrue,
				Reason:             "NewReplicaSetAvailable",
				Message:            "rollout complete",
				LastTransitionTime: created,
			}},
		},
	}, actions)
	if deploymentDetail.DesiredReplicas != 3 || deploymentDetail.ReadyReplicas != 2 || deploymentDetail.Strategy != "RollingUpdate" {
		t.Fatalf("deploymentDetail = %#v", deploymentDetail)
	}
	if got := deploymentDetail.Containers; len(got) != 1 || got[0].Name != "api" || got[0].Image != "example/api:v1" {
		t.Fatalf("deploymentDetail.Containers = %#v", got)
	}
	if got := deploymentDetail.Conditions; len(got) != 1 || got[0].Type != "Progressing" || got[0].Reason != "NewReplicaSetAvailable" {
		t.Fatalf("deploymentDetail.Conditions = %#v", got)
	}

	rollout := mapDeploymentRolloutStatus(appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "team-a", Annotations: map[string]string{"deployment.kubernetes.io/revision": "7"}},
		Spec:       appsv1.DeploymentSpec{Replicas: &replicas},
		Status: appsv1.DeploymentStatus{
			UpdatedReplicas:    3,
			ReadyReplicas:      3,
			AvailableReplicas:  3,
			ObservedGeneration: 5,
			Conditions:         []appsv1.DeploymentCondition{{Type: appsv1.DeploymentAvailable, Status: corev1.ConditionTrue}},
		},
	})
	if rollout.Status != "healthy" || rollout.Message != "deployment is fully available" || rollout.Revision != "7" {
		t.Fatalf("rollout = %#v", rollout)
	}

	statefulSet := mapStatefulSetDetail(appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "team-a", CreationTimestamp: created},
		Spec: appsv1.StatefulSetSpec{
			Replicas:       &replicas,
			ServiceName:    "db-headless",
			Selector:       &metav1.LabelSelector{MatchLabels: map[string]string{"app": "db"}},
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{Type: appsv1.RollingUpdateStatefulSetStrategyType},
		},
		Status: appsv1.StatefulSetStatus{ReadyReplicas: 2, CurrentReplicas: 3, CurrentRevision: "db-1", UpdateRevision: "db-2"},
	}, actions)
	if statefulSet.ServiceName != "db-headless" || statefulSet.UpdateStrategy != "RollingUpdate" || statefulSet.CurrentRevision != "db-1" || statefulSet.UpdateRevision != "db-2" {
		t.Fatalf("statefulSet = %#v", statefulSet)
	}

	daemonSet := mapDaemonSet(appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: "node-agent", Namespace: "team-a", CreationTimestamp: created},
		Status: appsv1.DaemonSetStatus{
			DesiredNumberScheduled: 5,
			CurrentNumberScheduled: 4,
			NumberReady:            3,
			NumberAvailable:        2,
			UpdatedNumberScheduled: 4,
		},
	}, actions)
	if daemonSet.DesiredNumber != 5 || daemonSet.CurrentNumber != 4 || daemonSet.ReadyNumber != 3 || daemonSet.AvailableNumber != 2 || daemonSet.UpdatedNumber != 4 {
		t.Fatalf("daemonSet = %#v", daemonSet)
	}

	jobDetail := mapJobDetail(batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "migrate", Namespace: "team-a", CreationTimestamp: created},
		Spec: batchv1.JobSpec{
			Completions:    &completions,
			Parallelism:    &parallelism,
			CompletionMode: &completionMode,
		},
		Status: batchv1.JobStatus{
			Succeeded:      2,
			Failed:         1,
			Active:         1,
			StartTime:      &created,
			CompletionTime: &lastSchedule,
		},
	}, actions)
	if jobDetail.Completions != 4 || jobDetail.Parallelism != 2 || jobDetail.CompletionMode != "Indexed" {
		t.Fatalf("jobDetail = %#v", jobDetail)
	}
	if jobDetail.StartTime == "" || jobDetail.CompletionTime != "2026-06-04T01:02:03Z" {
		t.Fatalf("jobDetail times = %#v", jobDetail)
	}

	cronJobDetail := mapCronJobDetail(batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{Name: "nightly", Namespace: "team-a", CreationTimestamp: created},
		Spec: batchv1.CronJobSpec{
			Schedule:          "0 1 * * *",
			Suspend:           &suspend,
			ConcurrencyPolicy: batchv1.ForbidConcurrent,
			TimeZone:          &timeZone,
		},
		Status: batchv1.CronJobStatus{
			Active:           []corev1.ObjectReference{{Name: "nightly-1"}, {Name: "nightly-2"}},
			LastScheduleTime: &lastSchedule,
		},
	}, actions)
	if cronJobDetail.Schedule != "0 1 * * *" || !cronJobDetail.Suspend || cronJobDetail.ActiveJobs != 2 {
		t.Fatalf("cronJobDetail = %#v", cronJobDetail)
	}
	if cronJobDetail.LastScheduleTime != "2026-06-04T01:02:03Z" || cronJobDetail.ConcurrencyPolicy != "Forbid" || cronJobDetail.TimeZone != "Asia/Shanghai" {
		t.Fatalf("cronJobDetail schedule fields = %#v", cronJobDetail)
	}

	replicaSet := mapReplicaSet(appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{Name: "api-abc", Namespace: "team-a", CreationTimestamp: created},
		Spec:       appsv1.ReplicaSetSpec{Replicas: &replicas},
		Status:     appsv1.ReplicaSetStatus{ReadyReplicas: 2, AvailableReplicas: 1},
	}, actions)
	if replicaSet.DesiredReplicas != 3 || replicaSet.ReadyReplicas != 2 || replicaSet.AvailableReplicas != 1 {
		t.Fatalf("replicaSet = %#v", replicaSet)
	}

	minReplicas := int32(2)
	hpa := mapHorizontalPodAutoscaler(autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "team-a", CreationTimestamp: created},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Kind: "Deployment", Name: "api"},
			MinReplicas:    &minReplicas,
			MaxReplicas:    10,
		},
		Status: autoscalingv2.HorizontalPodAutoscalerStatus{CurrentReplicas: 3, DesiredReplicas: 5},
	}, actions)
	if hpa.TargetRef != "Deployment/api" || hpa.MinReplicas != 2 || hpa.MaxReplicas != 10 || hpa.CurrentReplicas != 3 || hpa.DesiredReplicas != 5 {
		t.Fatalf("hpa = %#v", hpa)
	}

	pdb := mapPodDisruptionBudget(policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "team-a", CreationTimestamp: created},
		Spec: policyv1.PodDisruptionBudgetSpec{
			MinAvailable:   intstrPtr(intstr.FromInt32(2)),
			MaxUnavailable: intstrPtr(intstr.FromString("25%")),
		},
		Status: policyv1.PodDisruptionBudgetStatus{
			CurrentHealthy:     3,
			DesiredHealthy:     2,
			DisruptionsAllowed: 1,
		},
	}, actions)
	if pdb.MinAvailable != "2" || pdb.MaxUnavailable != "25%" || pdb.CurrentHealthy != 3 || pdb.DesiredHealthy != 2 || pdb.DisruptionsAllowed != 1 {
		t.Fatalf("pdb = %#v", pdb)
	}
}

func TestFilterScopedNamespaceItems(t *testing.T) {
	t.Parallel()

	items := []struct {
		name      string
		namespace string
	}{
		{name: "a", namespace: "team-a"},
		{name: "b", namespace: "team-b"},
		{name: "c", namespace: "team-c"},
	}
	filtered := filterScopedNamespaceItems(items, domainaccess.Decision{
		ResourceScope: &domainaccess.ResourceScope{Namespaces: []string{"team-b", "team-c"}},
	}, func(item struct {
		name      string
		namespace string
	}) string {
		return item.namespace
	})
	if !reflect.DeepEqual(filtered, items[1:]) {
		t.Fatalf("filtered = %#v, want %#v", filtered, items[1:])
	}

	unscoped := filterScopedNamespaceItems(items, domainaccess.Decision{}, func(item struct {
		name      string
		namespace string
	}) string {
		return item.namespace
	})
	if !reflect.DeepEqual(unscoped, items) {
		t.Fatalf("unscoped = %#v, want original items", unscoped)
	}
}

func testDecision() domainaccess.Decision {
	return domainaccess.Decision{
		Allowed: true,
		AllowedActions: []domainaccess.Action{
			domainaccess.ActionList,
			domainaccess.ActionView,
			domainaccess.ActionUpdate,
		},
	}
}

func intstrPtr(value intstr.IntOrString) *intstr.IntOrString {
	return &value
}
