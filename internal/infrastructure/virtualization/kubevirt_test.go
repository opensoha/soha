package virtualization

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	kubeinfra "github.com/soha/soha/internal/infrastructure/kubernetes"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"
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
	dataVolumes, _, _ := unstructured.NestedSlice(vm.Object, "spec", "dataVolumeTemplates")
	if len(dataVolumes) != 1 {
		t.Fatalf("dataVolumeTemplates len = %d, want 1", len(dataVolumes))
	}
	volumes, _, _ := unstructured.NestedSlice(vm.Object, "spec", "template", "spec", "volumes")
	if len(volumes) == 0 {
		t.Fatalf("volumes len = 0")
	}
	root := volumes[0].(map[string]any)
	if _, ok := root["dataVolume"]; !ok {
		t.Fatalf("root volume = %#v, want dataVolume", root)
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
	volumes, _, _ := unstructured.NestedSlice(vm.Object, "spec", "template", "spec", "volumes")
	if len(volumes) == 0 {
		t.Fatalf("volumes len = 0")
	}
	root := volumes[0].(map[string]any)
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
	root := volumes[0].(map[string]any)
	containerDisk := root["containerDisk"].(map[string]any)
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

func TestKubeVirtAdapterSyncAssetsDegradesWhenCRDMissing(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewSimpleDynamicClientWithCustomListKinds(scheme, map[schema.GroupVersionResource]string{
		kubeVirtVMGVR:                  "VirtualMachineList",
		kubeVirtVMIGVR:                 "VirtualMachineInstanceList",
		kubeVirtInstancetypeGVR:        "VirtualMachineInstancetypeList",
		kubeVirtClusterInstancetypeGVR: "VirtualMachineClusterInstancetypeList",
		kubeVirtDataSourceGVR:          "DataSourceList",
		pvcGVR:                         "PersistentVolumeClaimList",
	}, &unstructured.Unstructured{
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

func TestKubeVirtAdapterSyncAssetsUsesOwnerUIDForVMInstance(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewSimpleDynamicClientWithCustomListKinds(scheme, map[schema.GroupVersionResource]string{
		kubeVirtVMGVR:                  "VirtualMachineList",
		kubeVirtVMIGVR:                 "VirtualMachineInstanceList",
		kubeVirtInstancetypeGVR:        "VirtualMachineInstancetypeList",
		kubeVirtClusterInstancetypeGVR: "VirtualMachineClusterInstancetypeList",
		kubeVirtDataSourceGVR:          "DataSourceList",
		pvcGVR:                         "PersistentVolumeClaimList",
	}, &unstructured.Unstructured{
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
	client := fake.NewSimpleDynamicClientWithCustomListKinds(scheme, map[schema.GroupVersionResource]string{
		kubeVirtVMGVR:                  "VirtualMachineList",
		kubeVirtVMIGVR:                 "VirtualMachineInstanceList",
		kubeVirtInstancetypeGVR:        "VirtualMachineInstancetypeList",
		kubeVirtClusterInstancetypeGVR: "VirtualMachineClusterInstancetypeList",
		kubeVirtDataSourceGVR:          "DataSourceList",
		pvcGVR:                         "PersistentVolumeClaimList",
	}, &unstructured.Unstructured{
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
	client := fake.NewSimpleDynamicClientWithCustomListKinds(scheme, map[schema.GroupVersionResource]string{
		kubeVirtVMGVR: "VirtualMachineList",
	}, &unstructured.Unstructured{
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

func TestKubeVirtAdapterTestConnectionDegradesWhenCRDMissing(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewSimpleDynamicClientWithCustomListKinds(scheme, map[schema.GroupVersionResource]string{
		kubeVirtVMGVR: "VirtualMachineList",
	})
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
	client := fake.NewSimpleDynamicClientWithCustomListKinds(scheme, map[schema.GroupVersionResource]string{
		kubeVirtVMIGVR: "VirtualMachineInstanceList",
	}, &unstructured.Unstructured{
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
	if result.BackendHeaders.Get("Authorization") != "Bearer token-1" {
		t.Fatalf("Authorization header = %q", result.BackendHeaders.Get("Authorization"))
	}
	if result.BackendTLSConfig == nil || !result.BackendTLSConfig.InsecureSkipVerify {
		t.Fatalf("BackendTLSConfig = %#v, want kube REST TLS config", result.BackendTLSConfig)
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal console result: %v", err)
	}
	if strings.Contains(string(raw), "token-1") || strings.Contains(string(raw), "BackendHeaders") || strings.Contains(string(raw), "BackendTLSConfig") {
		t.Fatalf("console result leaked backend internals: %s", raw)
	}
}

func TestKubeVirtAdapterCreateAndPowerAction(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewSimpleDynamicClientWithCustomListKinds(scheme, map[schema.GroupVersionResource]string{
		kubeVirtVMGVR: "VirtualMachineList",
	})
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
			client := fake.NewSimpleDynamicClientWithCustomListKinds(scheme, map[schema.GroupVersionResource]string{
				kubeVirtVMGVR: "VirtualMachineList",
			}, &unstructured.Unstructured{
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
