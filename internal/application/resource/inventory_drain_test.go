package resource

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	cr "github.com/opensoha/soha-contracts/resource"
	aperrors "github.com/opensoha/soha/internal/api/errors"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	agentinfra "github.com/opensoha/soha/internal/infrastructure/agent"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestDrainFailureIsLocalizedSafeConflictWithoutChangingOtherErrors(t *testing.T) {
	for _, code := range []string{"node_drain_pdb_blocked", "node_drain_timeout", "node_drain_canceled", "node_drain_pod_selection", "node_drain_eviction_blocked", "node_drain_failed"} {
		err := nodeDrainFailure(&cr.NodeDrainError{Code: code, Cause: errors.New("private token")})
		var business *apperrors.BusinessError
		if !errors.Is(err, apperrors.ErrConflict) || !errors.As(err, &business) || business.Code() != code {
			t.Fatalf("lost classification: %v", err)
		}
		if strings.Contains(business.Message("zh-CN"), "private") || !strings.Contains(business.Message("zh-CN"), "排水") && code != "node_drain_pdb_blocked" {
			t.Fatalf("unsafe translation: %v", business)
		}
	}
	denied := apperrors.ErrAccessDenied
	if nodeDrainFailure(denied) != denied {
		t.Fatal("denied semantics changed")
	}
}

func TestInventoryPreservesAgentHTTPDrainFailureThroughPublicErrorMapping(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/platform/ownership-v2/infrastructure/nodes/node/drain" {
			t.Errorf("unexpected Agent action %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": "node_drain_pdb_blocked", "message": "private provider token"}})
	}))
	defer server.Close()
	connection := domaincluster.Connection{Summary: domaincluster.Summary{ID: "c", ConnectionMode: domaincluster.ConnectionModeAgent}, Metadata: map[string]any{"endpoint": server.URL, "token": "fixture"}}
	registry := agentinfra.NewRegistry(time.Second)
	inventory := &Inventory{resourceAccess: &resourceAccess{resolver: stubConnectionResolver{connection: connection}, authorizer: exactPermissionAuthorizer{"platform.nodes.drain": true}, audit: discardAuditRecorder{}}, agent: func(c domaincluster.Connection) (InventoryAgent, error) { return registry.ClientFor(c) }}
	err := inventory.DrainNode(context.Background(), domainidentity.Principal{}, "c", "node", domainresource.NodeDrainInput{TimeoutSeconds: 30})
	if aperrors.StatusCode(err) != 409 || aperrors.Code(err) != "node_drain_pdb_blocked" || !strings.Contains(aperrors.Message(err, "zh-CN"), "PodDisruptionBudget") || strings.Contains(aperrors.Message(err), "private") {
		t.Fatalf("public error changed: %v", err)
	}
}
