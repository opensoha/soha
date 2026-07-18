package resourcebackend

import (
	"errors"
	"testing"
	"time"

	appresource "github.com/opensoha/soha/internal/application/resource"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	agentinfra "github.com/opensoha/soha/internal/infrastructure/agent"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
	informerinfra "github.com/opensoha/soha/internal/infrastructure/informer"
	k8sinfra "github.com/opensoha/soha/internal/infrastructure/kubernetes"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	storagev1 "k8s.io/api/storage/v1"
	apiresource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestClustersExposeMetadataThroughApplicationPort(t *testing.T) {
	t.Parallel()
	backend := NewClusters(k8sinfra.NewManager([]cfgpkg.ClusterConfig{{
		ID:          "cluster-a",
		Name:        "Cluster A",
		Environment: "test",
	}}))

	summary, err := backend.Metadata("cluster-a")
	if err != nil {
		t.Fatalf("Metadata() error = %v", err)
	}
	if summary.ID != "cluster-a" || summary.Name != "Cluster A" {
		t.Fatalf("Metadata() = %#v", summary)
	}
}

func TestCacheUnavailableMapsInfrastructureSentinel(t *testing.T) {
	t.Parallel()
	cache := NewCache(nil)
	if !cache.CacheUnavailable(informerinfra.ErrCacheNotReady) {
		t.Fatal("CacheUnavailable() = false for informer sentinel")
	}
	if cache.CacheUnavailable(errors.New("list failed")) {
		t.Fatal("CacheUnavailable() = true for unrelated error")
	}
}

func TestMapClusterEventUsesBestAvailableTimestamp(t *testing.T) {
	t.Parallel()
	createdAt := time.Now().UTC().Add(-2 * time.Minute).Truncate(time.Second)
	eventTime := createdAt.Add(time.Minute)
	view := mapClusterEvent(corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name: "scheduled", Namespace: "platform", CreationTimestamp: metav1.NewTime(createdAt),
		},
		EventTime:      metav1.MicroTime{Time: eventTime},
		Type:           "Normal",
		Reason:         "Scheduled",
		InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "api-0"},
	})
	if view.Name != "scheduled" || view.Namespace != "platform" {
		t.Fatalf("mapClusterEvent() identity = %#v", view)
	}
	if view.LastTimestamp != eventTime.Format(time.RFC3339) {
		t.Fatalf("mapClusterEvent() lastTimestamp = %q, want %q", view.LastTimestamp, eventTime.Format(time.RFC3339))
	}
}

func TestMapHelmReleaseFallsBackToSecretName(t *testing.T) {
	t.Parallel()
	view := mapHelmRelease(
		"sh.helm.release.v1.gateway.v12",
		"platform",
		map[string]string{"status": "deployed", "helm.sh/chart": "gateway-1.2.3"},
		time.Now().UTC(),
	)
	if view.Name != "gateway" || view.Revision != "12" {
		t.Fatalf("mapHelmRelease() = %#v", view)
	}
	if view.StorageDriver != "secret" || view.Status != "deployed" {
		t.Fatalf("mapHelmRelease() metadata = %#v", view)
	}
}

func TestBuildPortForwardURLPreservesAPIServerAuthority(t *testing.T) {
	t.Parallel()
	got, err := buildPortForwardURL("https://api.example.test:6443", "platform", "api-0")
	if err != nil {
		t.Fatalf("buildPortForwardURL() error = %v", err)
	}
	if got.String() != "https://api.example.test:6443/api/v1/namespaces/platform/pods/api-0/portforward" {
		t.Fatalf("buildPortForwardURL() = %q", got.String())
	}
}

func TestResourceGVRForKindSupportsPlatformKinds(t *testing.T) {
	t.Parallel()
	cases := []struct {
		kind            string
		resource        string
		namespaceScoped bool
	}{
		{kind: "ServiceAccount", resource: "serviceaccounts", namespaceScoped: true},
		{kind: "Role", resource: "roles", namespaceScoped: true},
		{kind: "ClusterRole", resource: "clusterroles"},
		{kind: "ReplicaSet", resource: "replicasets", namespaceScoped: true},
		{kind: "PersistentVolumeClaim", resource: "persistentvolumeclaims", namespaceScoped: true},
		{kind: "StorageClass", resource: "storageclasses"},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			t.Parallel()
			gvr, namespaceScoped, err := resourceGVRForKind(tc.kind)
			if err != nil {
				t.Fatalf("resourceGVRForKind(%q) error = %v", tc.kind, err)
			}
			if gvr.Resource != tc.resource || namespaceScoped != tc.namespaceScoped {
				t.Fatalf("resourceGVRForKind(%q) = %s scoped=%v", tc.kind, gvr.Resource, namespaceScoped)
			}
		})
	}
}

func TestStorageMappingsPreserveCapacityAndPolicy(t *testing.T) {
	t.Parallel()
	created := metav1.NewTime(time.Now().Add(-2 * time.Hour))
	storageClassName := "fast"
	filesystem := corev1.PersistentVolumeFilesystem
	reclaimDelete := corev1.PersistentVolumeReclaimDelete
	waitForFirstConsumer := storagev1.VolumeBindingWaitForFirstConsumer
	allowExpansion := true

	pvc := mapPersistentVolumeClaim(corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "data", Namespace: "team-a", CreationTimestamp: created},
		Spec: corev1.PersistentVolumeClaimSpec{
			VolumeName: "pv-data", StorageClassName: &storageClassName,
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce, corev1.ReadOnlyMany},
			Resources: corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{
				corev1.ResourceStorage: apiresource.MustParse("5Gi"),
			}},
		},
		Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound},
	})
	if pvc.Name != "data" || pvc.StorageClass != "fast" || pvc.Requested != "5Gi" || len(pvc.AccessModes) != 2 {
		t.Fatalf("mapPersistentVolumeClaim() = %#v", pvc)
	}

	pvDetail := mapPersistentVolumeDetail(corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: "pv-data", CreationTimestamp: created, Labels: map[string]string{"tier": "gold"}},
		Spec: corev1.PersistentVolumeSpec{
			StorageClassName: "fast", PersistentVolumeReclaimPolicy: reclaimDelete,
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany}, VolumeMode: &filesystem,
			ClaimRef: &corev1.ObjectReference{Namespace: "team-a", Name: "data"},
			Capacity: corev1.ResourceList{corev1.ResourceStorage: apiresource.MustParse("10Gi")},
		},
		Status: corev1.PersistentVolumeStatus{Phase: corev1.VolumeBound},
	})
	if pvDetail.ClaimRef != "team-a/data" || pvDetail.ClaimNamespace != "team-a" || pvDetail.ClaimName != "data" || pvDetail.Capacity != "10Gi" || pvDetail.ReclaimPolicy != "Delete" || pvDetail.VolumeMode != "Filesystem" {
		t.Fatalf("mapPersistentVolumeDetail() = %#v", pvDetail)
	}

	pvcDetail := mapPersistentVolumeClaimDetail(corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "data", Namespace: "team-a", CreationTimestamp: created},
	}, []corev1.Pod{{
		ObjectMeta: metav1.ObjectMeta{Name: "api-0", Namespace: "team-a"},
		Spec:       corev1.PodSpec{Volumes: []corev1.Volume{{VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "data"}}}}},
	}})
	if len(pvcDetail.Pods) != 1 || pvcDetail.Pods[0].Name != "api-0" {
		t.Fatalf("mapPersistentVolumeClaimDetail() pods = %#v", pvcDetail.Pods)
	}

	storageClass := mapStorageClass(storagev1.StorageClass{
		ObjectMeta:  metav1.ObjectMeta{Name: "fast", CreationTimestamp: created},
		Provisioner: "kubernetes.io/no-provisioner", Parameters: map[string]string{"type": "ssd"},
		ReclaimPolicy: &reclaimDelete, VolumeBindingMode: &waitForFirstConsumer,
		AllowVolumeExpansion: &allowExpansion,
	})
	if storageClass.ReclaimPolicy != "Delete" || storageClass.VolumeBindingMode != "WaitForFirstConsumer" || !storageClass.AllowVolumeExpansion {
		t.Fatalf("mapStorageClass() = %#v", storageClass)
	}
}

func TestMapStorageClassTableOmitsParameters(t *testing.T) {
	t.Parallel()
	table := metav1.Table{
		ColumnDefinitions: []metav1.TableColumnDefinition{
			{Name: "Name"}, {Name: "Provisioner"}, {Name: "ReclaimPolicy"},
			{Name: "VolumeBindingMode"}, {Name: "AllowVolumeExpansion"},
		},
		Rows: []metav1.TableRow{{
			Cells:  []any{"fast", "csi.example.io", "Delete", "WaitForFirstConsumer", true},
			Object: tableTestMetadata("fast", ""),
		}},
	}
	items, err := mapStorageClassTable(table)
	if err != nil || len(items) != 1 || items[0].Name != "fast" || items[0].Provisioner != "csi.example.io" || !items[0].AllowVolumeExpansion {
		t.Fatalf("mapStorageClassTable() = %#v, %v", items, err)
	}
}

func TestNetworkMappingsPreserveRoutesAndPorts(t *testing.T) {
	t.Parallel()
	created := metav1.NewTime(time.Now().Add(-time.Hour))
	service := mapService(corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "team-a", CreationTimestamp: created},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP, ClusterIP: "10.0.0.1", Selector: map[string]string{"app": "api"},
			Ports: []corev1.ServicePort{{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP}},
		},
	})
	if service.Name != "api" || len(service.Ports) != 1 || service.Ports[0] != "http:80/tcp" {
		t.Fatalf("mapService() = %#v", service)
	}

	className := "nginx"
	ingress := mapIngress(networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Name: "edge", Namespace: "team-a", CreationTimestamp: created},
		Spec: networkingv1.IngressSpec{
			IngressClassName: &className,
			DefaultBackend:   &networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{Name: "default-svc"}},
			Rules: []networkingv1.IngressRule{{
				Host: "api.example.com",
				IngressRuleValue: networkingv1.IngressRuleValue{HTTP: &networkingv1.HTTPIngressRuleValue{
					Paths: []networkingv1.HTTPIngressPath{{Backend: networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{Name: "api"}}}},
				}},
			}},
		},
	})
	if ingress.ClassName != "nginx" || len(ingress.BackendServices) != 2 || ingress.BackendServices[0] != "api" {
		t.Fatalf("mapIngress() = %#v", ingress)
	}

	portName, portNumber, protocol := "http", int32(8080), corev1.ProtocolTCP
	endpointSlice := mapEndpointSlice(discoveryv1.EndpointSlice{
		ObjectMeta:  metav1.ObjectMeta{Name: "api-abc", Namespace: "team-a", CreationTimestamp: created},
		AddressType: discoveryv1.AddressTypeIPv4,
		Endpoints:   []discoveryv1.Endpoint{{Addresses: []string{"10.1.0.1"}}},
		Ports:       []discoveryv1.EndpointPort{{Name: &portName, Port: &portNumber, Protocol: &protocol}},
	})
	if endpointSlice.AddressType != "IPv4" || endpointSlice.Endpoints != 1 || endpointSlice.Ports[0] != "http:8080/tcp" {
		t.Fatalf("mapEndpointSlice() = %#v", endpointSlice)
	}
	ready := true
	detail := buildServiceDetail(corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "team-a", Labels: map[string]string{"app": "api"}},
	}, []discoveryv1.EndpointSlice{{Endpoints: []discoveryv1.Endpoint{{
		Addresses: []string{"10.1.0.1"}, Conditions: discoveryv1.EndpointConditions{Ready: &ready},
		TargetRef: &corev1.ObjectReference{Kind: "Pod", Name: "api-1"},
	}}}}, []domainresource.PodView{{Name: "api-1", Namespace: "team-a"}})
	if len(detail.Endpoints) != 1 || detail.Endpoints[0].Address != "10.1.0.1" || len(detail.BackendPods) != 1 || detail.Labels["app"] != "api" {
		t.Fatalf("buildServiceDetail() = %#v", detail)
	}
}

func TestNewAgentClientsProvidesCapabilityScopedFactories(t *testing.T) {
	t.Parallel()
	clients := NewAgentClients(agentinfra.NewRegistry(0))
	connection := domaincluster.Connection{
		Summary: domaincluster.Summary{ID: "agent-cluster"},
		Metadata: map[string]any{
			"endpoint": "http://127.0.0.1:18080",
			"token":    "test-token",
		},
	}

	tests := []struct {
		name    string
		resolve func() (any, error)
	}{
		{name: "workloads", resolve: func() (any, error) { return clients.Workloads(connection) }},
		{name: "configuration", resolve: func() (any, error) { return clients.Configuration(connection) }},
		{name: "network", resolve: func() (any, error) { return clients.Network(connection) }},
		{name: "storage", resolve: func() (any, error) { return clients.Storage(connection) }},
		{name: "rbac", resolve: func() (any, error) { return clients.RBAC(connection) }},
		{name: "helm", resolve: func() (any, error) { return clients.Helm(connection) }},
		{name: "inventory", resolve: func() (any, error) { return clients.Inventory(connection) }},
		{name: "custom resources", resolve: func() (any, error) { return clients.CustomResources(connection) }},
		{name: "generic", resolve: func() (any, error) { return clients.Generic(connection) }},
		{name: "events", resolve: func() (any, error) { return clients.Events(connection) }},
		{name: "port forwards", resolve: func() (any, error) { return clients.PortForwards(connection) }},
		{name: "resource creation", resolve: func() (any, error) { return clients.ResourceCreation(connection) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client, err := tt.resolve()
			if err != nil {
				t.Fatalf("resolve client: %v", err)
			}
			if client == nil {
				t.Fatal("resolved nil client")
			}
		})
	}
}

var (
	_ appresource.ClusterMetadataProvider  = (*Clusters)(nil)
	_ appresource.DirectEventReader        = (*Direct)(nil)
	_ appresource.DirectHelm               = (*Direct)(nil)
	_ appresource.DirectPortForwardStarter = (*Direct)(nil)
	_ appresource.DirectStorageReader      = (*Direct)(nil)
	_ appresource.DirectNetworkReader      = (*Direct)(nil)
	_ appresource.DirectGenericResource    = (*Direct)(nil)
)
