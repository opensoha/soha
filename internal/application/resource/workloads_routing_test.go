package resource

import (
	"context"
	"errors"
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainoperation "github.com/opensoha/soha/internal/domain/operation"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
)

func TestListWorkloadResourcesPreservesDirectSourceAndActions(t *testing.T) {
	t.Parallel()

	audit := &workloadAuditRecorder{}
	w := workloadRoutingTestService(audit, nil)
	items, err := listWorkloadResources(context.Background(), w, domainidentity.Principal{UserID: "user-1"}, "direct-cluster", "team-a", workloadListSpec[domainresource.JobView]{
		kind: "Job", auditText: "listed jobs",
		agent: func(WorkloadAgent) ([]domainresource.JobView, error) {
			t.Fatal("agent route called for direct connection")
			return nil, nil
		},
		direct: func() ([]domainresource.JobView, string, error) {
			return []domainresource.JobView{{Name: "migrate", Namespace: "team-a"}}, "cache", nil
		},
		namespaceOf: func(item domainresource.JobView) string { return item.Namespace },
		populate:    populateAllowedActionsJobs,
	})
	if err != nil {
		t.Fatalf("listWorkloadResources() error = %v", err)
	}
	if len(items) != 1 || len(items[0].AllowedActions) == 0 {
		t.Fatalf("items = %#v, want populated actions", items)
	}
	if len(audit.entries) != 1 || audit.entries[0].Summary != "listed jobs via cache in namespace team-a" {
		t.Fatalf("audit entries = %#v", audit.entries)
	}
}

func TestPerformWorkloadMutationPreservesAuditAndOperationBehavior(t *testing.T) {
	t.Parallel()

	audit := &workloadAuditRecorder{}
	operations := &workloadOperationRecorder{}
	w := workloadRoutingTestService(audit, operations)
	directCalls := 0
	err := performWorkloadMutation(context.Background(), w, domainidentity.Principal{UserID: "user-1"}, "direct-cluster", "team-a", "api", workloadMutationSpec{
		permission: appaccess.PermPlatformDeploymentRestart, kind: "Deployment", action: domainaccess.ActionRestart,
		agent: func(WorkloadAgent) error {
			t.Fatal("agent route called for direct connection")
			return nil
		},
		direct: func() error {
			directCalls++
			return nil
		},
		successMessage:   func(source string) string { return "restarted deployment via " + source },
		auditErrorPrefix: "record restart deployment audit", operation: "platform.deployment.restart",
	})
	if err != nil {
		t.Fatalf("performWorkloadMutation() error = %v", err)
	}
	if directCalls != 1 || len(audit.entries) != 1 || audit.entries[0].Result != "success" {
		t.Fatalf("directCalls=%d audit=%#v", directCalls, audit.entries)
	}
	if len(operations.entries) != 1 || operations.entries[0].OperationType != "platform.deployment.restart" {
		t.Fatalf("operation entries = %#v", operations.entries)
	}

	directErr := errors.New("direct restart failed")
	err = performWorkloadMutation(context.Background(), w, domainidentity.Principal{UserID: "user-1"}, "direct-cluster", "team-a", "api", workloadMutationSpec{
		permission: appaccess.PermPlatformDeploymentRestart, kind: "Deployment", action: domainaccess.ActionRestart,
		agent: func(WorkloadAgent) error { return nil }, direct: func() error { return directErr },
		successMessage:   func(source string) string { return "restarted deployment via " + source },
		auditErrorPrefix: "record restart deployment audit", operation: "platform.deployment.restart",
	})
	if !errors.Is(err, directErr) {
		t.Fatalf("performWorkloadMutation() error = %v, want %v", err, directErr)
	}
	if len(audit.entries) != 2 || audit.entries[1].Result != "failure" || len(operations.entries) != 1 {
		t.Fatalf("audit=%#v operations=%#v", audit.entries, operations.entries)
	}
}

func workloadRoutingTestService(audit AuditRecorder, operations OperationRecorder) *Workloads {
	return &Workloads{resourceAccess: &resourceAccess{
		resolver: stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{
			ID: "direct-cluster", ConnectionMode: domaincluster.ConnectionModeDirectKubeconfig,
		}}},
		authorizer: allowAllResourceAuthorizer{}, permissions: allowWorkloadPermission{},
		audit: audit, operations: operations,
	}}
}

type allowWorkloadPermission struct{}

func (allowWorkloadPermission) Authorize(context.Context, domainidentity.Principal, string) error {
	return nil
}

type workloadAuditRecorder struct{ entries []domainaudit.Entry }

func (r *workloadAuditRecorder) Record(_ context.Context, entry domainaudit.Entry) error {
	r.entries = append(r.entries, entry)
	return nil
}

type workloadOperationRecorder struct{ entries []domainoperation.Entry }

func (r *workloadOperationRecorder) Record(_ context.Context, entry domainoperation.Entry) error {
	r.entries = append(r.entries, entry)
	return nil
}
