package guest

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	env "github.com/opensoha/soha-contracts/agent/environment"
)

func runtimeFixture(t *testing.T) (RuntimeOperation, time.Time) {
	t.Helper()
	binding := guestFixture[Binding](t, "binding")
	now := binding.IssuedAt.Add(time.Second)
	op := RuntimeOperation{SchemaVersion: RuntimeOperationVersion, OperationID: binding.ID, Binding: binding, Kind: "environment", Deadline: now.Add(30 * time.Second), Request: env.Request{SchemaVersion: env.RequestVersion, Action: "inspect", Lease: env.Lease{SchemaVersion: env.LeaseVersion, ID: binding.ID, RunID: binding.ID, OwnerID: binding.AssistantID, ScopeHash: "sha256:" + strings.Repeat("a", 64), Mode: "restricted-write", IssuedAt: binding.IssuedAt, ExpiresAt: binding.ExpiresAt, Quota: env.Quota{DiskBytes: 16 << 20, MemoryBytes: 2 << 30, CPUUnits: 1000, MaxProcesses: 64}}}}
	op.ArtifactSHA256 = strings.Repeat("a", 64)
	return op, now
}

func TestRuntimeJSONRejectsAmbiguity(t *testing.T) {
	for _, raw := range []string{`{"path":"a","path":"b"}`, `{"Path":"a"}`, `{"path":"a","unknown":true}`} {
		var value RuntimeFileInput
		if json.Unmarshal([]byte(raw), &value) == nil {
			t.Fatal("ambiguous runtime JSON accepted", raw)
		}
	}
	for _, raw := range []string{`{"url":"https://example.com/","method":"GET","method":"POST"}`, `{"url":"https://example.com/","method":"CONNECT"}`, `{"url":"file:///etc/shadow","method":"GET"}`} {
		var value BrowserNetworkRequest
		if json.Unmarshal([]byte(raw), &value) == nil {
			t.Fatal("invalid network frame accepted", raw)
		}
	}
}

func TestRuntimeAuthorityAndWriteBounds(t *testing.T) {
	op, now := runtimeFixture(t)
	if err := op.Validate(now); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*RuntimeOperation){
		"owner":         func(o *RuntimeOperation) { o.Request.Lease.OwnerID = o.Binding.OwnerID },
		"expired":       func(o *RuntimeOperation) { o.Deadline = now },
		"long deadline": func(o *RuntimeOperation) { o.Deadline = now.Add(time.Minute + time.Second) },
		"creation":      func(o *RuntimeOperation) { o.Request.CreateData = true },
		"disposal":      func(o *RuntimeOperation) { o.Request.Action = "delete_data" },
		"wrong kind":    func(o *RuntimeOperation) { o.Kind = "root" },
	} {
		t.Run(name, func(t *testing.T) {
			other := op
			change(&other)
			if other.Validate(now) == nil {
				t.Fatal("invalid authority accepted")
			}
		})
	}
	op.Kind, op.File = "file_write", &RuntimeFileInput{Path: "notes/example.txt", ContentBase64: base64.StdEncoding.EncodeToString([]byte("ok"))}
	if err := op.Validate(now); err != nil {
		t.Fatal(err)
	}
	op.Request.Lease.Mode = "observe-only"
	if op.Validate(now) == nil {
		t.Fatal("read-only lease wrote a file")
	}
	op.Request.Lease.Mode = "restricted-write"
	op.File.ContentBase64 = base64.StdEncoding.EncodeToString(make([]byte, RuntimeFileLimit+1))
	if op.Validate(now) == nil {
		t.Fatal("oversized write accepted")
	}
}

func TestRuntimePathsAndBrowserTargets(t *testing.T) {
	for _, path := range []string{"", "/etc/shadow", "..", "foo/../bar", "a//b", "a/./b", "a\\b", "x\x00"} {
		if RuntimeFilePath(path) {
			t.Fatalf("unsafe path %q accepted", path)
		}
	}
	for _, target := range []string{"file:///etc/passwd", "file:///data/../private", "file:///data/%2e%2e/private", "file://host/data/a", "https://user:pass@example.com/", "https://example.com:8080/", "javascript:alert(1)", "http://example.com"} {
		if BrowserURL(target) {
			t.Fatalf("unsafe target %q accepted", target)
		}
	}
	for _, target := range []string{"https://example.com/", "file:///data/acceptance.html"} {
		if !BrowserURL(target) {
			t.Fatalf("bounded target %q refused", target)
		}
	}
}

func TestRuntimeReceiptCorrelationAndDigest(t *testing.T) {
	op, now := runtimeFixture(t)
	result := RuntimeResult{SchemaVersion: RuntimeResultVersion, OperationID: op.OperationID, BindingID: op.Binding.ID, Environment: env.Result{SchemaVersion: env.ResultVersion, LeaseID: op.Request.Lease.ID, RunID: op.Request.Lease.RunID, State: "active", ObservedAt: now, Snapshot: &env.Snapshot{SchemaVersion: env.SnapshotVersion, LeaseID: op.Request.Lease.ID, RunID: op.Request.Lease.RunID, State: "active", ObservedAt: now}}}
	if err := result.ValidateFor(op); err != nil {
		t.Fatal(err)
	}
	result.BindingID = op.Binding.DataRef
	if result.ValidateFor(op) == nil {
		t.Fatal("foreign receipt accepted")
	}
	digest, err := op.Digest()
	if err != nil {
		t.Fatal(err)
	}
	op.Kind, op.File = "file_read", &RuntimeFileInput{Path: "a"}
	changed, err := op.Digest()
	if err != nil || digest == changed {
		t.Fatal("payload did not change request identity")
	}
}
