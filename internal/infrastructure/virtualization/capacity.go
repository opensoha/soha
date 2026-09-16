package virtualization

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	domain "github.com/opensoha/soha/internal/domain/virtualization"
)

func capacityInteger(value any) (int64, error) {
	parsed, err := strconv.ParseInt(stringFromAny(value), 10, 64)
	if err != nil || parsed < 0 || parsed > 1<<60 {
		return 0, fmt.Errorf("provider capacity value is missing or invalid")
	}
	return parsed, nil
}

func (a *PVEAdapter) ObserveCapacity(ctx context.Context, connection Connection, input CreateVMInput) (domain.CapacitySnapshot, error) {
	snapshot := domain.CapacitySnapshot{SourceID: domain.CapacitySourceID(connection), ObservedAt: time.Now().UTC(), Nodes: []domain.CapacityNode{}, Storage: []domain.CapacityStorage{}}
	if err := a.checkPVECloneCapacity(ctx, connection, input); err != nil {
		return snapshot, err
	}
	// A filtered PVE inventory cannot establish available allocation capacity.
	var permissions struct {
		Data map[string]map[string]any `json:"data"`
	}
	if err := a.do(ctx, connection, http.MethodGet, "/access/permissions", nil, &permissions); err != nil {
		return snapshot, err
	}
	for _, permission := range []string{"Sys.Audit", "VM.Audit", "Datastore.Audit"} {
		if !boolFromAny(permissions.Data["/"][permission]) {
			return snapshot, fmt.Errorf("capacity requires inherited %s visibility over the whole provider inventory", permission)
		}
	}
	var resources pveDataEnvelope
	if err := a.do(ctx, connection, http.MethodGet, "/cluster/resources", nil, &resources); err != nil {
		return snapshot, err
	}
	overhead, err := domain.CapacityMemoryOverheadMiB(connection.Options)
	if err != nil {
		return snapshot, err
	}
	nodes, err := pveCapacityNodes(resources.Data, overhead)
	if err != nil {
		return snapshot, err
	}
	snapshot.Nodes = nodes
	storageByKey := map[string]domain.CapacityStorage{}
	for _, node := range nodes {
		items, err := a.observePVEStorageCapacity(ctx, connection, node.Name)
		if err != nil {
			return snapshot, err
		}
		for _, item := range items {
			if previous, exists := storageByKey[item.Key]; exists {
				item.AvailableGiB = min(item.AvailableGiB, previous.AvailableGiB)
				item.Nodes = append(previous.Nodes, item.Nodes...)
			}
			storageByKey[item.Key] = item
		}
	}
	for _, item := range storageByKey {
		snapshot.Storage = append(snapshot.Storage, item)
	}
	slices.SortFunc(snapshot.Storage, func(a, b domain.CapacityStorage) int { return strings.Compare(a.Key, b.Key) })
	for _, vm := range resources.Data {
		if stringFromAny(vm["type"]) != "qemu" {
			continue
		}
		config, err := a.pveVMConfig(ctx, connection, stringFromAny(vm["node"]), stringFromAny(vm["vmid"]))
		if err != nil {
			return snapshot, err
		}
		if owner, ok := strings.CutPrefix(stringFromAny(config["description"]), "soha-operation:"); ok && owner != "" && stringFromAny(config["lock"]) == "" {
			snapshot.ObservedOperations = append(snapshot.ObservedOperations, owner)
		}
	}
	snapshot.Complete = true
	return snapshot, nil
}

func (a *PVEAdapter) checkPVECloneCapacity(ctx context.Context, connection Connection, input CreateVMInput) error {
	if input.SourceMode != "template_clone" && input.SourceMode != "vm_clone" && input.TemplateID == "" {
		return nil
	}
	source := firstNonEmpty(input.TemplateID, input.SourceRef, input.BootImage)
	if input.Node == "" || source == "" || stringFromAny(input.ProviderParams["storage"]) == "" {
		return invalidf("reserved clones require an explicit source node and destination storage")
	}
	config, err := a.pveVMConfig(ctx, connection, input.Node, source)
	if err != nil {
		return err
	}
	return validatePVEReservedClone(config, pveDiskSizeGiB(input.DiskSize))
}

func validatePVEReservedClone(config map[string]any, diskGiB int) error {
	root := pveDiskSizeGiB(stringFromAny(config["scsi0"]))
	if stringFromAny(config["lock"]) != "" || root <= 0 || root > diskGiB {
		return invalidf("reserved clones require an unlocked scsi0 root disk no larger than the requested disk")
	}
	for key, raw := range config {
		value := stringFromAny(raw)
		if key == "scsi0" || key == "ide2" && pveCloudInitDrive(value) {
			continue
		}
		if isPVEDiskID(key) || pveUnreservedDevice(key) {
			return invalidf("reserved clones support only a scsi0 root disk and an optional ide2 cloud-init drive; unreserved device: %s", key)
		}
	}
	return nil
}

func pveCloudInitDrive(value string) bool {
	volume := strings.Split(value, ",")[0]
	return strings.HasSuffix(volume, "-cloudinit") && pveConfigOption(value, "media") == "cdrom" && pveDiskSizeGiB(value) <= 1
}

func pveUnreservedDevice(key string) bool {
	for _, prefix := range []string{"efidisk", "tpmstate", "unused", "hostpci", "usb", "virtiofs"} {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return key == "args" || key == "hookscript"
}

func pveCapacityNodes(resources []map[string]any, overheadMiB int64) ([]domain.CapacityNode, error) {
	nodes := []domain.CapacityNode{}
	for _, item := range resources {
		if stringFromAny(item["type"]) != "node" || stringFromAny(item["status"]) != "online" {
			continue
		}
		cpu, err := capacityInteger(item["maxcpu"])
		if err != nil {
			return nil, err
		}
		memory, err := capacityInteger(item["maxmem"])
		if err != nil {
			return nil, err
		}
		usedMemory, err := capacityInteger(item["mem"])
		if err != nil {
			return nil, err
		}
		name := stringFromAny(item["node"])
		committedCPU, committedMemory, err := pveNodeCommitments(resources, name, overheadMiB)
		if err != nil {
			return nil, err
		}
		// Reserve current host use and all configured guest memory, including
		// stopped guests. No CPU or memory overcommit is inferred from idle load.
		nodes = append(nodes, domain.CapacityNode{Name: name, CPU: max(0, cpu-committedCPU), MemoryMiB: max(0, min(memory-usedMemory, memory-committedMemory)) / (1 << 20)})
	}
	slices.SortFunc(nodes, func(a, b domain.CapacityNode) int { return strings.Compare(a.Name, b.Name) })
	return nodes, nil
}

func pveNodeCommitments(resources []map[string]any, node string, overheadMiB int64) (int64, int64, error) {
	var cpu, memory int64
	for _, item := range resources {
		kind := stringFromAny(item["type"])
		if stringFromAny(item["node"]) != node || kind != "qemu" && kind != "lxc" || boolFromAny(item["template"]) {
			continue
		}
		cores, err := capacityInteger(item["maxcpu"])
		if err != nil {
			return 0, 0, err
		}
		bytes, err := capacityInteger(item["maxmem"])
		if err != nil {
			return 0, 0, err
		}
		cpu += cores
		memory += bytes
		if kind == "qemu" {
			memory += overheadMiB * (1 << 20)
		}
	}
	return cpu, memory, nil
}

func (a *PVEAdapter) observePVEStorageCapacity(ctx context.Context, connection Connection, node string) ([]domain.CapacityStorage, error) {
	var storages pveDataEnvelope
	endpoint := "/nodes/" + url.PathEscape(node) + "/storage"
	if err := a.do(ctx, connection, http.MethodGet, endpoint, nil, &storages); err != nil {
		return nil, err
	}
	result := []domain.CapacityStorage{}
	for _, storage := range storages.Data {
		if !boolFromAny(storage["active"]) || !boolFromAny(storage["enabled"]) || !pveStorageContentSet(storage)["images"] {
			continue
		}
		name := stringFromAny(storage["storage"])
		available, err := capacityInteger(storage["avail"])
		if err != nil {
			return nil, err
		}
		total, err := capacityInteger(storage["total"])
		if err != nil {
			return nil, err
		}
		var volumes pveDataEnvelope
		if err := a.do(ctx, connection, http.MethodGet, endpoint+"/"+url.PathEscape(name)+"/content", nil, &volumes); err != nil {
			return nil, err
		}
		var committed int64
		for _, volume := range volumes.Data {
			size, err := capacityInteger(volume["size"])
			if err != nil {
				return nil, err
			}
			committed += size
		}
		key := "node/" + node + "/" + name
		if boolFromAny(storage["shared"]) {
			key = "shared/" + name
		}
		result = append(result, domain.CapacityStorage{Key: key, Name: name, Nodes: []string{node}, AvailableGiB: max(0, min(available, total-committed)) / (1 << 30)})
	}
	return result, nil
}
