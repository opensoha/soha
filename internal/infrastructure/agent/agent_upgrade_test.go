package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestAgentUpgradeUsesStandardDeploymentAndHonorsOwnership(t *testing.T) {
	for _, owner := range []string{"", "Helm", "legacy"} {
		t.Run("owner_"+owner, func(t *testing.T) {
			writes := 0
			server := httptest.NewServer(agentUpgradeTestHandler(t, owner, &writes))
			defer server.Close()
			client := &Client{baseURL: server.URL, httpClient: server.Client()}
			status, err := client.GetAgentUpgradeStatus(context.Background())
			if err != nil || status.Version != "v0.1.6" || status.CanUpgrade != (owner == "") || status.RolloutStatus != "progressing" {
				t.Fatalf("status = %#v, error = %v", status, err)
			}
			previous, err := client.UpgradeAgent(context.Background(), "ghcr.io/opensoha/soha-agent:v0.1.7")
			if owner != "" {
				if !errors.Is(err, apperrors.ErrConflict) || writes != 0 {
					t.Fatalf("owner bypass: writes=%d err=%v", writes, err)
				}
			} else if err != nil || writes != 1 || previous != "ghcr.io/opensoha/soha-agent:v0.1.6" {
				t.Fatalf("writes=%d previous=%s err=%v", writes, previous, err)
			}
			if owner == "" {
				_, err = client.UpgradeAgent(context.Background(), "ghcr.io/opensoha/soha-agent:v0.1.5")
				if !errors.Is(err, apperrors.ErrConflict) || writes != 1 {
					t.Fatalf("downgrade reached Agent: writes=%d error=%v", writes, err)
				}
			}
		})
	}
}

func agentUpgradeTestHandler(t *testing.T, owner string, writes *int) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case owner == "legacy" && strings.Contains(r.URL.Path, "/ownership-v2/"):
			w.WriteHeader(http.StatusNotFound)
		case r.URL.Path == "/api/v1/version":
			_, _ = w.Write([]byte(`{"data":{"version":"v0.1.6"}}`))
		case strings.HasSuffix(r.URL.Path, "/yaml"):
			if r.URL.Query().Get("namespace") != agentWorkload {
				t.Error("wrong namespace")
			}
			manifest := "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: soha-agent\n  namespace: soha-agent\n  generation: 2\n  labels:\n    app.kubernetes.io/managed-by: '" + owner + "'\nspec:\n  template:\n    spec:\n      containers:\n      - name: soha-agent\n        image: ghcr.io/opensoha/soha-agent:v0.1.6\n"
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]string{"content": manifest}})
		case strings.HasSuffix(r.URL.Path, "/rollout-status"):
			_, _ = w.Write([]byte(`{"data":{"status":"healthy","message":"available","observedGeneration":1}}`))
		case r.URL.Path == "/api/v1/platform/ownership-v2/actions/deployments/image":
			(*writes)++
			var body updateDeploymentImageRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Name != agentWorkload || body.Namespace != agentWorkload || body.ContainerName != agentWorkload || body.Image != "ghcr.io/opensoha/soha-agent:v0.1.7" {
				t.Errorf("unexpected write: %#v", body)
			}
			_, _ = w.Write([]byte(`{"data":{"containerName":"soha-agent","previousImage":"ghcr.io/opensoha/soha-agent:v0.1.6"}}`))
		default:
			t.Errorf("unexpected request: %s", r.URL)
			w.WriteHeader(404)
		}
	})
}
