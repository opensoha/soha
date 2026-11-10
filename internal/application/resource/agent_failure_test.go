package resource

import (
	"context"
	"errors"
	"net/http"
	"testing"

	apierrors "github.com/opensoha/soha/internal/api/errors"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestAgentOperationFailuresAreReturned(t *testing.T) {
	want := errors.New("agent request failed")
	agent := &failingResourceAgent{failure: want}
	service := New(Dependencies{
		Connections: stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{
			ID: "agent-cluster", ConnectionMode: domaincluster.ConnectionModeAgent,
		}}},
		Authorizer: requestActionAuthorizer{}, Permissions: allowRuntimePermission{}, Audit: discardAuditRecorder{},
		Agents: AgentClients{
			Helm:    func(domaincluster.Connection) (HelmAgent, error) { return agent, nil },
			RBAC:    func(domaincluster.Connection) (RBACAgent, error) { return agent, nil },
			Generic: func(domaincluster.Connection) (GenericResourceAgent, error) { return agent, nil },
		},
	})
	ctx, principal := context.Background(), domainidentity.Principal{UserID: "tester"}
	cases := []struct {
		name string
		run  func(*testing.T) error
	}{
		{"helm rollback preflight", func(t *testing.T) error {
			plan, err := service.Helm().PlanHelmReleaseRollback(ctx, principal, "agent-cluster", "team-a", "api", domainresource.HelmReleaseRollbackInput{Revision: 1})
			if plan.Ready {
				t.Error("failed agent preflight produced a ready plan")
			}
			return err
		}},
		{"helm rollback", func(t *testing.T) error {
			_, err := service.Helm().RollbackHelmRelease(ctx, principal, "agent-cluster", "team-a", "api", domainresource.HelmReleaseRollbackInput{Revision: 1})
			return err
		}},
		{"resource preflight", func(t *testing.T) error {
			plan, err := service.GenericResources().PlanResourceYAMLUpdate(ctx, principal, "agent-cluster", domainresource.ResourceUpdatePlanRequest{
				Namespace: "team-a", Kind: "ConfigMap", Name: "api", Content: "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: api\n",
			})
			if plan.Ready {
				t.Error("failed agent dry-run produced a ready plan")
			}
			if !errors.Is(err, apperrors.ErrClusterUnready) {
				t.Errorf("failed agent dry-run lost cluster unavailable classification: %v", err)
			}
			if status, code := apierrors.StatusCode(err), apierrors.Code(err); status != http.StatusBadGateway || code != "cluster_unavailable" {
				t.Errorf("failed agent dry-run maps to HTTP %d (%s), want 502 (cluster_unavailable)", status, code)
			}
			return err
		}},
		{"access review", func(t *testing.T) error {
			_, err := service.RBAC().ReviewSubjectAccess(ctx, principal, "agent-cluster", domainresource.SubjectAccessReviewInput{
				Subject: domainresource.AccessReviewSubject{Kind: "ServiceAccount", Namespace: "team-a", Name: "builder"},
				Checks:  []domainresource.AccessReviewCheck{{Verb: "get", Resource: "pods", Namespace: "team-a"}},
			})
			return err
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if err := test.run(t); !errors.Is(err, want) {
				t.Fatalf("agent error was lost: %v", err)
			}
		})
	}
}

type failingResourceAgent struct {
	HelmAgent
	RBACAgent
	GenericResourceAgent
	failure error
}

func (a *failingResourceAgent) DryRunHelmReleaseRollback(context.Context, string, string, domainresource.HelmReleaseRollbackInput) error {
	return a.failure
}

func (a *failingResourceAgent) RollbackHelmRelease(context.Context, string, string, domainresource.HelmReleaseRollbackInput) (domainresource.HelmReleaseDetailView, error) {
	return domainresource.HelmReleaseDetailView{}, a.failure
}

func (a *failingResourceAgent) DryRunResourceYAML(context.Context, string, string, string, string) (domainresource.ResourceUpdateAnalysis, error) {
	return domainresource.ResourceUpdateAnalysis{}, a.failure
}

func (a *failingResourceAgent) ReviewSubjectAccess(context.Context, domainresource.SubjectAccessReviewInput) (domainresource.SubjectAccessReviewResult, error) {
	return domainresource.SubjectAccessReviewResult{}, a.failure
}
