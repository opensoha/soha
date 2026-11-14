package environment

import (
	"encoding/json"
	"testing"
	"time"
)

func TestPreflightRejectsCallerSelectedResourcesAndMismatchedQuotaReceipt(t *testing.T) {
	request := PreflightRequest{SchemaVersion: PreflightRequestVersion, ID: "00000000-0000-4000-8000-000000000048", Quota: Quota{DiskBytes: 32 << 20, MemoryBytes: 64 << 20, CPUUnits: 250, MaxProcesses: 16}}
	payload, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	var decoded PreflightRequest
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"ownerId", "nodeUrl", "command", "image"} {
		var fields map[string]any
		if err := json.Unmarshal(payload, &fields); err != nil {
			t.Fatal(err)
		}
		fields[key] = "caller-selected"
		invalid, err := json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		if json.Unmarshal(invalid, &decoded) == nil {
			t.Fatalf("accepted %s", key)
		}
	}
	result := PreflightResult{SchemaVersion: PreflightResultVersion, ID: request.ID, Quota: request.Quota, Ready: true, CgroupVersion: "2", DiskMechanism: "btrfs-qgroup", ObservedAt: time.Now().UTC()}
	if err := result.ValidateFor(request); err != nil {
		t.Fatal(err)
	}
	result.Quota.DiskBytes++
	if result.ValidateFor(request) == nil {
		t.Fatal("different quota acknowledged")
	}
}
