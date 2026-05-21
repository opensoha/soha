package virtualization

import (
	"context"
	"fmt"

	kubeinfra "github.com/kubecrux/kubecrux/internal/infrastructure/kubernetes"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

type KubeBundleProvider interface {
	Bundle(ctx context.Context, clusterID string) (*kubeinfra.Bundle, error)
}

type KubeVirtAdapter struct {
	bundles KubeBundleProvider
}

func NewKubeVirtAdapter(bundles KubeBundleProvider) *KubeVirtAdapter {
	return &KubeVirtAdapter{bundles: bundles}
}

func (a *KubeVirtAdapter) TestConnection(ctx context.Context, connection Connection) (ConnectionTestResult, error) {
	bundle, err := a.bundle(ctx, connection)
	if err != nil {
		return ConnectionTestResult{Healthy: false, Status: "unsupported", Message: err.Error()}, err
	}
	_, err = bundle.Dynamic.Resource(kubeVirtVMGVR).Namespace(namespaceOrDefault(connection, "default")).List(ctx, metav1.ListOptions{Limit: 1})
	if apierrors.IsNotFound(err) || apierrors.IsMethodNotSupported(err) {
		return ConnectionTestResult{Healthy: false, Status: "degraded", Message: "KubeVirt VirtualMachine CRD is not available"}, nil
	}
	if err != nil {
		return ConnectionTestResult{Healthy: false, Status: "degraded", Message: err.Error()}, nil
	}
	return ConnectionTestResult{Healthy: true, Status: "healthy"}, nil
}

func (a *KubeVirtAdapter) SyncAssets(ctx context.Context, connection Connection) (AssetSyncResult, error) {
	bundle, err := a.bundle(ctx, connection)
	if err != nil {
		return AssetSyncResult{}, err
	}
	namespace := namespaceFromConnection(connection)
	var assets []Asset
	health := AssetHealth{Status: "healthy"}
	for _, item := range []struct {
		gvr        schema.GroupVersionResource
		kind       string
		namespaced bool
	}{
		{kubeVirtVMGVR, "virtualmachine", true},
		{kubeVirtVMIGVR, "virtualmachineinstance", true},
		{kubeVirtDataSourceGVR, "datasource", true},
		{pvcGVR, "persistentvolumeclaim", true},
	} {
		list, listErr := listUnstructured(ctx, bundle.Dynamic, item.gvr, namespace, item.namespaced)
		if apierrors.IsNotFound(listErr) || apierrors.IsMethodNotSupported(listErr) {
			health = AssetHealth{Status: "degraded", Message: fmt.Sprintf("%s resource is not available", item.kind)}
			continue
		}
		if listErr != nil {
			health = AssetHealth{Status: "degraded", Message: listErr.Error()}
			continue
		}
		for i := range list.Items {
			assets = append(assets, assetFromUnstructured(item.kind, &list.Items[i]))
		}
	}
	return AssetSyncResult{Health: health, Assets: assets}, nil
}

func (a *KubeVirtAdapter) CreateVM(ctx context.Context, connection Connection, input CreateVMInput) (VM, error) {
	if input.Name == "" {
		return VM{}, invalidf("vm name is required")
	}
	bundle, err := a.bundle(ctx, connection)
	if err != nil {
		return VM{}, err
	}
	namespace := input.Namespace
	if namespace == "" {
		namespace = namespaceOrDefault(connection, "default")
	}
	object := BuildKubeVirtVM(input)
	object.SetNamespace(namespace)
	created, err := bundle.Dynamic.Resource(kubeVirtVMGVR).Namespace(namespace).Create(ctx, object, metav1.CreateOptions{})
	if err != nil {
		return VM{}, err
	}
	return vmFromUnstructured(created), nil
}

func (a *KubeVirtAdapter) PowerAction(ctx context.Context, connection Connection, vm VM, action PowerAction) (PowerActionResult, error) {
	if vm.Name == "" {
		return PowerActionResult{}, invalidf("vm name is required")
	}
	bundle, err := a.bundle(ctx, connection)
	if err != nil {
		return PowerActionResult{}, err
	}
	namespace := vm.Namespace
	if namespace == "" {
		namespace = namespaceOrDefault(connection, "default")
	}
	resource := bundle.Dynamic.Resource(kubeVirtVMGVR).Namespace(namespace)
	switch action {
	case PowerActionStart:
		return patchKubeVirtRunStrategy(ctx, resource, vm.Name, "Always", action)
	case PowerActionStop:
		return patchKubeVirtRunStrategy(ctx, resource, vm.Name, "Halted", action)
	case PowerActionRestart:
		if _, err := patchKubeVirtRunStrategy(ctx, resource, vm.Name, "Halted", action); err != nil {
			return PowerActionResult{}, err
		}
		return patchKubeVirtRunStrategy(ctx, resource, vm.Name, "Always", action)
	case PowerActionDelete:
		if err := resource.Delete(ctx, vm.Name, metav1.DeleteOptions{}); err != nil {
			return PowerActionResult{}, err
		}
		return PowerActionResult{Accepted: true, Action: action}, nil
	default:
		return PowerActionResult{}, invalidf("unsupported power action %q", action)
	}
}

func (a *KubeVirtAdapter) bundle(ctx context.Context, connection Connection) (*kubeinfra.Bundle, error) {
	if connection.Mode == "agent" {
		return nil, unsupportedf("kubevirt adapter does not support agent-connected clusters")
	}
	if connection.ClusterID == "" {
		return nil, invalidf("cluster id is required")
	}
	if a.bundles == nil {
		return nil, invalidf("kubernetes bundle provider is required")
	}
	bundle, err := a.bundles.Bundle(ctx, connection.ClusterID)
	if err != nil {
		return nil, err
	}
	if bundle == nil || bundle.Dynamic == nil {
		return nil, invalidf("dynamic client bundle is required")
	}
	return bundle, nil
}

var (
	kubeVirtVMGVR         = schema.GroupVersionResource{Group: "kubevirt.io", Version: "v1", Resource: "virtualmachines"}
	kubeVirtVMIGVR        = schema.GroupVersionResource{Group: "kubevirt.io", Version: "v1", Resource: "virtualmachineinstances"}
	kubeVirtDataSourceGVR = schema.GroupVersionResource{Group: "cdi.kubevirt.io", Version: "v1beta1", Resource: "datasources"}
	pvcGVR                = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "persistentvolumeclaims"}
)

func BuildKubeVirtVM(input CreateVMInput) *unstructured.Unstructured {
	runStrategy := "Halted"
	if input.StartAfterCreate {
		runStrategy = "Always"
	}
	labels := map[string]any{"kubecrux.io/managed": "true"}
	disks := []any{
		map[string]any{"name": "rootdisk", "disk": map[string]any{"bus": "virtio"}},
	}
	volumes := []any{
		map[string]any{"name": "rootdisk", "containerDisk": map[string]any{"image": input.BootImage}},
	}
	if input.CloudInit != "" {
		disks = append(disks, map[string]any{"name": "cloudinitdisk", "disk": map[string]any{"bus": "virtio"}})
		volumes = append(volumes, map[string]any{"name": "cloudinitdisk", "cloudInitNoCloud": map[string]any{"userData": input.CloudInit}})
	}
	spec := map[string]any{
		"runStrategy": runStrategy,
		"template": map[string]any{
			"metadata": map[string]any{"labels": labels},
			"spec": map[string]any{
				"domain": map[string]any{
					"cpu": map[string]any{"cores": int64(input.CPU)},
					"devices": map[string]any{
						"disks": disks,
					},
					"resources": map[string]any{"requests": map[string]any{"memory": input.Memory}},
				},
				"volumes": volumes,
			},
		},
	}
	if input.Node != "" {
		_ = unstructured.SetNestedField(spec, map[string]any{"kubernetes.io/hostname": input.Node}, "template", "spec", "nodeSelector")
	}
	if input.Network != "" {
		_ = unstructured.SetNestedSlice(spec, []any{map[string]any{"name": "default", "pod": map[string]any{}}}, "template", "spec", "networks")
		_ = unstructured.SetNestedSlice(spec, []any{map[string]any{"name": "default", "bridge": map[string]any{}}}, "template", "spec", "domain", "devices", "interfaces")
	}
	if input.DiskSize != "" {
		spec["dataVolumeTemplates"] = []any{map[string]any{
			"metadata": map[string]any{"name": input.Name + "-rootdisk"},
			"spec": map[string]any{
				"pvc": map[string]any{
					"resources": map[string]any{
						"requests": map[string]any{"storage": input.DiskSize},
					},
				},
			},
		}}
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "kubevirt.io/v1",
		"kind":       "VirtualMachine",
		"metadata": map[string]any{
			"name":      input.Name,
			"namespace": input.Namespace,
			"labels":    labels,
		},
		"spec": spec,
	}}
}

func patchKubeVirtRunStrategy(ctx context.Context, resource dynamic.ResourceInterface, name string, runStrategy string, action PowerAction) (PowerActionResult, error) {
	obj, err := resource.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return PowerActionResult{}, err
	}
	if err := unstructured.SetNestedField(obj.Object, runStrategy, "spec", "runStrategy"); err != nil {
		return PowerActionResult{}, err
	}
	if _, err := resource.Update(ctx, obj, metav1.UpdateOptions{}); err != nil {
		return PowerActionResult{}, err
	}
	return PowerActionResult{Accepted: true, Action: action}, nil
}

func listUnstructured(ctx context.Context, client dynamic.Interface, gvr schema.GroupVersionResource, namespace string, namespaced bool) (*unstructured.UnstructuredList, error) {
	if namespaced && namespace != "" {
		return client.Resource(gvr).Namespace(namespace).List(ctx, metav1.ListOptions{})
	}
	return client.Resource(gvr).List(ctx, metav1.ListOptions{})
}

func assetFromUnstructured(kind string, item *unstructured.Unstructured) Asset {
	asset := Asset{
		Type:      kind,
		Name:      item.GetName(),
		Namespace: item.GetNamespace(),
		Status:    readStatus(item),
		Metadata:  map[string]string{},
	}
	if node, ok, _ := unstructured.NestedString(item.Object, "spec", "nodeName"); ok {
		asset.Node = node
	}
	return asset
}

func vmFromUnstructured(item *unstructured.Unstructured) VM {
	return VM{
		ID:        string(item.GetUID()),
		Name:      item.GetName(),
		Namespace: item.GetNamespace(),
		Status:    readStatus(item),
		Metadata:  map[string]string{},
	}
}

func readStatus(item *unstructured.Unstructured) string {
	if printable, ok, _ := unstructured.NestedString(item.Object, "status", "printableStatus"); ok {
		return printable
	}
	if phase, ok, _ := unstructured.NestedString(item.Object, "status", "phase"); ok {
		return phase
	}
	return ""
}

func namespaceFromConnection(connection Connection) string {
	if namespace, ok := stringOption(connection.Options, "namespace"); ok {
		return namespace
	}
	return ""
}

func namespaceOrDefault(connection Connection, fallback string) string {
	if namespace, ok := stringOption(connection.Options, "namespace"); ok && namespace != "" {
		return namespace
	}
	return fallback
}

func stringOption(options map[string]any, key string) (string, bool) {
	if options == nil {
		return "", false
	}
	value, ok := options[key]
	if !ok {
		return "", false
	}
	text, ok := value.(string)
	return text, ok
}
