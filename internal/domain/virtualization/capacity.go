package virtualization

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var ErrCapacityUnknown = errors.New("provider capacity cannot be established")
var ErrProviderIdentityClaimed = errors.New("provider VM identity is reserved by another task")

// Capacity and provider identity claims use the same operator-owned source key.
func CapacitySourceID(connection AdapterConnection) string {
	if configured, ok := connection.Options["capacitySourceId"].(string); ok && strings.TrimSpace(configured) != "" {
		return connection.Provider + "/" + strings.TrimSpace(configured)
	}
	if connection.ClusterID != "" {
		return "kubernetes/" + connection.ClusterID
	}
	return "pve/" + strings.TrimRight(strings.ToLower(connection.Endpoint), "/")
}

// Capacity uses provider semantics: PVE runtime headroom, Kubernetes allocatable requests.
// Pending Soha reservations are deducted atomically; a snapshot is not a runtime guarantee.
type CapacitySnapshot struct {
	ConnectionID          string                  `json:"connectionId"`
	HypervisorOverheadMiB int64                   `json:"hypervisorOverheadMiB"`
	Policy                CapacityPolicy          `json:"policy"`
	Namespace             string                  `json:"namespace,omitempty"`
	QuotaCPU              *int64                  `json:"quotaCpu,omitempty"`
	QuotaMemoryMiB        *int64                  `json:"quotaMemoryMiB,omitempty"`
	QuotaDiskGiB          *int64                  `json:"quotaDiskGiB,omitempty"`
	SourceID              string                  `json:"sourceId"`
	ObservedAt            time.Time               `json:"observedAt"`
	Complete              bool                    `json:"complete"`
	Reason                string                  `json:"reason,omitempty"`
	Nodes                 []CapacityNode          `json:"nodes"`
	Storage               []CapacityStorage       `json:"storage"`
	ObservedOperations    []string                `json:"-"`
	ObservedVMs           []CapacityVMObservation `json:"-"`
	VMs                   []CapacityVMAllocation  `json:"-"`
}

type CapacityVMAllocation struct {
	Generation      string
	RuntimeObserved bool
	ProviderVMID    string
	Name            string
	Node            string
	Status          string
	CPU             int64
	MemoryMiB       int64
	RootDiskGiB     int64
	RootStorage     string
	Disks           []CapacityDiskAllocation
}

type CapacityDiskAllocation struct {
	ID, Storage, Serial string
	ProviderRef         string
	SizeGiB             int64
}

func (d CapacityDiskAllocation) MatchesNewDisk(change AdapterDiskChange) bool {
	return change.Add && change.Serial != "" && d.ID == change.ID && d.Storage == change.Storage && d.Serial == change.Serial && d.SizeGiB == int64(change.SizeGiB)
}

type CapacityVMObservation struct {
	Generation      string
	OperationID     string
	ProviderVMID    string
	Node            string
	Name            string
	Resident        bool
	RuntimeObserved bool
	Status          string
}

type CapacityNode struct {
	Name                string `json:"name"`
	CPU                 int64  `json:"cpu"`
	MemoryMiB           int64  `json:"memoryMiB"`
	LogicalCPU          int64  `json:"logicalCpu"`
	TotalMemoryMiB      int64  `json:"totalMemoryMiB"`
	HostReserveMiB      int64  `json:"hostReserveMiB"`
	ConfiguredCPU       int64  `json:"configuredCpu"`
	ConfiguredMemoryMiB int64  `json:"configuredMemoryMiB"`
	BudgetObserved      bool   `json:"budgetObserved"`
}

type CapacityStorage struct {
	Key          string   `json:"key"`
	Name         string   `json:"name"`
	Nodes        []string `json:"nodes"`
	AvailableGiB int64    `json:"availableGiB"`
}

type CapacityDemand struct {
	Node        string
	Storage     string
	CPU         int64
	MemoryMiB   int64
	DiskGiB     int64
	StorageOnly bool
}

type CapacityProvider interface {
	ObserveCapacity(context.Context, AdapterConnection, AdapterCreateVMInput) (CapacitySnapshot, error)
}

// CapacityMemoryOverheadMiB leaves a configurable allowance for hypervisor
// processes and basic guest devices; it is not a provider scheduling guarantee.
func CapacityMemoryOverheadMiB(options map[string]any) (int64, error) {
	value, exists := options["capacityMemoryOverheadMiB"]
	if !exists {
		return 512, nil
	}
	parsed, err := strconv.ParseInt(fmt.Sprint(value), 10, 64)
	if err != nil || parsed < 0 || parsed > 65536 {
		return 0, fmt.Errorf("capacityMemoryOverheadMiB must be between 0 and 65536")
	}
	return parsed, nil
}
