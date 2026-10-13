package virtualization

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	domain "github.com/opensoha/soha/internal/domain/virtualization"
)

func (a *PVEAdapter) observePVEConfiguredCapacity(ctx context.Context, connection Connection, resources []map[string]any, snapshot *domain.CapacitySnapshot) error {
	overhead, err := domain.CapacityMemoryOverheadMiB(connection.Options)
	if err != nil {
		return err
	}
	snapshot.HypervisorOverheadMiB = overhead
	for _, vm := range resources {
		kind := stringFromAny(vm["type"])
		if kind != "qemu" && kind != "lxc" {
			continue
		}
		node, id := stringFromAny(vm["node"]), stringFromAny(vm["vmid"])
		var config map[string]any
		if kind == "qemu" {
			config, err = a.pveVMConfig(ctx, connection, node, id)
		} else {
			var response struct {
				Data map[string]any `json:"data"`
			}
			err = a.do(ctx, connection, http.MethodGet, "/nodes/"+url.PathEscape(node)+"/lxc/"+url.PathEscape(id)+"/config", nil, &response)
			config = response.Data
		}
		if err != nil {
			return err
		}
		status := stringFromAny(vm["status"])
		resident := status == "running" || status == "paused"
		logicalCPU := int64(0)
		for _, n := range snapshot.Nodes {
			if n.Name == node {
				logicalCPU = n.LogicalCPU
			}
		}
		cpu, memory, err := pveConfiguredDemand(kind, config, logicalCPU)
		if err != nil {
			return err
		}
		root := stringFromAny(config["scsi0"])
		rootStorage, _, _ := strings.Cut(root, ":")
		snapshot.VMs = append(snapshot.VMs, domain.CapacityVMAllocation{Generation: pveVMGeneration(config), RuntimeObserved: pveVMCapacityObserved(vm), ProviderVMID: id, Name: stringFromAny(config["name"]), Node: node, Status: status, CPU: cpu, MemoryMiB: memory, RootDiskGiB: int64(pveDiskSizeGiB(root)), RootStorage: rootStorage, Disks: pveCapacityDisks(config)})
		if resident {
			for i := range snapshot.Nodes {
				if snapshot.Nodes[i].Name != node {
					continue
				}
				snapshot.Nodes[i].ConfiguredCPU += cpu
				snapshot.Nodes[i].ConfiguredMemoryMiB += memory
				if kind == "qemu" {
					snapshot.Nodes[i].ConfiguredMemoryMiB += overhead
				}
			}
		}
		if owner, ok := strings.CutPrefix(stringFromAny(config["description"]), "soha-operation:"); ok && owner != "" && stringFromAny(config["lock"]) == "" {
			observed := domain.CapacityVMObservation{Generation: pveVMGeneration(config), OperationID: owner, ProviderVMID: id, Node: node, Name: stringFromAny(config["name"]), Resident: resident, RuntimeObserved: pveVMCapacityObserved(vm), Status: status}
			snapshot.ObservedVMs = append(snapshot.ObservedVMs, observed)
			if observed.RuntimeObserved {
				snapshot.ObservedOperations = append(snapshot.ObservedOperations, owner)
			}
		}
	}
	return nil
}

func pveCapacityDisks(config map[string]any) []domain.CapacityDiskAllocation {
	disks := []domain.CapacityDiskAllocation{}
	for id, raw := range config {
		if !isPVEDiskID(id) {
			continue
		}
		value := stringFromAny(raw)
		storage, _ := pveStorageFromVolume(value)
		disks = append(disks, domain.CapacityDiskAllocation{ID: id, Storage: storage, Serial: pveConfigOption(value, "serial"), SizeGiB: int64(pveDiskSizeGiB(value))})
	}
	return disks
}

func pveConfiguredDemand(kind string, config map[string]any, logicalCPU int64) (int64, int64, error) {
	read := func(key string, fallback int64) (int64, error) {
		if config[key] == nil {
			return fallback, nil
		}
		return capacityInteger(config[key])
	}
	cores, err := read("cores", 1)
	if err != nil {
		return 0, 0, err
	}
	if kind == "lxc" && (config["cores"] == nil || cores == 0) {
		cores = logicalCPU
	}
	sockets := int64(1)
	if kind == "qemu" {
		sockets, err = read("sockets", 1)
	}
	if err != nil || cores < 1 || sockets < 1 || cores > 65536 || sockets > 65536 || cores*sockets > 65536 {
		return 0, 0, fmt.Errorf("provider configured CPU is invalid")
	}
	memory, err := read("memory", 512)
	if err != nil || memory < 1 || memory > 1<<40 {
		return 0, 0, fmt.Errorf("provider configured memory is invalid")
	}
	return cores * sockets, memory, nil
}
