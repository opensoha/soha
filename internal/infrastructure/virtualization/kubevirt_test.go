package virtualization

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	kubeinfra "github.com/opensoha/soha/internal/infrastructure/kubernetes"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"
	typedfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	ktesting "k8s.io/client-go/testing"
)

type stubBundleProvider struct {
	bundle *kubeinfra.Bundle
	err    error
}

func (s stubBundleProvider) Bundle(context.Context, string) (*kubeinfra.Bundle, error) {
	return s.bundle, s.err
}

func kubeVirtTestListKinds() map[schema.GroupVersionResource]string {
	return map[schema.GroupVersionResource]string{
		kubeVirtVMGVR:                  "VirtualMachineList",
		kubeVirtVMIGVR:                 "VirtualMachineInstanceList",
		kubeVirtInstancetypeGVR:        "VirtualMachineInstancetypeList",
		kubeVirtClusterInstancetypeGVR: "VirtualMachineClusterInstancetypeList",
		kubeVirtDataSourceGVR:          "DataSourceList",
		kubeVirtDataVolumeGVR:          "DataVolumeList",
		kubeVirtNADGVR:                 "NetworkAttachmentDefinitionList",
		pvcGVR:                         "PersistentVolumeClaimList",
	}
}

func TestBuildKubeVirtVMUsesDataSourceClone(t *testing.T) {
	vm := BuildKubeVirtVM(CreateVMInput{
		Name:       "demo",
		Namespace:  "apps",
		CPU:        2,
		Memory:     "4Gi",
		DiskSize:   "20Gi",
		SourceMode: "datasource_clone",
		SourceRef:  "ubuntu-ds",
		ProviderParams: map[string]any{
			"storageClass":   "fast-ssd",
			"dataVolumeName": "demo-rootdisk",
		},
	})
	dataVolumes, _, err := unstructured.NestedSlice(vm.Object, "spec", "dataVolumeTemplates")
	if err != nil {
		t.Fatalf("dataVolumeTemplates error = %v", err)
	}
	if len(dataVolumes) != 1 {
		t.Fatalf("dataVolumeTemplates len = %d, want 1", len(dataVolumes))
	}
	dataVolume, ok := dataVolumes[0].(map[string]any)
	if !ok {
		t.Fatalf("dataVolumeTemplates[0] = %#v, want map", dataVolumes[0])
	}
	accessModes, _, err := unstructured.NestedStringSlice(dataVolume, "spec", "storage", "accessModes")
	if err != nil {
		t.Fatalf("accessModes error = %v", err)
	}
	if len(accessModes) != 1 || accessModes[0] != "ReadWriteOnce" {
		t.Fatalf("accessModes = %#v, want ReadWriteOnce", accessModes)
	}
	volumes, _, err := unstructured.NestedSlice(vm.Object, "spec", "template", "spec", "volumes")
	if err != nil {
		t.Fatalf("volumes error = %v", err)
	}
	if len(volumes) == 0 {
		t.Fatalf("volumes len = 0")
	}
	root, ok := volumes[0].(map[string]any)
	if !ok {
		t.Fatalf("root volume = %#v, want map", volumes[0])
	}
	if _, ok := root["dataVolume"]; !ok {
		t.Fatalf("root volume = %#v, want dataVolume", root)
	}
}

func TestBuildKubeVirtVMUsesDataSourceCloneStorageOptions(t *testing.T) {
	vm := BuildKubeVirtVM(CreateVMInput{
		Name:       "demo",
		Namespace:  "apps",
		DiskSize:   "20Gi",
		SourceMode: "datasource_clone",
		SourceRef:  "ubuntu-ds",
		ProviderParams: map[string]any{
			"accessModes": []any{"ReadWriteMany", "ReadOnlyMany"},
			"volumeMode":  "Block",
		},
	})
	dataVolumes, _, err := unstructured.NestedSlice(vm.Object, "spec", "dataVolumeTemplates")
	if err != nil {
		t.Fatalf("dataVolumeTemplates error = %v", err)
	}
	if len(dataVolumes) != 1 {
		t.Fatalf("dataVolumeTemplates len = %d, want 1", len(dataVolumes))
	}
	dataVolume, ok := dataVolumes[0].(map[string]any)
	if !ok {
		t.Fatalf("dataVolumeTemplates[0] = %#v, want map", dataVolumes[0])
	}
	accessModes, _, err := unstructured.NestedStringSlice(dataVolume, "spec", "storage", "accessModes")
	if err != nil {
		t.Fatalf("accessModes error = %v", err)
	}
	if strings.Join(accessModes, ",") != "ReadWriteMany,ReadOnlyMany" {
		t.Fatalf("accessModes = %#v", accessModes)
	}
	volumeMode, _, err := unstructured.NestedString(dataVolume, "spec", "storage", "volumeMode")
	if err != nil {
		t.Fatalf("volumeMode error = %v", err)
	}
	if volumeMode != "Block" {
		t.Fatalf("volumeMode = %q, want Block", volumeMode)
	}
}

func TestKubeVirtVMIQueriesMatchExportedNamespace(t *testing.T) {
	memory := kubeVirtVMIInstantQuery("kubevirt_vmi_memory_resident_bytes", "testvm-cirros", "virt-lab")
	if !strings.Contains(memory, `namespace="virt-lab"`) || !strings.Contains(memory, `exported_namespace="virt-lab"`) {
		t.Fatalf("memory query = %s", memory)
	}
	cpu := kubeVirtVMISumRateQuery("kubevirt_vmi_cpu_usage_seconds_total", "testvm-cirros", "virt-lab", "5m")
	if !strings.Contains(cpu, `sum(rate(kubevirt_vmi_cpu_usage_seconds_total{name="testvm-cirros",namespace="virt-lab"}[5m]))`) ||
		!strings.Contains(cpu, `sum(rate(kubevirt_vmi_cpu_usage_seconds_total{name="testvm-cirros",exported_namespace="virt-lab"}[5m]))`) {
		t.Fatalf("cpu query = %s", cpu)
	}
}

func TestBuildKubeVirtVMUsesPVCClone(t *testing.T) {
	vm := BuildKubeVirtVM(CreateVMInput{
		Name:       "demo",
		Namespace:  "apps",
		CPU:        2,
		Memory:     "4Gi",
		SourceMode: "pvc_clone",
		SourceRef:  "root-pvc",
	})
	volumes, _, err := unstructured.NestedSlice(vm.Object, "spec", "template", "spec", "volumes")
	if err != nil {
		t.Fatalf("volumes error = %v", err)
	}
	if len(volumes) == 0 {
		t.Fatalf("volumes len = 0")
	}
	root, ok := volumes[0].(map[string]any)
	if !ok {
		t.Fatalf("root volume = %#v, want map", volumes[0])
	}
	if _, ok := root["persistentVolumeClaim"]; !ok {
		t.Fatalf("root volume = %#v, want persistentVolumeClaim", root)
	}
}

func TestBuildKubeVirtVM(t *testing.T) {
	vm := BuildKubeVirtVM(CreateVMInput{
		Name:             "demo",
		Namespace:        "apps",
		Node:             "node-a",
		CPU:              2,
		Memory:           "4Gi",
		Architecture:     "arm64",
		BootImage:        "registry.local/vm:latest",
		DiskSize:         "20Gi",
		Network:          "pod",
		CloudInit:        "#cloud-config",
		StartAfterCreate: true,
	})
	if vm.GetAPIVersion() != "kubevirt.io/v1" || vm.GetKind() != "VirtualMachine" {
		t.Fatalf("unexpected api kind: %s %s", vm.GetAPIVersion(), vm.GetKind())
	}
	runStrategy, _, _ := unstructured.NestedString(vm.Object, "spec", "runStrategy")
	if runStrategy != "Always" {
		t.Fatalf("runStrategy = %q, want Always", runStrategy)
	}
	node, _, _ := unstructured.NestedStringMap(vm.Object, "spec", "template", "spec", "nodeSelector")
	if node["kubernetes.io/hostname"] != "node-a" {
		t.Fatalf("nodeSelector = %#v", node)
	}
	architecture, _, _ := unstructured.NestedString(vm.Object, "spec", "template", "spec", "architecture")
	if architecture != "arm64" {
		t.Fatalf("architecture = %q, want arm64", architecture)
	}
	volumes, _, _ := unstructured.NestedSlice(vm.Object, "spec", "template", "spec", "volumes")
	if len(volumes) != 2 {
		t.Fatalf("volumes len = %d, want 2", len(volumes))
	}
}

func TestBuildKubeVirtVMUsesMultusNetwork(t *testing.T) {
	vm := BuildKubeVirtVM(CreateVMInput{
		Name:      "demo",
		Namespace: "apps",
		CPU:       2,
		Memory:    "4Gi",
		Network:   "apps/docker-build-net",
		ProviderParams: map[string]any{
			"networkType":    "multus",
			"interfaceModel": "virtio",
		},
	})

	networks, _, _ := unstructured.NestedSlice(vm.Object, "spec", "template", "spec", "networks")
	if len(networks) != 1 {
		t.Fatalf("networks len = %d, want 1", len(networks))
	}
	multus, ok := networks[0].(map[string]any)["multus"].(map[string]any)
	if !ok || multus["networkName"] != "apps/docker-build-net" {
		t.Fatalf("multus network = %#v", networks[0])
	}
	interfaces, _, _ := unstructured.NestedSlice(vm.Object, "spec", "template", "spec", "domain", "devices", "interfaces")
	if len(interfaces) != 1 {
		t.Fatalf("interfaces len = %d, want 1", len(interfaces))
	}
	iface, ok := interfaces[0].(map[string]any)
	if !ok {
		t.Fatalf("interfaces[0] = %#v, want map", interfaces[0])
	}
	if iface["model"] != "virtio" {
		t.Fatalf("interface model = %#v, want virtio", iface["model"])
	}
	if _, ok := iface["bridge"]; !ok {
		t.Fatalf("interface = %#v, want bridge binding", iface)
	}
}

func TestBuildKubeVirtVMUsesSourceRefForContainerDisk(t *testing.T) {
	vm := BuildKubeVirtVM(CreateVMInput{
		Name:       "demo",
		Namespace:  "apps",
		CPU:        1,
		Memory:     "512Mi",
		BootImage:  "image-record-id",
		SourceMode: "containerdisk",
		SourceRef:  "quay.io/containerdisks/cirros:latest",
	})
	volumes, _, _ := unstructured.NestedSlice(vm.Object, "spec", "template", "spec", "volumes")
	if len(volumes) == 0 {
		t.Fatalf("volumes len = 0")
	}
	root, ok := volumes[0].(map[string]any)
	if !ok {
		t.Fatalf("root volume = %#v, want map", volumes[0])
	}
	containerDisk, ok := root["containerDisk"].(map[string]any)
	if !ok {
		t.Fatalf("containerDisk = %#v, want map", root["containerDisk"])
	}
	if containerDisk["image"] != "quay.io/containerdisks/cirros:latest" {
		t.Fatalf("containerDisk image = %#v, want sourceRef", containerDisk["image"])
	}
}

func TestKubeVirtAdapterRejectsAgentMode(t *testing.T) {
	adapter := NewKubeVirtAdapter(stubBundleProvider{})
	_, err := adapter.CreateVM(context.Background(), Connection{Mode: "agent", ClusterID: "c1"}, CreateVMInput{Name: "demo"})
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("CreateVM() error = %v, want ErrUnsupported", err)
	}
}

func TestKubeVirtAdapterTestConnectionReportsAgentModeUnsupported(t *testing.T) {
	adapter := NewKubeVirtAdapter(stubBundleProvider{})
	result, err := adapter.TestConnection(context.Background(), Connection{Mode: "agent", ClusterID: "c1"})
	if err != nil {
		t.Fatalf("TestConnection() error = %v, want nil", err)
	}
	if result.Status != "unsupported" || result.Reason != "agent_mode_unsupported" || result.NextAction == "" {
		t.Fatalf("result = %#v, want structured agent unsupported", result)
	}
}

func TestKubeVirtAdapterSyncAssetsDegradesWhenCRDMissing(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewSimpleDynamicClientWithCustomListKinds(scheme, kubeVirtTestListKinds(), &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "PersistentVolumeClaim",
			"metadata": map[string]any{
				"name":      "disk-a",
				"namespace": "apps",
			},
			"status": map[string]any{"phase": "Bound"},
		},
	})
	client.PrependReactor("list", "virtualmachines", func(action ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewNotFound(kubeVirtVMGVR.GroupResource(), "")
	})
	client.PrependReactor("list", "virtualmachineinstances", func(action ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewNotFound(kubeVirtVMIGVR.GroupResource(), "")
	})
	client.PrependReactor("list", "datasources", func(action ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewNotFound(kubeVirtDataSourceGVR.GroupResource(), "")
	})
	adapter := NewKubeVirtAdapter(stubBundleProvider{bundle: &kubeinfra.Bundle{Dynamic: client}})
	result, err := adapter.SyncAssets(context.Background(), Connection{ClusterID: "c1", Options: map[string]any{"namespace": "apps"}})
	if err != nil {
		t.Fatalf("SyncAssets() error = %v", err)
	}
	if result.Health.Status != "degraded" {
		t.Fatalf("health status = %q, want degraded", result.Health.Status)
	}
	if len(result.Assets) != 1 || result.Assets[0].Name != "disk-a" {
		t.Fatalf("assets = %#v", result.Assets)
	}
}

func TestKubeVirtAdapterSyncAssetsKeepsHealthyWhenOptionalCRDMissing(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewSimpleDynamicClientWithCustomListKinds(scheme, kubeVirtTestListKinds(), &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "kubevirt.io/v1",
			"kind":       "VirtualMachine",
			"metadata": map[string]any{
				"name":      "demo",
				"namespace": "apps",
				"uid":       "vm-uid",
			},
			"status": map[string]any{"printableStatus": "Running"},
		},
	}, &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "kubevirt.io/v1",
			"kind":       "VirtualMachineInstance",
			"metadata": map[string]any{
				"name":      "demo",
				"namespace": "apps",
				"uid":       "vmi-uid",
			},
			"status": map[string]any{"phase": "Running"},
		},
	})
	client.PrependReactor("list", "network-attachment-definitions", func(action ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewNotFound(kubeVirtNADGVR.GroupResource(), "")
	})
	adapter := NewKubeVirtAdapter(stubBundleProvider{bundle: &kubeinfra.Bundle{Dynamic: client}})

	result, err := adapter.SyncAssets(context.Background(), Connection{ClusterID: "c1", Options: map[string]any{"namespace": "apps"}})
	if err != nil {
		t.Fatalf("SyncAssets() error = %v", err)
	}
	if result.Health.Status != "healthy" {
		t.Fatalf("health status = %q, want healthy", result.Health.Status)
	}
	if !strings.Contains(result.Health.Message, "networkattachmentdefinition resource is not available") {
		t.Fatalf("health message = %q, want optional missing resource warning", result.Health.Message)
	}
}

func TestKubeVirtAdapterSyncAssetsUsesOwnerUIDForVMInstance(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewSimpleDynamicClientWithCustomListKinds(scheme, kubeVirtTestListKinds(), &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "kubevirt.io/v1",
			"kind":       "VirtualMachine",
			"metadata": map[string]any{
				"name":      "demo",
				"namespace": "apps",
				"uid":       "vm-uid",
			},
			"status": map[string]any{"printableStatus": "Starting"},
		},
	}, &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "kubevirt.io/v1",
			"kind":       "VirtualMachineInstance",
			"metadata": map[string]any{
				"name":      "demo",
				"namespace": "apps",
				"uid":       "vmi-uid",
				"ownerReferences": []any{
					map[string]any{
						"apiVersion": "kubevirt.io/v1",
						"kind":       "VirtualMachine",
						"name":       "demo",
						"uid":        "vm-uid",
						"controller": true,
					},
				},
			},
			"status": map[string]any{"phase": "Running"},
		},
	})
	adapter := NewKubeVirtAdapter(stubBundleProvider{bundle: &kubeinfra.Bundle{Dynamic: client}})

	result, err := adapter.SyncAssets(context.Background(), Connection{ClusterID: "c1", Options: map[string]any{"namespace": "apps"}})
	if err != nil {
		t.Fatalf("SyncAssets() error = %v", err)
	}
	var vmUIDs []string
	for _, asset := range result.Assets {
		if asset.Type == "virtualmachine" || asset.Type == "virtualmachineinstance" {
			vmUIDs = append(vmUIDs, asset.Metadata["uid"])
		}
	}
	if len(vmUIDs) != 2 || vmUIDs[0] != "vm-uid" || vmUIDs[1] != "vm-uid" {
		t.Fatalf("vm uids = %#v, want both assets keyed by VM uid", vmUIDs)
	}
}

func TestKubeVirtAdapterSyncAssetsIncludesClusterInstancetypeFlavors(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewSimpleDynamicClientWithCustomListKinds(scheme, kubeVirtTestListKinds(), &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "instancetype.kubevirt.io/v1beta1",
			"kind":       "VirtualMachineClusterInstancetype",
			"metadata": map[string]any{
				"name": "cx1.medium",
				"uid":  "flavor-uid",
			},
			"spec": map[string]any{
				"cpu":    map[string]any{"guest": int64(2)},
				"memory": map[string]any{"guest": "4Gi"},
			},
		},
	})
	adapter := NewKubeVirtAdapter(stubBundleProvider{bundle: &kubeinfra.Bundle{Dynamic: client}})

	result, err := adapter.SyncAssets(context.Background(), Connection{ClusterID: "c1"})
	if err != nil {
		t.Fatalf("SyncAssets() error = %v", err)
	}
	for _, asset := range result.Assets {
		if asset.Type == "flavor" && asset.Name == "cx1.medium" {
			if asset.Metadata["uid"] != "flavor-uid" || asset.Metadata["cpu"] != "2" || asset.Metadata["memory"] != "4Gi" || asset.Metadata["scope"] != "cluster" {
				t.Fatalf("flavor metadata = %#v", asset.Metadata)
			}
			return
		}
	}
	t.Fatalf("cluster instancetype flavor missing from assets: %#v", result.Assets)
}

func TestKubeVirtAdapterTestConnection(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewSimpleDynamicClientWithCustomListKinds(scheme, kubeVirtTestListKinds(), &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "kubevirt.io/v1",
			"kind":       "VirtualMachine",
			"metadata": map[string]any{
				"name":      "demo",
				"namespace": "apps",
			},
		},
	})
	adapter := NewKubeVirtAdapter(stubBundleProvider{bundle: &kubeinfra.Bundle{Dynamic: client}})

	result, err := adapter.TestConnection(context.Background(), Connection{
		ClusterID: "c1",
		Options:   map[string]any{"namespace": "apps"},
	})
	if err != nil {
		t.Fatalf("TestConnection() error = %v", err)
	}
	if !result.Healthy || result.Status != "healthy" {
		t.Fatalf("result = %#v, want healthy", result)
	}
}

func TestKubeVirtAdapterTestConnectionDegradesWhenNamespaceMissing(t *testing.T) {
	scheme := runtime.NewScheme()
	dynamicClient := fake.NewSimpleDynamicClientWithCustomListKinds(scheme, kubeVirtTestListKinds())
	typedClient := typedfake.NewSimpleClientset(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}})
	adapter := NewKubeVirtAdapter(stubBundleProvider{bundle: &kubeinfra.Bundle{Typed: typedClient, Dynamic: dynamicClient}})

	result, err := adapter.TestConnection(context.Background(), Connection{
		ClusterID: "c1",
		Options:   map[string]any{"namespace": "missing"},
	})
	if err != nil {
		t.Fatalf("TestConnection() error = %v", err)
	}
	if result.Healthy || result.Status != "degraded" || result.Reason != "namespace_not_found" {
		t.Fatalf("result = %#v, want namespace_not_found degraded", result)
	}
}

func TestKubeVirtAdapterTestConnectionDegradesWhenCRDMissing(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewSimpleDynamicClientWithCustomListKinds(scheme, kubeVirtTestListKinds())
	client.PrependReactor("list", "virtualmachines", func(action ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewNotFound(kubeVirtVMGVR.GroupResource(), "")
	})
	adapter := NewKubeVirtAdapter(stubBundleProvider{bundle: &kubeinfra.Bundle{Dynamic: client}})

	result, err := adapter.TestConnection(context.Background(), Connection{ClusterID: "c1"})
	if err != nil {
		t.Fatalf("TestConnection() error = %v", err)
	}
	if result.Healthy || result.Status != "degraded" {
		t.Fatalf("result = %#v, want degraded", result)
	}
}

func TestKubeVirtAdapterGetConsoleURLReturnsBackendURL(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewSimpleDynamicClientWithCustomListKinds(scheme, kubeVirtTestListKinds(), &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "kubevirt.io/v1",
			"kind":       "VirtualMachineInstance",
			"metadata": map[string]any{
				"name":      "demo",
				"namespace": "apps",
			},
		},
	})
	adapter := NewKubeVirtAdapter(stubBundleProvider{bundle: &kubeinfra.Bundle{
		Dynamic: client,
		RESTConfig: &rest.Config{
			Host:        "https://k8s.example",
			BearerToken: "token-1",
			TLSClientConfig: rest.TLSClientConfig{
				Insecure: true,
			},
		},
	}})
	result, err := adapter.GetConsoleURL(context.Background(), Connection{ClusterID: "c1"}, VM{ID: "vm-1", Name: "demo", Namespace: "apps"})
	if err != nil {
		t.Fatalf("GetConsoleURL() error = %v", err)
	}
	if !result.Ready || result.URL != "/api/v1/virtualization/vms/vm-1/console/vnc" {
		t.Fatalf("result = %#v", result)
	}
	if result.BackendURL != "https://k8s.example/apis/subresources.kubevirt.io/v1/namespaces/apps/virtualmachineinstances/demo/vnc" {
		t.Fatalf("backendURL = %q", result.BackendURL)
	}
	headers := ConsoleBackendHeaders(result)
	if headers.Get("Authorization") != "Bearer token-1" {
		t.Fatalf("Authorization header = %q", headers.Get("Authorization"))
	}
	tlsConfig, err := ConsoleBackendTLSConfig(result)
	if err != nil {
		t.Fatalf("ConsoleBackendTLSConfig() error = %v", err)
	}
	if tlsConfig == nil || !tlsConfig.InsecureSkipVerify {
		t.Fatalf("BackendTLS = %#v, want kube REST TLS config", result.BackendTLS)
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal console result: %v", err)
	}
	if strings.Contains(string(raw), "token-1") || strings.Contains(string(raw), "BackendHeaders") || strings.Contains(string(raw), "BackendTLS") {
		t.Fatalf("console result leaked backend internals: %s", raw)
	}
}

func TestKubeVirtAdapterCreateAndPowerAction(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewSimpleDynamicClientWithCustomListKinds(scheme, kubeVirtTestListKinds())
	adapter := NewKubeVirtAdapter(stubBundleProvider{bundle: &kubeinfra.Bundle{Dynamic: client}})
	vm, err := adapter.CreateVM(context.Background(), Connection{ClusterID: "c1"}, CreateVMInput{Name: "demo", Namespace: "apps"})
	if err != nil {
		t.Fatalf("CreateVM() error = %v", err)
	}
	if vm.Name != "demo" || vm.Namespace != "apps" {
		t.Fatalf("vm = %#v", vm)
	}
	if _, err := adapter.PowerAction(context.Background(), Connection{ClusterID: "c1"}, vm, PowerActionStart); err != nil {
		t.Fatalf("PowerAction(start) error = %v", err)
	}
	obj, err := client.Resource(kubeVirtVMGVR).Namespace("apps").Get(context.Background(), "demo", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get created vm error = %v", err)
	}
	runStrategy, _, _ := unstructured.NestedString(obj.Object, "spec", "runStrategy")
	if runStrategy != "Always" {
		t.Fatalf("runStrategy = %q, want Always", runStrategy)
	}
}

func TestKubeVirtAdapterCreateVMReturnsProvisioningMetadata(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewSimpleDynamicClientWithCustomListKinds(scheme, kubeVirtTestListKinds(), &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "cdi.kubevirt.io/v1beta1",
			"kind":       "DataVolume",
			"metadata": map[string]any{
				"name":      "demo-rootdisk",
				"namespace": "apps",
				"uid":       "dv-uid",
			},
			"spec": map[string]any{
				"sourceRef": map[string]any{"kind": "DataSource", "name": "ubuntu", "namespace": "apps"},
				"storage":   map[string]any{"storageClassName": "fast-ssd"},
			},
			"status": map[string]any{
				"phase":    "ImportInProgress",
				"progress": "45.0%",
				"conditions": []any{
					map[string]any{"type": "Ready", "status": "False", "reason": "Importing", "message": "copying image"},
				},
			},
		},
	}, &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "PersistentVolumeClaim",
			"metadata": map[string]any{
				"name":      "demo-rootdisk",
				"namespace": "apps",
				"uid":       "pvc-uid",
			},
			"spec":   map[string]any{"storageClassName": "fast-ssd"},
			"status": map[string]any{"phase": "Bound"},
		},
	}, &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "kubevirt.io/v1",
			"kind":       "VirtualMachineInstance",
			"metadata": map[string]any{
				"name":      "demo",
				"namespace": "apps",
				"uid":       "vmi-uid",
			},
			"status": map[string]any{
				"phase":    "Running",
				"nodeName": "node-a",
				"interfaces": []any{
					map[string]any{"name": "default", "ipAddress": "10.1.2.3"},
				},
			},
		},
	})
	adapter := NewKubeVirtAdapter(stubBundleProvider{bundle: &kubeinfra.Bundle{Dynamic: client}})

	vm, err := adapter.CreateVM(context.Background(), Connection{ClusterID: "c1"}, CreateVMInput{
		Name:       "demo",
		Namespace:  "apps",
		SourceMode: "datasource_clone",
		SourceRef:  "ubuntu",
		ProviderParams: map[string]any{
			"storageClass":   "fast-ssd",
			"dataVolumeName": "demo-rootdisk",
		},
	})
	if err != nil {
		t.Fatalf("CreateVM() error = %v", err)
	}
	if vm.Metadata["dataVolumeName"] != "demo-rootdisk" || vm.Metadata["dataVolumePhase"] != "ImportInProgress" || vm.Metadata["dataVolumeProgress"] != "45.0%" {
		t.Fatalf("dataVolume metadata = %#v", vm.Metadata)
	}
	if vm.Metadata["pvcPhase"] != "Bound" || vm.Metadata["vmiStatus"] != "Running" {
		t.Fatalf("pvc/vmi metadata = %#v", vm.Metadata)
	}
	if len(vm.IPAddresses) != 1 || vm.IPAddresses[0] != "10.1.2.3" {
		t.Fatalf("IPAddresses = %#v", vm.IPAddresses)
	}
}

func TestKubeVirtAdapterPowerActionsUpdateRunStrategyAndDelete(t *testing.T) {
	tests := []struct {
		name            string
		action          PowerAction
		wantRunStrategy string
		wantDeleted     bool
	}{
		{name: "start", action: PowerActionStart, wantRunStrategy: "Always"},
		{name: "stop", action: PowerActionStop, wantRunStrategy: "Halted"},
		{name: "restart", action: PowerActionRestart, wantRunStrategy: "Always"},
		{name: "delete", action: PowerActionDelete, wantDeleted: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			client := fake.NewSimpleDynamicClientWithCustomListKinds(scheme, kubeVirtTestListKinds(), &unstructured.Unstructured{
				Object: map[string]any{
					"apiVersion": "kubevirt.io/v1",
					"kind":       "VirtualMachine",
					"metadata": map[string]any{
						"name":      "demo",
						"namespace": "apps",
					},
					"spec": map[string]any{"runStrategy": "Halted"},
				},
			})
			adapter := NewKubeVirtAdapter(stubBundleProvider{bundle: &kubeinfra.Bundle{Dynamic: client}})

			result, err := adapter.PowerAction(context.Background(), Connection{ClusterID: "c1"}, VM{Name: "demo", Namespace: "apps"}, tt.action)
			if err != nil {
				t.Fatalf("PowerAction(%s) error = %v", tt.action, err)
			}
			if !result.Accepted || result.Action != tt.action {
				t.Fatalf("result = %#v", result)
			}

			obj, err := client.Resource(kubeVirtVMGVR).Namespace("apps").Get(context.Background(), "demo", metav1.GetOptions{})
			if tt.wantDeleted {
				if !apierrors.IsNotFound(err) {
					t.Fatalf("Get deleted vm error = %v, want not found", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Get vm error = %v", err)
			}
			runStrategy, _, _ := unstructured.NestedString(obj.Object, "spec", "runStrategy")
			if runStrategy != tt.wantRunStrategy {
				t.Fatalf("runStrategy = %q, want %s", runStrategy, tt.wantRunStrategy)
			}
		})
	}
}
