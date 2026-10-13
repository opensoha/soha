package virtualization

import (
	"context"
	"fmt"
	"math"
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
	connection, err := a.authenticatedConnection(ctx, connection)
	if err != nil {
		return snapshot, err
	}
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
	nodes, err := pveCapacityNodes(resources.Data)
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
	if err := a.observePVEConfiguredCapacity(ctx, connection, resources.Data, &snapshot); err != nil {
		return snapshot, err
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

func pveCapacityNodes(resources []map[string]any) ([]domain.CapacityNode, error) {
	nodes := []domain.CapacityNode{}
	for _, item := range resources {
		if stringFromAny(item["type"]) != "node" || stringFromAny(item["status"]) != "online" {
			continue
		}
		cpu, err := capacityInteger(item["maxcpu"])
		if err != nil || cpu == 0 || cpu > 65536 {
			return nil, fmt.Errorf("provider node CPU capacity is missing or invalid")
		}
		utilization, err := strconv.ParseFloat(fmt.Sprint(item["cpu"]), 64)
		if err != nil || math.IsNaN(utilization) || math.IsInf(utilization, 0) || utilization < 0 || utilization > 1 {
			return nil, fmt.Errorf("provider node CPU utilization is missing or invalid")
		}
		memory, err := capacityInteger(item["maxmem"])
		if err != nil || memory == 0 {
			return nil, fmt.Errorf("provider node memory capacity is missing or invalid")
		}
		usedMemory, err := capacityInteger(item["mem"])
		if err != nil || usedMemory > memory {
			return nil, fmt.Errorf("provider node memory use is missing or invalid")
		}
		name := stringFromAny(item["node"])
		if name == "" {
			return nil, fmt.Errorf("provider node name is missing")
		}
		// PVE mem is total minus MemAvailable. Keep 5% (at least 2 GiB) for host pressure and sampling lag.
		reserve := max(int64(2<<30), memory/20)
		nodes = append(nodes, domain.CapacityNode{Name: name, CPU: int64(math.Floor(float64(cpu) * (1 - utilization))), MemoryMiB: max(0, memory-usedMemory-reserve) / (1 << 20), LogicalCPU: cpu, TotalMemoryMiB: memory / (1 << 20), HostReserveMiB: reserve / (1 << 20), BudgetObserved: true})
	}
	slices.SortFunc(nodes, func(a, b domain.CapacityNode) int { return strings.Compare(a.Name, b.Name) })
	return nodes, nil
}

func pveVMCapacityObserved(vm map[string]any) bool {
	// A stopped/just-started VM has not yet appeared in the host utilization sample.
	uptime, uptimeErr := capacityInteger(vm["uptime"])
	memory, memoryErr := capacityInteger(vm["mem"])
	return stringFromAny(vm["status"]) == "running" && uptimeErr == nil && uptime >= 30 && memoryErr == nil && memory > 0
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
