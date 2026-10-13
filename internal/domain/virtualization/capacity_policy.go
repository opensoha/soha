package virtualization

import (
	"fmt"
	"time"
)

type OvercommitPolicy struct {
	Enabled bool `json:"enabled"`
	Percent int  `json:"percent"`
}

type CapacityPolicy struct {
	SourceConfirmed bool             `json:"sourceConfirmed"`
	SourceID        string           `json:"sourceId"`
	Revision        int              `json:"revision"`
	Mode            string           `json:"mode"`
	CPU             OvercommitPolicy `json:"cpu"`
	Memory          OvercommitPolicy `json:"memory"`
	UpdatedBy       string           `json:"updatedBy,omitempty"`
	UpdatedAt       *time.Time       `json:"updatedAt,omitempty"`
}

func DefaultCapacityPolicy(source string) CapacityPolicy {
	return CapacityPolicy{SourceID: source, Mode: "unconfigured", CPU: OvercommitPolicy{Percent: 100}, Memory: OvercommitPolicy{Percent: 100}}
}

func (p CapacityPolicy) Validate() error {
	if p.Mode != "observing" && p.Mode != "enforced" {
		return fmt.Errorf("policy mode must be observing or enforced")
	}
	if p.CPU.Percent < 100 || p.CPU.Percent > 800 || p.Memory.Percent < 100 || p.Memory.Percent > 150 {
		return fmt.Errorf("CPU ratio must be 100–800%% and memory ratio 100–150%%")
	}
	if !p.CPU.Enabled && p.CPU.Percent != 100 || !p.Memory.Enabled && p.Memory.Percent != 100 {
		return fmt.Errorf("disabled overcommit requires a 100%% ratio")
	}
	return nil
}

type CapacityCheckDiagnostic struct {
	Check     string `json:"check"`
	Dimension string `json:"dimension"`
	Status    string `json:"status"`
	Limit     int64  `json:"limit"`
	Committed int64  `json:"committed"`
	Pending   int64  `json:"pending"`
	Demand    int64  `json:"demand"`
	Available int64  `json:"available"`
	Reason    string `json:"reason"`
}

type CapacityNodeDiagnostic struct {
	Node    string                    `json:"node"`
	Storage string                    `json:"storage,omitempty"`
	Checks  []CapacityCheckDiagnostic `json:"checks"`
}

func CapacityCheck(check, dimension string, limit, committed, pending, demand int64) CapacityCheckDiagnostic {
	available := max(int64(0), limit-committed-pending)
	result := CapacityCheckDiagnostic{Check: check, Dimension: dimension, Status: "available", Limit: limit, Committed: committed, Pending: pending, Demand: demand, Available: available, Reason: "fits"}
	// Existing over-limit resources can receive a non-increasing change.
	if demand > 0 && demand > available {
		result.Status, result.Reason = "unavailable", "insufficient "+check+" "+dimension
	}
	return result
}
