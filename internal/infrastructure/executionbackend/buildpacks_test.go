package executionbackend

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBuildpacksCapabilityRejectsOlderRunnerWithoutEndpoint(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/runtime/buildpacks/capability" {
			t.Errorf("unexpected fallback request: %s %s", r.Method, r.URL.Path)
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	runner, err := NewBuildpacksRunner(server.URL, "test-token")
	if err != nil {
		t.Fatal(err)
	}
	capability, err := runner.BuildpacksCapability(context.Background(), "app")
	if err != nil || capability.Ready || capability.Reason != "buildpacks_runner_unavailable" || requests != 1 {
		t.Fatalf("old runner must be unavailable: capability=%#v err=%v requests=%d", capability, err, requests)
	}
}
