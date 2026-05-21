package virtualization

import (
	"context"
	"errors"
	"testing"

	kubeinfra "github.com/kubecrux/kubecrux/internal/infrastructure/kubernetes"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"
	ktesting "k8s.io/client-go/testing"
)

type stubBundleProvider struct {
	bundle *kubeinfra.Bundle
	err    error
}

func (s stubBundleProvider) Bundle(context.Context, string) (*kubeinfra.Bundle, error) {
	return s.bundle, s.err
}

func TestBuildKubeVirtVM(t *testing.T) {
	vm := BuildKubeVirtVM(CreateVMInput{
		Name:             "demo",
		Namespace:        "apps",
		Node:             "node-a",
		CPU:              2,
		Memory:           "4Gi",
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
	volumes, _, _ := unstructured.NestedSlice(vm.Object, "spec", "template", "spec", "volumes")
	if len(volumes) != 2 {
		t.Fatalf("volumes len = %d, want 2", len(volumes))
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
		kubeVirtVMGVR:         "VirtualMachineList",
		kubeVirtVMIGVR:        "VirtualMachineInstanceList",
		kubeVirtDataSourceGVR: "DataSourceList",
		pvcGVR:                "PersistentVolumeClaimList",
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
