package virtualization

import (
	"context"
	"encoding/json"
	guest "github.com/opensoha/soha-contracts/agent/guest"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestPVEPersonalGuestFixedDispatchAndDecodedReceipt(t *testing.T) {
	now := time.Now().UTC()
	id := "11111111-1111-4111-8111-111111111111"
	binding := guest.Binding{SchemaVersion: guest.BindingVersion, ID: id, Version: 1, AssistantID: id, OwnerID: id, Provider: "pve", ComputeRef: id, ComputeGeneration: 1, ProviderIdentityHash: "sha256:" + strings.Repeat("a", 64), DataRef: id, DataDiskSerial: "data01", DataDiskBytes: 8 << 30, PolicyRevision: "sha256:" + strings.Repeat("b", 64), IssuedAt: now, ExpiresAt: now.Add(time.Hour)}
	bundle := guest.Provisioning{Plan: guest.DataPlan{SchemaVersion: guest.DataPlanVersion, OperationID: id, Binding: binding, DiskQuotaBytes: 6 << 30}, Endpoint: "https://192.0.2.1:8444", EnrollmentID: id, Challenge: strings.Repeat("A", 43), CertificatePEM: "public-test-cert", ClientCAPEM: "public-test-ca", ArtifactSHA256: strings.Repeat("c", 64), CPUQuotaMilli: 1000, MemoryBytes: 2 << 30, PidsLimit: 64}
	result := guest.DataResult{DataRef: id, FilesystemID: id, DiskQuota: 6 << 30, DiskUsed: 4096}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/api2/json/nodes/test-node/qemu/51049/agent/exec" && r.URL.Path != "/api2/json/nodes/test-node/qemu/51049/agent/exec-status" {
			t.Error("request escaped the owned VM")
			w.WriteHeader(400)
			return
		}
		if r.Method == http.MethodPost {
			var input struct {
				Command []string `json:"command"`
				Input   string   `json:"input-data"`
			}
			if json.NewDecoder(r.Body).Decode(&input) != nil || !reflect.DeepEqual(input.Command, []string{"/usr/bin/nsenter", "--mount=/proc/1/ns/mnt", "--", "/usr/bin/python3", "-c", personalGuestInstaller}) {
				t.Error("unfixed privileged command")
				w.WriteHeader(400)
				return
			}
			var decoded guest.Provisioning
			if json.Unmarshal([]byte(input.Input), &decoded) != nil || decoded != bundle {
				t.Error("bootstrap changed")
			}
			_, _ = w.Write([]byte(`{"data":{"pid":49}}`))
			return
		}
		if r.URL.Query().Get("pid") != "49" {
			t.Error("provider PID changed")
		}
		raw, _ := json.Marshal(result)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"exited": 1, "exitcode": 0, "out-truncated": 0, "out-data": string(raw)}})
	}))
	defer server.Close()
	adapter := NewPVEAdapter(server.Client())
	actual, err := adapter.ProvisionPersonalGuest(context.Background(), Connection{Endpoint: server.URL}, VM{Node: "test-node", ID: "51049"}, bundle)
	if err != nil || actual != result || calls != 2 {
		t.Fatal("PVE decoded output was rejected", err)
	}
}

func TestPersonalGuestFailureStageNeverExposesProviderOutput(t *testing.T) {
	const safe = "managed personal guest provisioning failed stage=runtime_scope; retain the original operation\n"
	for _, test := range []struct {
		output    string
		truncated int
		want      string
	}{
		{safe, 0, "runtime_scope"}, {safe, 1, "unknown"}, {safe + "private output", 0, "unknown"},
		{"managed personal guest provisioning failed stage=private-token; retain the original operation", 0, "unknown"},
		{strings.Repeat("x", 513), 0, "unknown"},
	} {
		if got := personalGuestFailureStage(test.output, test.truncated); got != test.want {
			t.Fatalf("unsafe diagnostic stage: %q", got)
		}
	}
}
