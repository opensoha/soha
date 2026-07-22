package virtualization

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	kubeinfra "github.com/opensoha/soha/internal/infrastructure/kubernetes"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

type KubeBundleProvider interface {
	Bundle(ctx context.Context, clusterID string) (*kubeinfra.Bundle, error)
}

type KubeVirtAdapter struct {
	bundles KubeBundleProvider
}

func (a *KubeVirtAdapter) VMCapabilities() []string {
	return []string{CapabilityResizeCPU, CapabilityResizeMemory}
}

func (a *KubeVirtAdapter) ListVMDevices(ctx context.Context, connection Connection, vm VM) ([]VMDevice, error) {
	bundle, err := a.bundle(ctx, connection)
	if err != nil {
		return nil, err
	}
	namespace := vm.Namespace
	if namespace == "" {
		namespace = namespaceOrDefault(connection, "default")
	}
	item, err := bundle.Dynamic.Resource(kubeVirtVMGVR).Namespace(namespace).Get(ctx, vm.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	devices := make([]VMDevice, 0)
	if disks, ok, _ := unstructured.NestedSlice(item.Object, "spec", "template", "spec", "domain", "devices", "disks"); ok {
		for _, raw := range disks {
			if disk, ok := raw.(map[string]any); ok {
				id := stringFromAny(disk["name"])
				devices = append(devices, VMDevice{ID: id, Kind: "disk", Name: id})
			}
		}
	}
	if interfaces, ok, _ := unstructured.NestedSlice(item.Object, "spec", "template", "spec", "domain", "devices", "interfaces"); ok {
		for _, raw := range interfaces {
			if iface, ok := raw.(map[string]any); ok {
				id := stringFromAny(iface["name"])
				devices = append(devices, VMDevice{ID: id, Kind: "network", Name: id, Model: stringFromAny(iface["model"])})
			}
		}
	}
	return devices, nil
}

func NewKubeVirtAdapter(bundles KubeBundleProvider) *KubeVirtAdapter {
	return &KubeVirtAdapter{bundles: bundles}
}

func (a *KubeVirtAdapter) TestConnection(ctx context.Context, connection Connection) (ConnectionTestResult, error) {
	bundle, err := a.bundle(ctx, connection)
	if err != nil {
		if errors.Is(err, ErrUnsupported) {
			result := ConnectionTestResult{Healthy: false, Status: "unsupported", Message: err.Error(), Reason: "unsupported", NextAction: "use a direct Kubernetes client connection for KubeVirt virtualization"}
			if details, ok := AdapterErrorDetails(err); ok {
				result.Reason = details.Reason
				result.NextAction = details.NextAction
				result.Message = details.Error()
			}
			return result, nil
		}
		return ConnectionTestResult{Healthy: false, Status: "unsupported", Message: err.Error()}, err
	}
	namespace := namespaceOrDefault(connection, "default")
	if bundle.Typed != nil {
		if _, err := bundle.Typed.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{}); apierrors.IsNotFound(err) {
			return ConnectionTestResult{
				Healthy:    false,
				Status:     "degraded",
				Message:    fmt.Sprintf("Kubernetes namespace %q is not available", namespace),
				Reason:     "namespace_not_found",
				NextAction: "create the namespace or update the KubeVirt connection default namespace",
			}, nil
		} else if err != nil {
			return ConnectionTestResult{Healthy: false, Status: "degraded", Message: err.Error()}, nil
		}
	}
	_, err = bundle.Dynamic.Resource(kubeVirtVMGVR).Namespace(namespace).List(ctx, metav1.ListOptions{Limit: 1})
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
		gvr      schema.GroupVersionResource
		kind     string
		scope    string
		required bool
	}{
		{kubeVirtVMGVR, "virtualmachine", "namespace", true},
		{kubeVirtVMIGVR, "virtualmachineinstance", "namespace", true},
		{kubeVirtInstancetypeGVR, "flavor", "namespace", false},
		{kubeVirtClusterInstancetypeGVR, "flavor", "cluster", false},
		{kubeVirtDataSourceGVR, "datasource", "namespace", false},
		{kubeVirtDataVolumeGVR, "datavolume", "namespace", false},
		{kubeVirtNADGVR, "networkattachmentdefinition", "namespace", false},
		{pvcGVR, "persistentvolumeclaim", "namespace", false},
	} {
		list, listErr := listUnstructured(ctx, bundle.Dynamic, item.gvr, namespace, item.scope == "namespace")
		if apierrors.IsNotFound(listErr) || apierrors.IsMethodNotSupported(listErr) {
			message := fmt.Sprintf("%s resource is not available", item.kind)
			if item.required {
				health = AssetHealth{Status: "degraded", Message: message}
			} else if health.Status == "healthy" {
				health.Message = appendHealthMessage(health.Message, message)
			}
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
	vm := vmFromUnstructured(created)
	a.enrichCreatedVM(ctx, bundle.Dynamic, namespace, input, &vm)
	return vm, nil
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

func (a *KubeVirtAdapter) ResizeVM(ctx context.Context, connection Connection, vm VM, input AdapterResizeVMInput) (PowerActionResult, error) {
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
	patch := map[string]any{"spec": map[string]any{"template": map[string]any{"spec": map[string]any{"domain": map[string]any{}}}}}
	domain := patch["spec"].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any)["domain"].(map[string]any)
	if input.CPU > 0 {
		domain["cpu"] = map[string]any{"cores": input.CPU}
	}
	if input.MemoryMiB > 0 {
		domain["resources"] = map[string]any{"requests": map[string]any{"memory": fmt.Sprintf("%dMi", input.MemoryMiB)}}
	}
	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return PowerActionResult{}, err
	}
	if _, err := bundle.Dynamic.Resource(kubeVirtVMGVR).Namespace(namespace).Patch(ctx, vm.Name, types.MergePatchType, patchBytes, metav1.PatchOptions{}); err != nil {
		return PowerActionResult{}, err
	}
	if input.DiskGiB > 0 {
		return PowerActionResult{Accepted: true, Action: "resize", Message: "CPU and memory updated; disk expansion requires a DataVolume operation"}, nil
	}
	return PowerActionResult{Accepted: true, Action: "resize", Message: "virtual machine resources updated"}, nil
}

func (a *KubeVirtAdapter) bundle(ctx context.Context, connection Connection) (*kubeinfra.Bundle, error) {
	if connection.Mode == "agent" {
		return nil, kubeVirtAgentModeUnsupportedError()
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
	kubeVirtVMGVR                  = schema.GroupVersionResource{Group: "kubevirt.io", Version: "v1", Resource: "virtualmachines"}
	kubeVirtVMIGVR                 = schema.GroupVersionResource{Group: "kubevirt.io", Version: "v1", Resource: "virtualmachineinstances"}
	kubeVirtInstancetypeGVR        = schema.GroupVersionResource{Group: "instancetype.kubevirt.io", Version: "v1beta1", Resource: "virtualmachineinstancetypes"}
	kubeVirtClusterInstancetypeGVR = schema.GroupVersionResource{Group: "instancetype.kubevirt.io", Version: "v1beta1", Resource: "virtualmachineclusterinstancetypes"}
	kubeVirtDataSourceGVR          = schema.GroupVersionResource{Group: "cdi.kubevirt.io", Version: "v1beta1", Resource: "datasources"}
	kubeVirtDataVolumeGVR          = schema.GroupVersionResource{Group: "cdi.kubevirt.io", Version: "v1beta1", Resource: "datavolumes"}
	kubeVirtNADGVR                 = schema.GroupVersionResource{Group: "k8s.cni.cncf.io", Version: "v1", Resource: "network-attachment-definitions"}
	pvcGVR                         = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "persistentvolumeclaims"}
)

func BuildKubeVirtVM(input CreateVMInput) *unstructured.Unstructured {
	runStrategy := "Halted"
	if input.StartAfterCreate {
		runStrategy = "Always"
	}
	labels := map[string]any{"soha.io/managed": "true"}
	disks := []any{
		map[string]any{"name": "rootdisk", "disk": map[string]any{"bus": "virtio"}},
	}
	volumes := []any{}
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
	if architecture := kubeVirtArchitecture(input.Architecture); architecture != "" {
		_ = unstructured.SetNestedField(spec, architecture, "template", "spec", "architecture")
	}
	storageClass := stringOption(input.ProviderParams, "storageClass")
	dataVolumeName := firstNonEmpty(stringOption(input.ProviderParams, "dataVolumeName"), input.Name+"-rootdisk")
	accessModes := kubeVirtDataVolumeAccessModes(input.ProviderParams)
	volumeMode := stringOption(input.ProviderParams, "volumeMode")
	sourceRef := firstNonEmpty(input.SourceRef, input.BootImage)
	sourceMode := firstNonEmpty(input.SourceMode, "datasource_clone")
	switch sourceMode {
	case "pvc_clone":
		volumes = append(volumes, map[string]any{"name": "rootdisk", "persistentVolumeClaim": map[string]any{"claimName": sourceRef}})
	case "datasource_clone":
		volumes = append(volumes, map[string]any{"name": "rootdisk", "dataVolume": map[string]any{"name": dataVolumeName}})
		dataVolume := map[string]any{
			"metadata": map[string]any{"name": dataVolumeName},
			"spec": map[string]any{
				"sourceRef": map[string]any{
					"kind":      "DataSource",
					"name":      sourceRef,
					"namespace": input.Namespace,
				},
				"storage": map[string]any{
					"resources": map[string]any{
						"requests": map[string]any{"storage": input.DiskSize},
					},
				},
			},
		}
		if storageClass != "" {
			_ = unstructured.SetNestedField(dataVolume, storageClass, "spec", "storage", "storageClassName")
		}
		if len(accessModes) > 0 {
			_ = unstructured.SetNestedStringSlice(dataVolume, accessModes, "spec", "storage", "accessModes")
		}
		if volumeMode != "" {
			_ = unstructured.SetNestedField(dataVolume, volumeMode, "spec", "storage", "volumeMode")
		}
		spec["dataVolumeTemplates"] = []any{dataVolume}
	default:
		volumes = append(volumes, map[string]any{"name": "rootdisk", "containerDisk": map[string]any{"image": sourceRef}})
	}
	if input.CloudInit != "" {
		disks = append(disks, map[string]any{"name": "cloudinitdisk", "disk": map[string]any{"bus": "virtio"}})
		volumes = append(volumes, map[string]any{"name": "cloudinitdisk", "cloudInitNoCloud": map[string]any{"userData": input.CloudInit}})
	}
	if input.Node != "" {
		_ = unstructured.SetNestedField(spec, map[string]any{"kubernetes.io/hostname": input.Node}, "template", "spec", "nodeSelector")
	}
	if networks, interfaces := kubeVirtNetworkSpec(input); len(networks) > 0 && len(interfaces) > 0 {
		_ = unstructured.SetNestedSlice(spec, networks, "template", "spec", "networks")
		_ = unstructured.SetNestedSlice(spec, interfaces, "template", "spec", "domain", "devices", "interfaces")
	}
	_ = unstructured.SetNestedSlice(spec, disks, "template", "spec", "domain", "devices", "disks")
	_ = unstructured.SetNestedSlice(spec, volumes, "template", "spec", "volumes")
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

func kubeVirtNetworkSpec(input CreateVMInput) ([]any, []any) {
	params := input.ProviderParams
	networkType := strings.ToLower(firstNonEmpty(
		stringOption(params, "networkType"),
		stringOption(params, "kubevirtNetworkType"),
	))
	networkAttachment := firstNonEmpty(
		stringOption(params, "networkAttachmentDefinition"),
		stringOption(params, "networkAttachmentName"),
		stringOption(params, "nadName"),
	)
	networkInput := strings.TrimSpace(input.Network)
	if networkAttachment == "" && networkType == "multus" {
		networkAttachment = networkInput
	}
	if networkType == "" {
		switch strings.ToLower(networkInput) {
		case "":
			return nil, nil
		case "pod", "default":
			networkType = "pod"
		default:
			networkType = "multus"
			networkAttachment = networkInput
		}
	}

	name := firstNonEmpty(
		stringOption(params, "interfaceName"),
		stringOption(params, "networkInterfaceName"),
	)
	if name == "" {
		if networkType == "pod" {
			name = "default"
		} else {
			name = kubeVirtNetworkObjectName(networkAttachment)
		}
	}
	if name == "" {
		name = "default"
	}

	iface := map[string]any{"name": name}
	switch strings.ToLower(firstNonEmpty(stringOption(params, "interfaceBinding"), stringOption(params, "bindingMethod"), "bridge")) {
	case "masquerade":
		iface["masquerade"] = map[string]any{}
	case "sriov":
		iface["sriov"] = map[string]any{}
	default:
		iface["bridge"] = map[string]any{}
	}
	if model := firstNonEmpty(stringOption(params, "interfaceModel"), stringOption(params, "model")); model != "" {
		iface["model"] = model
	}

	if networkType == "multus" {
		if networkAttachment == "" {
			return nil, nil
		}
		return []any{map[string]any{"name": name, "multus": map[string]any{"networkName": networkAttachment}}}, []any{iface}
	}
	return []any{map[string]any{"name": name, "pod": map[string]any{}}}, []any{iface}
}

func kubeVirtNetworkObjectName(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if slash := strings.LastIndex(trimmed, "/"); slash >= 0 && slash < len(trimmed)-1 {
		trimmed = trimmed[slash+1:]
	}
	var builder strings.Builder
	for _, r := range strings.ToLower(trimmed) {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '-' || r == '.':
			builder.WriteRune('-')
		default:
			builder.WriteRune('-')
		}
	}
	name := strings.Trim(builder.String(), "-")
	if len(name) > 63 {
		name = strings.TrimRight(name[:63], "-")
	}
	if name == "" {
		return "net1"
	}
	return name
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

func appendHealthMessage(current, message string) string {
	current = strings.TrimSpace(current)
	message = strings.TrimSpace(message)
	if current == "" {
		return message
	}
	if message == "" || strings.Contains(current, message) {
		return current
	}
	return current + "; " + message
}

func assetFromUnstructured(kind string, item *unstructured.Unstructured) Asset {
	asset := Asset{
		Type:      kind,
		Name:      item.GetName(),
		Namespace: item.GetNamespace(),
		Status:    readStatus(item),
		Metadata: map[string]string{
			"uid": string(item.GetUID()),
		},
	}
	switch kind {
	case "virtualmachineinstance":
		addVMIAssetMetadata(asset.Metadata, item)
	case "virtualmachine":
		addVMAssetMetadata(asset.Metadata, item)
	case "flavor":
		addFlavorAssetMetadata(asset.Metadata, item)
	case "datavolume":
		addDataVolumeAssetMetadata(asset.Metadata, item)
	case "persistentvolumeclaim":
		addPVCAssetMetadata(asset.Metadata, item)
	case "networkattachmentdefinition":
		asset.Metadata["sourceKind"] = "networkattachmentdefinition"
		asset.Metadata["networkAttachmentDefinition"] = namespacedName(item.GetNamespace(), item.GetName())
	}
	asset.Node = unstructuredAssetNode(item)
	return asset
}

func addVMIAssetMetadata(metadata map[string]string, item *unstructured.Unstructured) {
	for _, owner := range item.GetOwnerReferences() {
		if owner.Kind == "VirtualMachine" && owner.UID != "" {
			metadata["uid"] = string(owner.UID)
			metadata["vmiUid"] = string(item.GetUID())
			break
		}
	}
	if ips := vmiIPAddresses(item); len(ips) > 0 {
		metadata["ipAddress"] = ips[0]
		metadata["ipAddresses"] = strings.Join(ips, ",")
	}
}

func addVMAssetMetadata(metadata map[string]string, item *unstructured.Unstructured) {
	if printable := nestedString(item.Object, "status", "printableStatus"); printable != "" {
		metadata["printableStatus"] = printable
	}
	if runStrategy := nestedString(item.Object, "spec", "runStrategy"); runStrategy != "" {
		metadata["runStrategy"] = runStrategy
	}
	if cpu := nestedInt64(item.Object, "spec", "template", "spec", "domain", "cpu", "cores"); cpu > 0 {
		metadata["cpu"] = strconv.FormatInt(cpu, 10)
	}
	if memory := nestedString(item.Object, "spec", "template", "spec", "domain", "resources", "requests", "memory"); memory != "" {
		metadata["memory"] = memory
	}
	addVMBootSourceMetadata(metadata, item)
	addVMDataVolumeTemplateMetadata(metadata, item)
}

func addVMBootSourceMetadata(metadata map[string]string, item *unstructured.Unstructured) {
	sourceMode, sourceRef := vmBootSource(item)
	if sourceRef == "" {
		return
	}
	metadata["sourceMode"] = sourceMode
	metadata["sourceRef"] = sourceRef
	switch sourceMode {
	case "datasource_clone":
		metadata["dataVolumeName"] = sourceRef
	case "pvc_clone":
		metadata["pvcName"] = sourceRef
	}
}

func unstructuredAssetNode(item *unstructured.Unstructured) string {
	if node, ok, _ := unstructured.NestedString(item.Object, "spec", "nodeName"); ok {
		return node
	}
	node, _, _ := unstructured.NestedString(item.Object, "status", "nodeName")
	return node
}

func addFlavorAssetMetadata(metadata map[string]string, item *unstructured.Unstructured) {
	metadata["cpu"] = strconv.FormatInt(nestedInt64(item.Object, "spec", "cpu", "guest"), 10)
	metadata["memory"] = nestedString(item.Object, "spec", "memory", "guest")
	metadata["scope"] = "namespace"
	if item.GetNamespace() == "" {
		metadata["scope"] = "cluster"
	}
}

func addDataVolumeAssetMetadata(metadata map[string]string, item *unstructured.Unstructured) {
	copyNestedMetadata(metadata, item, []nestedMetadataField{
		{"phase", []string{"status", "phase"}},
		{"progress", []string{"status", "progress"}},
		{"storageClass", []string{"spec", "storage", "storageClassName"}},
		{"sourceKind", []string{"spec", "sourceRef", "kind"}},
		{"sourceRef", []string{"spec", "sourceRef", "name"}},
		{"sourceNamespace", []string{"spec", "sourceRef", "namespace"}},
	})
	metadata["pvcName"] = firstNonEmpty(
		nestedString(item.Object, "status", "claimName"), item.GetName(),
	)
	addConditionMetadata(metadata, item, "")
}

func addPVCAssetMetadata(metadata map[string]string, item *unstructured.Unstructured) {
	copyNestedMetadata(metadata, item, []nestedMetadataField{
		{"phase", []string{"status", "phase"}},
		{"storageClass", []string{"spec", "storageClassName"}},
		{"requestedStorage", []string{"spec", "resources", "requests", "storage"}},
		{"capacityStorage", []string{"status", "capacity", "storage"}},
		{"volumeName", []string{"spec", "volumeName"}},
	})
	addConditionMetadata(metadata, item, "")
}

type nestedMetadataField struct {
	name string
	path []string
}

func copyNestedMetadata(
	metadata map[string]string,
	item *unstructured.Unstructured,
	fields []nestedMetadataField,
) {
	for _, field := range fields {
		if value := nestedString(item.Object, field.path...); value != "" {
			metadata[field.name] = value
		}
	}
}

func vmFromUnstructured(item *unstructured.Unstructured) VM {
	metadata := map[string]string{}
	if printable := nestedString(item.Object, "status", "printableStatus"); printable != "" {
		metadata["printableStatus"] = printable
	}
	if sourceMode, sourceRef := vmBootSource(item); sourceRef != "" {
		metadata["sourceMode"] = sourceMode
		metadata["sourceRef"] = sourceRef
		if sourceMode == "datasource_clone" {
			metadata["dataVolumeName"] = sourceRef
		}
		if sourceMode == "pvc_clone" {
			metadata["pvcName"] = sourceRef
		}
	}
	addVMDataVolumeTemplateMetadata(metadata, item)
	return VM{
		ID:        string(item.GetUID()),
		Name:      item.GetName(),
		Namespace: item.GetNamespace(),
		Status:    readStatus(item),
		Metadata:  metadata,
	}
}

func (a *KubeVirtAdapter) enrichCreatedVM(ctx context.Context, client dynamic.Interface, namespace string, input CreateVMInput, vm *VM) {
	if vm == nil || client == nil {
		return
	}
	if vm.Metadata == nil {
		vm.Metadata = map[string]string{}
	}
	sourceMode := firstNonEmpty(input.SourceMode, vm.Metadata["sourceMode"], "datasource_clone")
	sourceRef := firstNonEmpty(input.SourceRef, input.BootImage, vm.Metadata["sourceRef"])
	if sourceMode != "" {
		vm.Metadata["sourceMode"] = sourceMode
	}
	if sourceRef != "" {
		vm.Metadata["sourceRef"] = sourceRef
	}
	if storageClass := stringOption(input.ProviderParams, "storageClass"); storageClass != "" {
		vm.Metadata["storageClass"] = storageClass
	}

	dataVolumeName := ""
	pvcName := ""
	switch sourceMode {
	case "datasource_clone":
		dataVolumeName = firstNonEmpty(stringOption(input.ProviderParams, "dataVolumeName"), vm.Metadata["dataVolumeName"], input.Name+"-rootdisk")
		pvcName = dataVolumeName
	case "pvc_clone":
		pvcName = sourceRef
	}
	if dataVolumeName != "" {
		vm.Metadata["dataVolumeName"] = dataVolumeName
		if dv, err := client.Resource(kubeVirtDataVolumeGVR).Namespace(namespace).Get(ctx, dataVolumeName, metav1.GetOptions{}); err == nil {
			mergePrefixedMetadata(vm.Metadata, "dataVolume", assetFromUnstructured("datavolume", dv).Metadata)
		}
	}
	if pvcName != "" {
		vm.Metadata["pvcName"] = pvcName
		if pvc, err := client.Resource(pvcGVR).Namespace(namespace).Get(ctx, pvcName, metav1.GetOptions{}); err == nil {
			mergePrefixedMetadata(vm.Metadata, "pvc", assetFromUnstructured("persistentvolumeclaim", pvc).Metadata)
		}
	}
	if vmi, err := client.Resource(kubeVirtVMIGVR).Namespace(namespace).Get(ctx, input.Name, metav1.GetOptions{}); err == nil {
		asset := assetFromUnstructured("virtualmachineinstance", vmi)
		mergePrefixedMetadata(vm.Metadata, "vmi", asset.Metadata)
		if asset.Status != "" {
			vm.Metadata["vmiStatus"] = asset.Status
		}
		if asset.Node != "" {
			vm.Node = asset.Node
		}
		if ips := commaSeparatedStrings(asset.Metadata["ipAddresses"]); len(ips) > 0 {
			vm.IPAddresses = uniqueStrings(append(vm.IPAddresses, ips...))
		}
	}
}

func mergePrefixedMetadata(target map[string]string, prefix string, source map[string]string) {
	for key, value := range source {
		if key == "" || value == "" || key == "uid" {
			continue
		}
		target[prefix+strings.ToUpper(key[:1])+key[1:]] = value
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

func vmBootSource(item *unstructured.Unstructured) (string, string) {
	volumes, ok, _ := unstructured.NestedSlice(item.Object, "spec", "template", "spec", "volumes")
	if !ok {
		return "", ""
	}
	for _, raw := range volumes {
		volume, ok := raw.(map[string]any)
		if !ok || volume["name"] != "rootdisk" {
			continue
		}
		if containerDisk, ok := volume["containerDisk"].(map[string]any); ok {
			if image, ok := containerDisk["image"].(string); ok {
				return "containerdisk", image
			}
		}
		if pvc, ok := volume["persistentVolumeClaim"].(map[string]any); ok {
			if claimName, ok := pvc["claimName"].(string); ok {
				return "pvc_clone", claimName
			}
		}
		if dataVolume, ok := volume["dataVolume"].(map[string]any); ok {
			if name, ok := dataVolume["name"].(string); ok {
				return "datasource_clone", name
			}
		}
	}
	return "", ""
}

func addVMDataVolumeTemplateMetadata(metadata map[string]string, item *unstructured.Unstructured) {
	templates, ok, _ := unstructured.NestedSlice(item.Object, "spec", "dataVolumeTemplates")
	if !ok || len(templates) == 0 {
		return
	}
	template, ok := templates[0].(map[string]any)
	if !ok {
		return
	}
	if name := nestedString(template, "metadata", "name"); name != "" {
		metadata["dataVolumeName"] = name
	}
	if storageClass := nestedString(template, "spec", "storage", "storageClassName"); storageClass != "" {
		metadata["storageClass"] = storageClass
	}
	if sourceName := nestedString(template, "spec", "sourceRef", "name"); sourceName != "" {
		metadata["dataSourceName"] = sourceName
	}
	if sourceNamespace := nestedString(template, "spec", "sourceRef", "namespace"); sourceNamespace != "" {
		metadata["dataSourceNamespace"] = sourceNamespace
	}
}

func addConditionMetadata(metadata map[string]string, item *unstructured.Unstructured, prefix string) {
	conditions, ok, _ := unstructured.NestedSlice(item.Object, "status", "conditions")
	if !ok || len(conditions) == 0 {
		return
	}
	if raw, err := json.Marshal(conditions); err == nil {
		metadata[prefix+"conditions"] = string(raw)
	}
	for _, entry := range conditions {
		condition, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		conditionType, _ := condition["type"].(string)
		status, _ := condition["status"].(string)
		reason, _ := condition["reason"].(string)
		message, _ := condition["message"].(string)
		if conditionType != "" && status != "" {
			metadata[prefix+"condition."+conditionType] = status
		}
		if strings.EqualFold(status, "False") || strings.EqualFold(conditionType, "Failure") || strings.EqualFold(conditionType, "Failed") {
			if reason != "" && metadata[prefix+"failureReason"] == "" {
				metadata[prefix+"failureReason"] = reason
			}
			if message != "" && metadata[prefix+"failureMessage"] == "" {
				metadata[prefix+"failureMessage"] = message
			}
		}
	}
}

func vmiIPAddresses(item *unstructured.Unstructured) []string {
	interfaces, ok, _ := unstructured.NestedSlice(item.Object, "status", "interfaces")
	if !ok {
		return nil
	}
	var out []string
	for _, raw := range interfaces {
		iface, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if ip, ok := iface["ipAddress"].(string); ok {
			out = append(out, ip)
		}
		out = append(out, stringSliceFromAny(iface["ipAddresses"])...)
		out = append(out, stringSliceFromAny(iface["ips"])...)
	}
	return uniqueStrings(out)
}

func stringSliceFromAny(value any) []string {
	switch typed := value.(type) {
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok {
				out = append(out, text)
			}
		}
		return out
	case []string:
		return typed
	case string:
		return []string{typed}
	default:
		return nil
	}
}

func commaSeparatedStrings(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return uniqueStrings(strings.Split(value, ","))
}

func uniqueStrings(items []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		value := strings.TrimSpace(item)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func namespacedName(namespace, name string) string {
	if strings.TrimSpace(namespace) == "" {
		return strings.TrimSpace(name)
	}
	return strings.TrimSpace(namespace) + "/" + strings.TrimSpace(name)
}

func nestedString(item map[string]any, fields ...string) string {
	value, ok, _ := unstructured.NestedString(item, fields...)
	if !ok {
		return ""
	}
	return value
}

func nestedInt64(item map[string]any, fields ...string) int64 {
	value, ok, _ := unstructured.NestedInt64(item, fields...)
	if ok {
		return value
	}
	floatValue, ok, _ := unstructured.NestedFloat64(item, fields...)
	if ok {
		return int64(floatValue)
	}
	return 0
}

func namespaceFromConnection(connection Connection) string {
	if namespace := stringOption(connection.Options, "namespace"); namespace != "" {
		return namespace
	}
	return ""
}

func namespaceOrDefault(connection Connection, fallback string) string {
	if namespace := stringOption(connection.Options, "namespace"); namespace != "" {
		return namespace
	}
	return fallback
}

func kubeVirtAgentModeUnsupportedError() error {
	return &AdapterError{
		Cause:      ErrUnsupported,
		Reason:     "agent_mode_unsupported",
		Message:    "kubevirt virtualization requires a direct Kubernetes client connection; agent-connected clusters are not supported yet",
		NextAction: "create or select a direct_kubeconfig Kubernetes cluster for KubeVirt virtualization",
	}
}

func kubeVirtArchitecture(value string) string {
	normalized := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(value)), "linux/")
	switch normalized {
	case "amd64", "x86_64", "x64", "x86":
		return "amd64"
	case "arm64", "aarch64":
		return "arm64"
	default:
		return ""
	}
}

func stringOption(options map[string]any, key string) string {
	if options == nil {
		return ""
	}
	value, ok := options[key]
	if !ok {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return text
}

func stringOptionValue(options map[string]any, key string) string {
	return stringOption(options, key)
}

func kubeVirtDataVolumeAccessModes(options map[string]any) []string {
	if options == nil {
		return []string{"ReadWriteOnce"}
	}
	if value, ok := options["accessModes"]; ok {
		modes := uniqueStrings(stringSliceFromAny(value))
		if len(modes) > 0 {
			return modes
		}
	}
	if value := strings.TrimSpace(stringOption(options, "accessMode")); value != "" {
		modes := uniqueStrings(commaSeparatedStrings(value))
		if len(modes) > 0 {
			return modes
		}
		return []string{value}
	}
	return []string{"ReadWriteOnce"}
}

func (a *KubeVirtAdapter) GetVMMetrics(ctx context.Context, connection Connection, vm VM, rangeMinutes, stepSeconds int) (VMMetricsResult, error) {
	prometheusURL := stringOptionValue(connection.Options, "prometheusUrl")
	if prometheusURL == "" {
		return VMMetricsResult{
			Message: "KubeVirt metrics require Prometheus integration - configure prometheusUrl in cluster connection options",
			Ready:   false,
			Source:  "none",
		}, nil
	}

	bearerToken := firstNonEmpty(
		stringOptionValue(connection.Credential, "prometheusBearerToken"),
		stringOptionValue(connection.Options, "prometheusBearerToken"),
	)
	namespace := vm.Namespace
	if namespace == "" {
		namespace = namespaceOrDefault(connection, "default")
	}

	queries := []struct {
		key   string
		label string
		unit  string
		query string
	}{
		{"cpu", "CPU Usage", "cores", kubeVirtVMISumRateQuery("kubevirt_vmi_cpu_usage_seconds_total", vm.Name, namespace, "5m")},
		{"memory", "Memory Usage", "bytes", kubeVirtVMIInstantQuery("kubevirt_vmi_memory_resident_bytes", vm.Name, namespace)},
		{"networkRx", "Network RX", "bytes/s", kubeVirtVMIRateQuery("kubevirt_vmi_network_receive_bytes_total", vm.Name, namespace, "5m")},
		{"networkTx", "Network TX", "bytes/s", kubeVirtVMIRateQuery("kubevirt_vmi_network_transmit_bytes_total", vm.Name, namespace, "5m")},
	}

	now := time.Now().UTC()
	start := now.Add(-time.Duration(rangeMinutes) * time.Minute)
	step := stepSeconds
	if step <= 0 {
		step = 60
	}

	var series []MetricSeries
	for _, q := range queries {
		points, err := queryPrometheusRange(ctx, prometheusURL, bearerToken, q.query, start, now, step)
		if err != nil || len(points) == 0 {
			continue
		}
		series = append(series, MetricSeries{Key: q.key, Label: q.label, Unit: q.unit, Points: points})
	}

	if len(series) == 0 {
		return VMMetricsResult{Message: "No metrics data available for this VM", Ready: false, Source: "prometheus"}, nil
	}
	return VMMetricsResult{Series: series, Ready: true, Source: "prometheus"}, nil
}

func kubeVirtVMIInstantQuery(metricName, vmName, namespace string) string {
	return fmt.Sprintf(`%s or %s`,
		kubeVirtVMISelector(metricName, "namespace", vmName, namespace),
		kubeVirtVMISelector(metricName, "exported_namespace", vmName, namespace),
	)
}

func kubeVirtVMIRateQuery(metricName, vmName, namespace, window string) string {
	return fmt.Sprintf(`%s or %s`,
		kubeVirtVMIRateSelector(metricName, "namespace", vmName, namespace, window),
		kubeVirtVMIRateSelector(metricName, "exported_namespace", vmName, namespace, window),
	)
}

func kubeVirtVMISumRateQuery(metricName, vmName, namespace, window string) string {
	return fmt.Sprintf(`sum(%s) or sum(%s)`,
		kubeVirtVMIRateSelector(metricName, "namespace", vmName, namespace, window),
		kubeVirtVMIRateSelector(metricName, "exported_namespace", vmName, namespace, window),
	)
}

func kubeVirtVMISelector(metricName, namespaceLabel, vmName, namespace string) string {
	return fmt.Sprintf(`%s{name=%s,%s=%s}`, metricName, strconv.Quote(vmName), namespaceLabel, strconv.Quote(namespace))
}

func kubeVirtVMIRateSelector(metricName, namespaceLabel, vmName, namespace, window string) string {
	return fmt.Sprintf(`rate(%s[%s])`, kubeVirtVMISelector(metricName, namespaceLabel, vmName, namespace), window)
}

func (a *KubeVirtAdapter) GetConsoleURL(ctx context.Context, connection Connection, vm VM) (ConsoleURLResult, error) {
	bundle, err := a.bundle(ctx, connection)
	if err != nil {
		return ConsoleURLResult{Message: err.Error(), Ready: false, Provider: "kubevirt", Type: "vnc", ProxyMode: "backend-ws-proxy"}, err
	}

	namespace := vm.Namespace
	if namespace == "" {
		namespace = namespaceOrDefault(connection, "default")
	}

	_, err = bundle.Dynamic.Resource(kubeVirtVMIGVR).
		Namespace(namespace).
		Get(ctx, vm.Name, metav1.GetOptions{})

	if err != nil {
		return ConsoleURLResult{Message: "VM instance not running", Ready: false, Provider: "kubevirt", Type: "vnc", ProxyMode: "backend-ws-proxy"}, nil
	}

	backendURL := firstNonEmpty(connection.BackendURL, connection.Endpoint)
	if backendURL == "" && bundle.RESTConfig != nil {
		backendURL = bundle.RESTConfig.Host
	}
	if strings.TrimSpace(backendURL) == "" {
		return ConsoleURLResult{Message: "cluster backend URL is required for kubevirt console", Ready: false, Provider: "kubevirt", Type: "vnc", ProxyMode: "backend-ws-proxy"}, nil
	}
	queryURL, err := url.Parse(strings.TrimRight(strings.TrimSpace(backendURL), "/"))
	if err != nil || queryURL.Scheme == "" || queryURL.Host == "" {
		return ConsoleURLResult{Message: "valid cluster backend URL is required for kubevirt console", Ready: false, Provider: "kubevirt", Type: "vnc", ProxyMode: "backend-ws-proxy"}, nil
	}
	queryURL.Path = strings.TrimRight(queryURL.Path, "/") + fmt.Sprintf("/apis/subresources.kubevirt.io/v1/namespaces/%s/virtualmachineinstances/%s/vnc", url.PathEscape(namespace), url.PathEscape(vm.Name))
	queryURL.RawPath = ""
	headers, err := kubeVirtConsoleHeaders(ctx, bundle.RESTConfig, queryURL.String())
	if err != nil {
		return ConsoleURLResult{Message: fmt.Sprintf("build kubevirt console auth headers: %v", err), Ready: false, Provider: "kubevirt", Type: "vnc", ProxyMode: "backend-ws-proxy"}, nil
	}
	tlsConfig, err := kubeVirtConsoleTLSConfig(bundle.RESTConfig)
	if err != nil {
		return ConsoleURLResult{Message: fmt.Sprintf("build kubevirt console TLS config: %v", err), Ready: false, Provider: "kubevirt", Type: "vnc", ProxyMode: "backend-ws-proxy"}, nil
	}
	vncURL := fmt.Sprintf("/api/v1/virtualization/vms/%s/console/vnc", vm.ID)

	return ConsoleURLResult{
		Type:           "vnc",
		URL:            vncURL,
		BackendURL:     queryURL.String(),
		Ready:          true,
		Provider:       "kubevirt",
		ProxyMode:      "backend-ws-proxy",
		BackendHeaders: headers,
		BackendTLS:     tlsConfig,
	}, nil
}

func kubeVirtConsoleHeaders(ctx context.Context, config *rest.Config, backendURL string) (http.Header, error) {
	if config == nil {
		return http.Header{}, nil
	}
	capture := &headerCaptureRoundTripper{}
	roundTripper, err := rest.HTTPWrappersForConfig(config, capture)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, backendURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := roundTripper.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	_ = resp.Body.Close()
	return capture.headers.Clone(), nil
}

type headerCaptureRoundTripper struct {
	headers http.Header
}

func (r *headerCaptureRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	r.headers = req.Header.Clone()
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader("")),
		Request:    req,
	}, nil
}

func kubeVirtConsoleTLSConfig(config *rest.Config) (BackendTLS, error) {
	if config == nil {
		return BackendTLS{}, nil
	}
	configCopy := rest.CopyConfig(config)
	if err := rest.LoadTLSFiles(configCopy); err != nil {
		return BackendTLS{}, err
	}
	transport := BackendTLS{
		ServerName:         configCopy.ServerName,
		InsecureSkipVerify: configCopy.Insecure,
		CAData:             append([]byte(nil), configCopy.CAData...),
		CertData:           append([]byte(nil), configCopy.CertData...),
		KeyData:            append([]byte(nil), configCopy.KeyData...),
		NextProtos:         append([]string(nil), configCopy.NextProtos...),
	}
	if _, err := ConsoleBackendTLSConfig(ConsoleURLResult{BackendTLS: transport}); err != nil {
		return BackendTLS{}, err
	}
	return transport, nil
}

var prometheusHTTPClient = &http.Client{Timeout: 10 * time.Second}

func queryPrometheusRange(ctx context.Context, endpoint, bearerToken, query string, start, end time.Time, stepSeconds int) ([]MetricPoint, error) {
	queryURL, err := url.Parse(strings.TrimRight(strings.TrimSpace(endpoint), "/") + "/api/v1/query_range")
	if err != nil {
		return nil, err
	}
	params := queryURL.Query()
	params.Set("query", query)
	params.Set("start", strconv.FormatInt(start.Unix(), 10))
	params.Set("end", strconv.FormatInt(end.Unix(), 10))
	params.Set("step", strconv.Itoa(stepSeconds))
	queryURL.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, queryURL.String(), nil)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(bearerToken) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(bearerToken))
	}

	resp, err := prometheusHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("prometheus query_range returned status %d", resp.StatusCode)
	}

	var payload struct {
		Data struct {
			Result []struct {
				Values [][]json.RawMessage `json:"values"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}

	var points []MetricPoint
	for _, result := range payload.Data.Result {
		for _, pair := range result.Values {
			if len(pair) < 2 {
				continue
			}
			var ts float64
			if err := json.Unmarshal(pair[0], &ts); err != nil {
				continue
			}
			var valStr string
			if err := json.Unmarshal(pair[1], &valStr); err != nil {
				continue
			}
			val, err := strconv.ParseFloat(valStr, 64)
			if err != nil {
				continue
			}
			points = append(points, MetricPoint{Timestamp: int64(ts), Value: val})
		}
	}
	return points, nil
}
