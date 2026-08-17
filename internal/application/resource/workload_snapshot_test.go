package resource

import (
	"context"
	"testing"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
)

func TestGenerateWorkloadSnapshotAuthorizesTargetAndSource(t *testing.T) {
	t.Parallel()
	authorizer := &snapshotAuthorizer{}
	direct := &snapshotSourceDirect{}
	builderCalled := false
	workloads := &Workloads{
		resourceAccess: &resourceAccess{
			resolver:   stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{ID: "cluster-a", ConnectionMode: domaincluster.ConnectionModeDirectKubeconfig}}},
			authorizer: authorizer, audit: discardAuditRecorder{},
		},
		direct: direct,
		snapshot: func(content string, request domainresource.WorkloadSnapshotRequest) (domainresource.WorkloadSnapshot, error) {
			builderCalled = true
			if content != "source-yaml" || request.TargetName != "report" {
				t.Fatalf("builder input = %q %#v", content, request)
			}
			return domainresource.WorkloadSnapshot{Content: "job-yaml", Containers: []domainresource.WorkloadSnapshotContainer{}, Warnings: []string{}}, nil
		},
	}

	_, err := workloads.GenerateWorkloadSnapshot(t.Context(), domainidentity.Principal{UserID: "user-1"}, "cluster-a", domainresource.WorkloadSnapshotRequest{
		Namespace: " ops ", SourceKind: domainresource.WorkloadSnapshotSourceDeployment, SourceName: " api ",
		TargetKind: domainresource.WorkloadSnapshotTargetJob, TargetName: " report ", RestartPolicy: domainresource.WorkloadSnapshotRestartNever,
	})
	if err != nil {
		t.Fatalf("GenerateWorkloadSnapshot() error = %v", err)
	}
	if !builderCalled || !direct.called {
		t.Fatalf("builderCalled=%v sourceCalled=%v", builderCalled, direct.called)
	}
	if len(authorizer.requests) < 2 || authorizer.requests[0].Resource.Kind != "Job" || authorizer.requests[0].Action != domainaccess.ActionCreate ||
		authorizer.requests[1].Resource.Kind != "Deployment" || authorizer.requests[1].Action != domainaccess.ActionView {
		t.Fatalf("authorization requests = %#v", authorizer.requests)
	}
}

func TestNormalizeWorkloadCronJobRequiresSchedule(t *testing.T) {
	t.Parallel()

	request := domainresource.WorkloadSnapshotRequest{
		Namespace: "ops", SourceKind: domainresource.WorkloadSnapshotSourceDeployment, SourceName: "api",
		TargetKind: domainresource.WorkloadSnapshotTargetWorkloadCronJob, TargetName: "report", RestartPolicy: domainresource.WorkloadSnapshotRestartNever,
	}
	if _, err := normalizeWorkloadSnapshotRequest(request); err == nil {
		t.Fatal("normalizeWorkloadSnapshotRequest() accepted a WorkloadCronJob without a schedule")
	}
	request.Schedule = "0 * * * *"
	if _, err := normalizeWorkloadSnapshotRequest(request); err != nil {
		t.Fatalf("normalizeWorkloadSnapshotRequest() error = %v", err)
	}
}

type snapshotSourceDirect struct {
	DirectWorkloads
	called bool
}

func (d *snapshotSourceDirect) GetDeploymentYAML(context.Context, string, string, string) (domainresource.ResourceYAMLView, error) {
	d.called = true
	return domainresource.ResourceYAMLView{Content: "source-yaml"}, nil
}

type snapshotAuthorizer struct{ requests []domainaccess.Request }

func (a *snapshotAuthorizer) Authorize(_ context.Context, request domainaccess.Request) (domainaccess.Decision, error) {
	a.requests = append(a.requests, request)
	return domainaccess.Decision{Allowed: true}, nil
}
