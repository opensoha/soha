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

// Capacity is measured from provider allocation records, not CPU utilization.
// Pending Soha reservations are deducted atomically when a task is admitted.
type CapacitySnapshot struct {
	Namespace          string            `json:"namespace,omitempty"`
	QuotaCPU           *int64            `json:"quotaCpu,omitempty"`
	QuotaMemoryMiB     *int64            `json:"quotaMemoryMiB,omitempty"`
	QuotaDiskGiB       *int64            `json:"quotaDiskGiB,omitempty"`
	SourceID           string            `json:"sourceId"`
	ObservedAt         time.Time         `json:"observedAt"`
	Complete           bool              `json:"complete"`
	Reason             string            `json:"reason,omitempty"`
	Nodes              []CapacityNode    `json:"nodes"`
	Storage            []CapacityStorage `json:"storage"`
	ObservedOperations []string          `json:"-"`
}

type CapacityNode struct {
	Name      string `json:"name"`
	CPU       int64  `json:"cpu"`
	MemoryMiB int64  `json:"memoryMiB"`
}

type CapacityStorage struct {
	Key          string   `json:"key"`
	Name         string   `json:"name"`
	Nodes        []string `json:"nodes"`
	AvailableGiB int64    `json:"availableGiB"`
}

type CapacityDemand struct {
	Node      string
	Storage   string
	CPU       int64
	MemoryMiB int64
	DiskGiB   int64
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
