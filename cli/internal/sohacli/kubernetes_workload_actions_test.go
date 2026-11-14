package sohacli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	contractresource "github.com/opensoha/soha/contracts/resource"
)

func TestKubernetesWorkloadActionPreflightsExactInputAndChecksReceipt(t *testing.T) {
	for _, scenario := range []string{"execute", "dry-run", "old-server", "changed-version", "malformed-receipt"} {
		t.Run(scenario, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "POST" || r.URL.Path != "/api/v1/clusters/cluster-a/resources/Deployment/app/conditional/actions/scale" || r.URL.Query().Get("namespace") != "team-a" {
					t.Errorf("unexpected route: %s %s", r.Method, r.URL.String())
				}
				var input contractresource.ConditionalWorkloadActionInput
				if json.NewDecoder(r.Body).Decode(&input) != nil || input.Preconditions.ExpectedUID != "uid" || input.Preconditions.ExpectedResourceVersion != "reviewed-rv" || input.Replicas == nil || *input.Replicas != 0 || input.DryRun != (calls == 1) {
					t.Errorf("changed request: %+v", input)
				}
				w.Header().Set("Content-Type", "application/json")
				if scenario == "old-server" || scenario == "changed-version" {
					status := 404
					if scenario == "changed-version" {
						status = 409
					}
					w.WriteHeader(status)
					_, _ = fmt.Fprint(w, `{"error":{"code":"conflict","message":"request rejected"}}`)
					return
				}
				item := contractresource.ConditionalWorkloadActionReceipt{Applied: !input.DryRun, DryRun: input.DryRun, Kind: "Deployment", Name: "app", Namespace: "team-a", Action: "scale", UID: "uid", ResourceVersion: "next", Preconditions: input.Preconditions}
				if scenario == "malformed-receipt" {
					item.UID = "recreated"
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"data": item})
			}))
			defer server.Close()
			args := []string{"k8s", "workloads", "scale", "Deployment", "app", "--cluster-id", "cluster-a", "--namespace", "team-a", "--expected-uid", "uid", "--expected-resource-version", "reviewed-rv", "--replicas", "0", "--yes", "--profile", "dev"}
			if scenario == "dry-run" {
				args = append(args, "--dry-run")
			}
			var out, errOut bytes.Buffer
			code := Run(t.Context(), args, Runtime{Out: &out, Err: &errOut, ConfigPath: writeTestConfig(t, server.URL)})
			want := 1
			if scenario == "execute" {
				want = 2
			}
			if calls != want || (code == 0) != (scenario == "execute" || scenario == "dry-run") {
				t.Fatalf("code=%d calls=%d stderr=%s", code, calls, errOut.String())
			}
		})
	}
}
