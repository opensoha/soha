package agent

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	contractenv "github.com/opensoha/soha-contracts/agent/environment"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
)

func personalEnvironmentFixture() (contractenv.Request, contractenv.Result) {
	now := time.Now().UTC()
	input := contractenv.Request{SchemaVersion: contractenv.RequestVersion, Action: "execute", Argv: []string{"/bin/true"}, Lease: contractenv.Lease{
		SchemaVersion: contractenv.LeaseVersion, ID: "lease-a", RunID: "run-a", OwnerID: "assistant-a", ScopeHash: "sha256:" + strings.Repeat("a", 64), Mode: "restricted-write",
		IssuedAt: now, ExpiresAt: now.Add(time.Hour), Quota: contractenv.Quota{DiskBytes: 32 << 20, MemoryBytes: 64 << 20, CPUUnits: 250, MaxProcesses: 16}}}
	output := "fixture"
	result := contractenv.Result{SchemaVersion: contractenv.ResultVersion, LeaseID: input.Lease.ID, RunID: input.Lease.RunID, State: "active", ObservedAt: now, Output: &output,
		Snapshot: &contractenv.Snapshot{SchemaVersion: contractenv.SnapshotVersion, LeaseID: input.Lease.ID, RunID: input.Lease.RunID, State: "active", ObservedAt: now,
			ResourceUsage: contractenv.Usage{DiskBytes: 4096, MemoryBytes: 1048576, CPUTimeNanos: 1000, Processes: 2}}}
	return input, result
}

func TestPersonalEnvironmentUsesRegisteredAuthAndRejectsFalseReceiptsWithoutRetry(t *testing.T) {
	for _, test := range []struct {
		name     string
		status   int
		change   func(*contractenv.Result)
		accepted bool
	}{
		{"valid", http.StatusOK, func(*contractenv.Result) {}, true},
		{"other lease", http.StatusOK, func(r *contractenv.Result) { r.LeaseID = "other" }, false},
		{"other run observation", http.StatusOK, func(r *contractenv.Result) { r.Snapshot.RunID = "other" }, false},
		{"no observation", http.StatusOK, func(r *contractenv.Result) { r.Snapshot = nil }, false},
		{"false command completion", http.StatusOK, func(r *contractenv.Result) { r.Output = nil }, false},
		{"stale receipt", http.StatusOK, func(r *contractenv.Result) {
			r.ObservedAt = time.Now().Add(-2 * time.Minute)
			r.Snapshot.ObservedAt = r.ObservedAt
		}, false},
		{"unknown outcome", http.StatusBadGateway, func(*contractenv.Result) {}, false},
		{"old Agent", http.StatusNotFound, func(*contractenv.Result) {}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			input, result := personalEnvironmentFixture()
			test.change(&result)
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.URL.Path != "/api/v1/runtime/personal-environments" || r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer fixture-agent-token" {
					t.Error("request bypassed registered connection")
				}
				var actual contractenv.Request
				if err := json.NewDecoder(r.Body).Decode(&actual); err != nil || actual.Lease.OwnerID != input.Lease.OwnerID || actual.Lease.ID != input.Lease.ID || actual.Lease.ScopeHash != input.Lease.ScopeHash || !actual.Lease.IssuedAt.Equal(input.Lease.IssuedAt) {
					t.Error("original lease changed")
				}
				w.WriteHeader(test.status)
				_ = json.NewEncoder(w).Encode(map[string]any{"data": result})
			}))
			defer server.Close()
			registry := NewRegistry(time.Second)
			defer func() { _ = registry.Close() }()
			client, err := registry.ClientFor(domaincluster.Connection{Summary: domaincluster.Summary{ID: "node-a"}, Metadata: map[string]any{"endpoint": server.URL, "token": "fixture-agent-token"}})
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.PersonalEnvironment(t.Context(), input)
			if (err == nil) != test.accepted || calls.Load() != 1 {
				t.Fatalf("accepted=%t calls=%d", err == nil, calls.Load())
			}
			if errors.Is(err, domaincopilot.ErrPersonalEnvironmentNotDispatched) {
				t.Fatal("dispatched Agent failure was marked safe to restore")
			}
			if client.httpClient.Timeout != time.Second {
				t.Fatal("operation changed shared client timeout")
			}
		})
	}
}

func TestPersonalEnvironmentRejectsUncredentialedConnectionBeforeDispatch(t *testing.T) {
	input, _ := personalEnvironmentFixture()
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("unauthenticated operation dispatched") }))
	defer server.Close()
	client := &Client{baseURL: server.URL, httpClient: server.Client()}
	if _, err := client.PersonalEnvironment(t.Context(), input); !errors.Is(err, domaincopilot.ErrPersonalEnvironmentNotDispatched) {
		t.Fatalf("uncredentialed connection did not prove no dispatch: %v", err)
	}
	client.token = "fixture-agent-token"
	input.Lease.OwnerID = ""
	if _, err := client.PersonalEnvironment(t.Context(), input); !errors.Is(err, domaincopilot.ErrPersonalEnvironmentNotDispatched) {
		t.Fatalf("invalid input did not prove no dispatch: %v", err)
	}
}

func TestPersonalEnvironmentPreflightBindsRegisteredAuthExactQuotaAndNonceWithoutRetry(t *testing.T) {
	for _, invalid := range []string{"", "quota", "nonce", "stale", "unsupported"} {
		t.Run(invalid, func(t *testing.T) {
			input := contractenv.PreflightRequest{SchemaVersion: contractenv.PreflightRequestVersion, ID: "00000000-0000-4000-8000-000000000048", Quota: contractenv.Quota{DiskBytes: 32 << 20, MemoryBytes: 64 << 20, CPUUnits: 250, MaxProcesses: 16}}
			result := contractenv.PreflightResult{SchemaVersion: contractenv.PreflightResultVersion, ID: input.ID, Quota: input.Quota, Ready: true, CgroupVersion: "2", DiskMechanism: "btrfs-qgroup", ObservedAt: time.Now().UTC()}
			switch invalid {
			case "quota":
				result.Quota.DiskBytes++
			case "nonce":
				result.ID = "other"
			case "stale":
				result.ObservedAt = time.Now().Add(-2 * time.Minute)
			}
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != http.MethodPost || r.URL.Path != "/api/v1/runtime/personal-environments/preflight" || r.Header.Get("Authorization") != "Bearer fixture-agent-token" {
					t.Error("preflight bypassed registered connection")
				}
				var actual contractenv.PreflightRequest
				if err := json.NewDecoder(r.Body).Decode(&actual); err != nil || actual != input {
					t.Error("preflight quota/nonce changed")
				}
				if invalid == "unsupported" {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				if err := json.NewEncoder(w).Encode(map[string]any{"data": result}); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()
			client := &Client{baseURL: server.URL, token: "fixture-agent-token", httpClient: server.Client()}
			actual, err := client.PersonalEnvironmentPreflight(t.Context(), input)
			if (err == nil) != (invalid == "") || calls.Load() != 1 {
				t.Fatalf("preflight accepted=%t calls=%d", err == nil, calls.Load())
			}
			if err != nil && actual.Ready {
				t.Fatal("invalid receipt leaked readiness")
			}
		})
	}
}
