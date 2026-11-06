package resource

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	contractresource "github.com/opensoha/soha-contracts/resource"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	agentinfra "github.com/opensoha/soha/internal/infrastructure/agent"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestAgentHelmWritesRequireActionSpecificClusterWideDecision(t *testing.T) {
	ctx := context.Background()
	principal := domainidentity.Principal{UserID: "user-1"}
	tests := []struct {
		action domainaccess.Action
		name   string
		run    func(*Helm) error
	}{
		{domainaccess.ActionCreate, "install", func(h *Helm) error {
			_, err := h.InstallHelmChart(ctx, principal, "agent-cluster", domainresource.HelmChartInstallInput{
				RepositoryURL: "https://charts.example", ChartName: "chart", Version: "1.0.0", ReleaseName: "test", Namespace: "platform",
			})
			return err
		}},
		{domainaccess.ActionUpdate, "values", func(h *Helm) error {
			_, err := h.UpdateHelmReleaseValues(ctx, principal, "agent-cluster", "platform", "test", "{}")
			return err
		}},
		{domainaccess.ActionUpdate, "conditional values", func(h *Helm) error {
			_, err := h.UpdateHelmReleaseValuesWithRevision(ctx, principal, "agent-cluster", "platform", "test", "{}", 3)
			return err
		}},
		{domainaccess.ActionDelete, "delete", func(h *Helm) error {
			return h.DeleteHelmRelease(ctx, principal, "agent-cluster", "platform", "test")
		}},
		{domainaccess.ActionRollback, "rollback preflight", func(h *Helm) error {
			_, err := h.PlanHelmReleaseRollback(ctx, principal, "agent-cluster", "platform", "test", domainresource.HelmReleaseRollbackInput{Revision: 1})
			return err
		}},
		{domainaccess.ActionRollback, "rollback", func(h *Helm) error {
			_, err := h.RollbackHelmRelease(ctx, principal, "agent-cluster", "platform", "test", domainresource.HelmReleaseRollbackInput{Revision: 1})
			return err
		}},
		{domainaccess.ActionRollback, "managed rollback", func(h *Helm) error {
			_, err := h.AuthorizeHelmDelivery(ctx, principal, sohaapi.HelmDeliverySnapshot{ClusterID: "agent-cluster", Namespace: "platform", Operation: sohaapi.HelmDeliverySnapshotOperationRollback}, true)
			return err
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			authorizer := &scopeDecisionAuthorizer{decision: domainaccess.Decision{
				Allowed: true, ResourceScope: &domainaccess.ResourceScope{Namespaces: []string{"platform"}},
			}}
			service := New(Dependencies{Connections: stubConnectionResolver{connection: agentConnection("https://agent.example")}, Authorizer: authorizer, Audit: noopResourceAuditRecorder{}})
			if err := tt.run(service.Helm()); !errors.Is(err, apperrors.ErrAccessDenied) {
				t.Fatalf("write must stop before Agent dispatch: %v", err)
			}
			req := authorizer.request
			if !req.RequireClusterWideNamespace || req.Action != tt.action || req.Resource.Kind != "HelmRelease" || req.Namespace.Namespace != "" {
				t.Fatalf("wrong cluster-wide authorization request: %+v", req)
			}
		})
	}
}

func TestManagedAgentHelmRequiresMatchingRuntimeOptIn(t *testing.T) {
	tests := []struct {
		name   string
		checks []contractresource.AgentActionCheck
		want   error
	}{
		{name: "older Agent", want: apperrors.ErrClusterUnready},
		{name: "different action", checks: []contractresource.AgentActionCheck{{Action: "platform.helm_releases.install", Configured: "allowed", Implementation: "supported"}}, want: apperrors.ErrAccessDenied},
		{name: "action disabled", checks: []contractresource.AgentActionCheck{{Action: "platform.helm_releases.rollback", Configured: "denied", Implementation: "supported"}}, want: apperrors.ErrAccessDenied},
		{name: "explicitly enabled", checks: []contractresource.AgentActionCheck{{Action: "platform.helm_releases.rollback", Configured: "allowed", Implementation: "supported"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/api/v1/diagnostics/actions" || tt.checks == nil {
					http.NotFound(w, r)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"items": tt.checks})
			}))
			defer server.Close()
			service := New(Dependencies{
				Agents:      testAgentClients(agentinfra.NewRegistry(0)),
				Connections: stubConnectionResolver{connection: agentConnection(server.URL)},
				Authorizer:  allowAllResourceAuthorizer{},
				Audit:       noopResourceAuditRecorder{},
			})
			snapshot := sohaapi.HelmDeliverySnapshot{ClusterID: "agent-cluster", Namespace: "platform", Operation: sohaapi.HelmDeliverySnapshotOperationRollback}
			provider, err := service.Helm().AuthorizeHelmDelivery(context.Background(), domainidentity.Principal{UserID: "user-1"}, snapshot, true)
			if !errors.Is(err, tt.want) || tt.want == nil && provider != "helm_agent.agent-cluster" {
				t.Fatalf("provider = %q, error = %v, want %v", provider, err, tt.want)
			}
		})
	}
}
