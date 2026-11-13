//go:build linux

package personalguest

import (
	"encoding/json"
	"strings"
	"testing"

	env "github.com/opensoha/soha-contracts/agent/environment"
	guest "github.com/opensoha/soha-contracts/agent/guest"
)

func TestRuntimeDockerRejectsUnboundedLogsAndRemoval(t *testing.T) {
	plan := guest.ToolsPlan{Binding: guest.Binding{DataRef: "22c55f65-d732-44b7-a047-10183444ca1f"}}
	op := guest.RuntimeOperation{Request: env.Request{Lease: env.Lease{Quota: env.Quota{CPUUnits: 1000, MemoryBytes: 512 << 20, MaxProcesses: 64}}}}
	config := map[string]any{"NetworkMode": "none", "CgroupParent": "soha-personal-" + strings.ReplaceAll(plan.Binding.DataRef, "-", "") + ".slice", "CgroupnsMode": "private", "ReadonlyRootfs": true, "CapDrop": []string{"ALL"}, "SecurityOpt": []string{"no-new-privileges:true"}, "NanoCpus": int64(1000000000), "Memory": int64(512 << 20), "MemorySwap": int64(512 << 20), "PidsLimit": 64, "LogConfig": map[string]any{"Type": "none", "Config": map[string]string{}}}
	observed := map[string]any{"Config": map[string]any{"User": "1000:1000"}, "HostConfig": config, "Mounts": []map[string]any{{"Source": runtimeWorkspace(plan), "Destination": "/data", "RW": true}}}
	check := func(wantValid bool) {
		t.Helper()
		raw, err := json.Marshal(observed)
		if err != nil {
			t.Fatal(err)
		}
		if valid := validateDockerInspection(string(raw), plan, op) == nil; valid != wantValid {
			t.Fatalf("policy validity %v, want %v", valid, wantValid)
		}
	}
	check(true)
	config["LogConfig"] = map[string]any{"Type": "json-file"}
	check(false)
	config["LogConfig"] = map[string]any{"Type": "none", "Config": map[string]string{}}
	config["AutoRemove"] = true
	check(false)
	config["AutoRemove"] = false
	observed["State"] = map[string]any{"Error": "process never started"}
	check(false)
}
