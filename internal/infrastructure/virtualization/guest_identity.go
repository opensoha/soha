package virtualization

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	domain "github.com/opensoha/soha/internal/domain/virtualization"
	"github.com/opensoha/soha/internal/platform/apperrors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (a *PVEAdapter) ValidateGuestBootstrap(ctx context.Context, connection Connection, input CreateVMInput) (domain.VMIdentity, error) {
	if input.SourceMode == "cloud_image" {
		return a.inspectPVECloudImage(ctx, connection, input)
	}
	if input.SourceMode != "template_clone" && input.SourceMode != "vm_clone" {
		return domain.VMIdentity{}, fmt.Errorf("%w: managed PVE bootstrap requires an inspected cloud-init clone source", apperrors.ErrConflict)
	}
	node := firstNonEmpty(input.Node, stringOptionValue(connection.Options, "defaultNode"))
	source := firstNonEmpty(input.TemplateID, input.SourceRef, input.BootImage)
	if node == "" || source == "" {
		return domain.VMIdentity{}, fmt.Errorf("%w: cloud-init source node is unknown", apperrors.ErrConflict)
	}
	config, err := a.pveVMConfig(ctx, connection, node, source)
	if err != nil {
		return domain.VMIdentity{}, fmt.Errorf("%w: cloud-init source could not be inspected", apperrors.ErrConflict)
	}
	for id, value := range config {
		if isPVEDiskID(id) && strings.Contains(stringFromAny(value), "cloudinit") {
			generation := pveVMGeneration(config)
			if generation == "" {
				return domain.VMIdentity{}, fmt.Errorf("%w: cloud-init source generation is unknown", apperrors.ErrConflict)
			}
			return domain.VMIdentity{ProviderVMID: source, Name: stringFromAny(config["name"]), Node: node, Generation: generation}, nil
		}
	}
	return domain.VMIdentity{}, fmt.Errorf("%w: clone source has no cloud-init drive", apperrors.ErrConflict)
}

func (a *PVEAdapter) ObserveVMIdentity(ctx context.Context, connection Connection, vm VM) (domain.VMIdentity, error) {
	config, err := a.pveVMConfig(ctx, connection, vm.Node, vm.ID)
	if err != nil {
		return domain.VMIdentity{}, err
	}
	generation := pveVMGeneration(config)
	current, err := a.fetchVM(ctx, connection, vm.Node, vm.ID, vm.Name)
	if err != nil {
		return domain.VMIdentity{}, err
	}
	ips := a.fetchPVEGuestAgentIPs(ctx, connection, vm.Node, vm.ID)
	after, err := a.pveVMConfig(ctx, connection, vm.Node, vm.ID)
	if err != nil {
		return domain.VMIdentity{}, err
	}
	if generation == "" || generation != pveVMGeneration(after) {
		return domain.VMIdentity{}, fmt.Errorf("%w: stable provider generation is unavailable", apperrors.ErrConflict)
	}
	return domain.VMIdentity{ProviderVMID: vm.ID, Name: firstNonEmpty(stringFromAny(after["name"]), current.Name), Node: vm.Node, Generation: generation, Status: current.Status, IPAddresses: ips, DataDisks: pvePersonalDataDisks(after)}, nil
}

// Unknown boot order cannot prove that a serial belongs to a separate data disk.
func pvePersonalDataDisks(config map[string]any) []domain.CapacityDiskAllocation {
	order := pveConfigOption(stringFromAny(config["boot"]), "order")
	if order == "" {
		return nil
	}
	boot := strings.Split(order, ";")
	var bootVolumes []string
	for _, id := range boot {
		if isPVEDiskID(id) {
			if value := stringFromAny(config[id]); value != "" {
				bootVolumes = append(bootVolumes, strings.Split(value, ",")[0])
			}
		}
	}
	if len(bootVolumes) == 0 {
		return nil
	}
	var disks []domain.CapacityDiskAllocation
	for id, raw := range config {
		value := stringFromAny(raw)
		serial := pveConfigOption(value, "serial")
		volume := strings.Split(value, ",")[0]
		if !isPVEDiskID(id) || slices.Contains(bootVolumes, volume) || serial == "" || pveConfigOption(value, "media") == "cdrom" || strings.Contains(volume, "cloudinit") {
			continue
		}
		storage, _ := pveStorageFromVolume(value)
		disks = append(disks, domain.CapacityDiskAllocation{ID: id, Storage: storage, Serial: serial, ProviderRef: volume, SizeGiB: pvePersonalDataSizeGiB(value)})
	}
	return disks
}

// Capacity summaries may round up; frozen data identity requires exact whole GiB.
func pvePersonalDataSizeGiB(value string) int64 {
	size := pveConfigOption(value, "size")
	var divisor int64
	switch {
	case strings.HasSuffix(size, "G"):
		divisor = 1
	case strings.HasSuffix(size, "M"):
		divisor = 1024
	default:
		return 0
	}
	number, err := strconv.ParseInt(size[:len(size)-1], 10, 32)
	if err != nil || number <= 0 || number%divisor != 0 {
		return 0
	}
	return number / divisor
}

func (a *KubeVirtAdapter) ObserveVMIdentity(ctx context.Context, connection Connection, vm VM) (domain.VMIdentity, error) {
	bundle, err := a.bundle(ctx, connection)
	if err != nil {
		return domain.VMIdentity{}, err
	}
	namespace := firstNonEmpty(vm.Namespace, namespaceOrDefault(connection, "default"))
	object, err := bundle.Dynamic.Resource(kubeVirtVMGVR).Namespace(namespace).Get(ctx, vm.Name, metav1.GetOptions{})
	if err != nil {
		return domain.VMIdentity{}, err
	}
	if object.GetDeletionTimestamp() != nil {
		return domain.VMIdentity{}, apperrors.ErrConflict
	}
	identity := domain.VMIdentity{ProviderVMID: string(object.GetUID()), Name: object.GetName(), Generation: string(object.GetUID()), Status: readStatus(object)}
	vmi, err := bundle.Dynamic.Resource(kubeVirtVMIGVR).Namespace(namespace).Get(ctx, vm.Name, metav1.GetOptions{})
	if err != nil {
		return identity, err
	}
	owned := false
	for _, owner := range vmi.GetOwnerReferences() {
		if owner.UID == object.GetUID() {
			owned = true
		}
	}
	if !owned || vmi.GetDeletionTimestamp() != nil {
		return identity, apperrors.ErrConflict
	}
	identity.Node = nestedString(vmi.Object, "status", "nodeName")
	identity.IPAddresses = vmiIPAddresses(vmi)
	if nestedString(vmi.Object, "status", "phase") == "Running" {
		identity.Status = "running"
	}
	return identity, nil
}
