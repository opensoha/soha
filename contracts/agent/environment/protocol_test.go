package environment

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRequestFixturesMatchRuntimeBoundary(t *testing.T) {
	for _, kind := range []string{"valid", "invalid"} {
		files, err := filepath.Glob(filepath.Join("..", "..", "fixtures", "json-schema", "personal-environment-request", kind, "*.json"))
		if err != nil || len(files) == 0 {
			t.Fatalf("missing fixtures: %v", err)
		}
		for _, file := range files {
			t.Run(kind+"/"+filepath.Base(file), func(t *testing.T) {
				data, err := os.ReadFile(file) // #nosec G304 -- Glob is rooted in repository fixtures with fixed valid/invalid categories.
				if err != nil {
					t.Fatal(err)
				}
				var request Request
				err = json.Unmarshal(data, &request)
				if (err == nil) != (kind == "valid") {
					t.Fatalf("fixture acceptance=%t; want %s", err == nil, kind)
				}
			})
		}
	}
}

func TestRequestRejectsUnboundedOriginalLeaseAndNullCommand(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "fixtures", "json-schema", "personal-environment-request", "valid", "acquire.json"))
	if err != nil {
		t.Fatal(err)
	}
	var request Request
	if err := json.Unmarshal(data, &request); err != nil {
		t.Fatal(err)
	}
	request.Lease.ExpiresAt = request.Lease.IssuedAt.Add(25 * time.Hour)
	if request.Validate() == nil {
		t.Fatal("unbounded lease accepted")
	}
	var properties map[string]any
	if err := json.Unmarshal(data, &properties); err != nil {
		t.Fatal(err)
	}
	properties["argv"] = nil
	nullCommand, err := json.Marshal(properties)
	if err != nil {
		t.Fatal(err)
	}
	if json.Unmarshal(nullCommand, &request) == nil {
		t.Fatal("null command accepted by non-execute action")
	}
}

func TestCommandReceiptPinsTheExactTemporaryTask(t *testing.T) {
	now := time.Now().UTC()
	request := Request{SchemaVersion: RequestVersion, Action: "execute", TaskID: "00000000-0000-4000-8000-000000000048", Argv: []string{"/bin/true"}, Lease: Lease{ID: "lease", RunID: "run"}}
	text := "fixture"
	result := Result{SchemaVersion: ResultVersion, LeaseID: "lease", RunID: "run", State: "active", Output: &text, ObservedAt: now, Snapshot: &Snapshot{SchemaVersion: SnapshotVersion, LeaseID: "lease", RunID: "run", State: "active", ObservedAt: now}}
	for _, task := range []string{"", "00000000-0000-4000-8000-000000000049", request.TaskID} {
		result.TaskID = task
		if err := result.ValidateFor(request); (err == nil) != (task == request.TaskID) {
			t.Fatal("command receipt changed or omitted its task identity")
		}
	}
}

func TestDataDeletionRequiresAConfirmedReceipt(t *testing.T) {
	request := Request{Action: "delete_data", Lease: Lease{ID: "lease", RunID: "run"}}
	for _, state := range []string{"missing", "released", "active", "deleted"} {
		result := Result{SchemaVersion: ResultVersion, LeaseID: "lease", RunID: "run", State: state, ObservedAt: time.Now().UTC()}
		if err := result.ValidateFor(request); (err == nil) != (state == "deleted") {
			t.Fatalf("unconfirmed %s data deletion: %v", state, err)
		}
	}
}
