package copilot

import (
	"encoding/json"
	"testing"
	"time"

	gateway "github.com/opensoha/soha/internal/application/aigateway"
	domain "github.com/opensoha/soha/internal/domain/copilot"
)

func TestDSHPersonalStdioDispatch(t *testing.T) {
	ref := domain.RuntimeAssetRef{Kind: "mcp", ID: "owner-tool", Version: "1"}
	cp := dshCheckpoint{
		Start:            &domain.DSHStart{},
		PluginDispatches: map[string]string{"tool-1": "authorized"},
		AssemblyDecision: &domain.DSHAssemblyDecision{
			Status: "confirmed",
			DSHAssemblyReport: domain.DSHAssemblyReport{NativeTools: []domain.DSHNativeTool{
				{Kind: "mcp", AssetID: "owner-tool", Version: "1", Name: "mcp_owner_tool_lookup"},
			}},
		},
	}
	cp.Pending = []nativeCall{{Call: gateway.WorkbenchToolCall{
		ID: "tool-1", Name: "mcp_owner_tool_lookup", Arguments: []byte(`{"key":"allowed"}`),
	}}}
	for _, tc := range []struct{ name, frame, callID string }{
		{"discovery", `{"jsonrpc":"2.0","method":"tools/list"}`, ""},
		{"authorized", `{"jsonrpc":"2.0","method":"tools/call","params":{"name":"lookup","arguments":{"key":"allowed"}}}`, "tool-1"},
		{"different args", `{"jsonrpc":"2.0","method":"tools/call","params":{"name":"lookup","arguments":{"key":"blocked"}}}`, "deny"},
		{"different tool", `{"jsonrpc":"2.0","method":"tools/call","params":{"name":"delete","arguments":{"key":"allowed"}}}`, "deny"},
		{"ambiguous casing", `{"jsonrpc":"2.0","method":"tools/call","METHOD":"tools/list","params":{"name":"delete"}}`, "deny"},
		{"reverse casing", `{"jsonrpc":"2.0","method":"tools/list","METHOD":"tools/call","params":{"name":"delete"}}`, "deny"},
		{"duplicate method", `{"jsonrpc":"2.0","method":"tools/call","method":"tools/list","params":{"name":"delete"}}`, "deny"},
		{"parameter casing", `{"jsonrpc":"2.0","method":"tools/call","params":{"name":"lookup","NAME":"delete","arguments":{"key":"allowed"}}}`, "deny"},
		{"unregistered method", `{"jsonrpc":"2.0","method":"prompts/get"}`, "deny"},
		{"unsolicited result", `{"jsonrpc":"2.0","id":1,"result":{}}`, "deny"},
		{"unsolicited error", `{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"unavailable"}}`, "deny"},
		{"mixed request and result", `{"jsonrpc":"2.0","id":1,"method":"ping","result":{}}`, "deny"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := authorizeDSHStdioFrame([]byte(tc.frame+"\n"), ref, cp)
			if tc.callID == "deny" && err == nil || tc.callID != "deny" && (err != nil || got != tc.callID) {
				t.Fatalf("dispatch = %q, %v", got, err)
			}
		})
	}
	wrongAsset := domain.RuntimeAssetRef{Kind: "mcp", ID: "owner", Version: "1"}
	collision := []byte("{\"jsonrpc\":\"2.0\",\"method\":\"tools/call\",\"params\":{\"name\":\"tool_lookup\",\"arguments\":{\"key\":\"allowed\"}}}\n")
	if _, err := authorizeDSHStdioFrame(collision, wrongAsset, cp); err == nil {
		t.Fatal("another asset can use the frozen dispatch by matching its composed name")
	}
	cp.PersonalStdioDispatches = map[string]bool{"tool-1": true}
	frame := []byte("{\"jsonrpc\":\"2.0\",\"method\":\"tools/call\",\"params\":{\"name\":\"lookup\",\"arguments\":{\"key\":\"allowed\"}}}\n")
	if _, err := authorizeDSHStdioFrame(frame, ref, cp); err == nil {
		t.Fatal("dispatch can be replayed after reconnect")
	}
}

func TestDSHPersonalStdioNumericArguments(t *testing.T) {
	for _, tc := range []struct {
		name, actual, approved string
		want                   bool
	}{
		{"SDK numeric serialization", `{"n":[1,0.001],"z":0}`, `{"z":-0,"n":[1.0,1e-3]}`, true},
		{"negative exponent value", `{"n":-1.2E+4}`, `{"n":-12000}`, true},
		{"distinct unsafe integers", `{"id":9007199254740992}`, `{"id":9007199254740993.0}`, false},
		{"distinct precision beyond JS", `{"n":1}`, `{"n":1.00000000000000001}`, false},
		{"underflow differs from zero", `{"n":0}`, `{"n":1e-1000}`, false},
		{"different number kind", `{"n":"1"}`, `{"n":1.0}`, false},
		{"duplicate member", `{"n":2,"n":1}`, `{"n":1.0}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := sameDSHStdioArguments(json.RawMessage(tc.actual), json.RawMessage(tc.approved)); got != tc.want {
				t.Fatalf("matching arguments = %t, want %t", got, tc.want)
			}
		})
	}
}

func TestDSHPersonalStdioLeaseOwner(t *testing.T) {
	now := time.Now()
	item := domain.PersonalEnvironmentRecord{
		AssistantID: "assistant-1", CreatedBy: "user-1", Backend: "container", Provider: "docker", State: "ready",
		Lease: personalEnvironmentLease("assistant-1", "user-1", domain.PersonalEnvironmentPolicy{LeaseSeconds: 60}, now),
	}
	if !eligibleDSHStdioEnvironment(item, "user-1") || eligibleDSHStdioEnvironment(item, "user-2") {
		t.Fatal("the assistant lease must be usable only by its owner")
	}
	item.Lease.OwnerID = "user-1"
	if eligibleDSHStdioEnvironment(item, "user-1") {
		t.Fatal("the lease owner must be the assistant")
	}
}
