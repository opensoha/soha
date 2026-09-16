package delivery

import (
	"context"
	"errors"
	"testing"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appdocker "github.com/opensoha/soha/internal/application/docker"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domaindocker "github.com/opensoha/soha/internal/domain/docker"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type batchDockerFixture struct {
	operations     map[string]domaindocker.Operation
	verdict        sohaapi.CapabilityAssessmentVerdict
	lastAssessment appdocker.ProjectAssessmentInput
	queueErr       error
}

func (*batchDockerFixture) DeliveryProjectAccess(context.Context, domainidentity.Principal, sohaapi.DockerDeliverySnapshot, string) ([]domaindelivery.AccessCandidate, error) {
	return nil, nil
}

func (f *batchDockerFixture) FreezeDeliveryProject(context.Context, domainidentity.Principal, string, string) (domaindocker.PreparedDeliveryProject, error) {
	return domaindocker.PreparedDeliveryProject{HostID: "host", ProjectID: "project", ProjectDigest: "frozen-project", Ciphertext: "frozen"}, nil
}
func (f *batchDockerFixture) PrepareDeliveryProject(_ context.Context, _ domainidentity.Principal, _ string, mappings, artifacts map[string]string) (domaindocker.PreparedDeliveryProject, error) {
	images := map[string]string{}
	for service, container := range mappings {
		images[service] = artifacts[container]
	}
	return domaindocker.PreparedDeliveryProject{HostID: "host", ProjectID: "project", ProjectDigest: "frozen-project", RenderedDigest: "rendered", Ciphertext: "prepared", ExpectedServices: []string{"api"}, Images: images}, nil
}
func (*batchDockerFixture) ValidateDeliveryProject(context.Context, domainidentity.Principal, sohaapi.DockerDeliverySnapshot, string) error {
	return nil
}
func (f *batchDockerFixture) QueueDeliveryProject(ctx context.Context, p domainidentity.Principal, snapshot sohaapi.DockerDeliverySnapshot, _ string, action string) (domaindocker.Operation, error) {
	if f.queueErr != nil {
		return domaindocker.Operation{}, f.queueErr
	}
	id, err := appdocker.DeliveryProjectOperationID(p, snapshot, action)
	if err != nil {
		return domaindocker.Operation{}, err
	}
	if item, ok := f.operations[id]; ok {
		return item, nil
	}
	node, _ := domainworkflow.NodeExecutionFrom(ctx)
	item := domaindocker.Operation{ID: id, HostID: snapshot.HostID, ProjectID: snapshot.ProjectID, OperationKind: "project_deploy", Status: "queued", Payload: node.Metadata(map[string]any{"action": action, "deliveryPlanId": snapshot.DeliveryPlanID, "releaseTargetId": snapshot.TargetID, "renderedDigest": snapshot.RenderedDigest})}
	f.operations[id] = item
	return item, nil
}

func TestDockerPreflightObservesReceiptWhileHostUnavailable(t *testing.T) {
	p := deliveryActionPrincipal()
	snapshot := sohaapi.DockerDeliverySnapshot{DeliveryPlanID: "plan", TargetID: "target", HostID: "host", ProjectID: "project", ProjectDigest: "frozen", RenderedDigest: "rendered"}
	var err error
	snapshot.PreflightOperationID, err = appdocker.DeliveryProjectOperationID(p, snapshot, "validate")
	if err != nil {
		t.Fatal(err)
	}
	plan := domaindelivery.DeliveryPlan{DockerSnapshots: []sohaapi.DockerDeliverySnapshot{snapshot}}
	frozen := domainworkflow.DeliveryTargetSnapshot{FrozenDocker: &domaindocker.PreparedDeliveryProject{HostID: "host", ProjectID: "project", ProjectDigest: "frozen"}}
	for _, status := range []string{"failed", "callback_timeout", "running"} {
		t.Run(status, func(t *testing.T) {
			f := &batchDockerFixture{operations: map[string]domaindocker.Operation{}, queueErr: apperrors.ErrClusterUnready}
			f.operations[snapshot.PreflightOperationID] = domaindocker.Operation{ID: snapshot.PreflightOperationID, HostID: "host", ProjectID: "project", OperationKind: "project_deploy", Status: status, Payload: map[string]any{"action": "validate", "deliveryPlanId": "plan", "releaseTargetId": "target", "renderedDigest": "rendered"}}
			s := &Service{docker: f}
			node, err := s.batchDockerPreflight(context.Background(), p, frozen, plan, domainworkflow.NodeRun{})
			want := "failed"
			if status == "running" {
				want = "waiting_execution"
			}
			if err != nil || node.Status != want || node.DockerOperationID != snapshot.PreflightOperationID {
				t.Fatalf("receipt was hidden by host readiness: %+v %v", node, err)
			}
			delete(f.operations, snapshot.PreflightOperationID)
			if _, err := s.batchDockerPreflight(context.Background(), p, frozen, plan, node); !errors.Is(err, apperrors.ErrClusterUnready) {
				t.Fatalf("missing operation bypassed dispatch readiness: %v", err)
			}
		})
	}
}
func (f *batchDockerFixture) GetOperation(_ context.Context, _ domainidentity.Principal, id string) (domaindocker.Operation, error) {
	item, ok := f.operations[id]
	if !ok {
		return item, apperrors.ErrNotFound
	}
	return item, nil
}
func (f *batchDockerFixture) CancelDeliveryProject(_ context.Context, id, _, _ string) (domaindocker.Operation, error) {
	item, ok := f.operations[id]
	if !ok {
		return item, apperrors.ErrNotFound
	}
	if item.Status != "completed" {
		item.Status = "canceling"
		f.operations[id] = item
	}
	return item, nil
}
func (f *batchDockerFixture) AssessProject(_ context.Context, _ domainidentity.Principal, input appdocker.ProjectAssessmentInput) (domainaigateway.CapabilityAssessment, error) {
	f.lastAssessment = input
	return domainaigateway.CapabilityAssessment{Verdict: f.verdict, Summary: "fresh expected Docker runtime evidence"}, nil
}
func (f *batchDockerFixture) complete(id string) {
	item := f.operations[id]
	item.Status = "completed"
	item.Result = map[string]any{"validatedRenderedDigest": item.Payload["renderedDigest"], "appliedRenderedDigest": item.Payload["renderedDigest"]}
	f.operations[id] = item
}

func TestDockerBatchUsesPlanApprovalAndDomainRecovery(t *testing.T) {
	service, repo, _, _, catalog, run, batch := batchRuntimeCheck(t)
	f := &batchDockerFixture{operations: map[string]domaindocker.Operation{}, verdict: "inconclusive"}
	service.SetDockerDelivery(f)
	target := &catalog.bindings[0].Targets[0]
	target.ExecutorKind = "docker_compose"
	target.TargetKind = "host_service"
	target.ClusterID = ""
	target.Namespace = ""
	target.Docker = &sohaapi.DockerDeliveryConfiguration{HostID: "host", ProjectID: "project", ImageMappings: map[string]string{"api": "api"}}
	snapshot, err := service.FreezeDeliveryTarget(context.Background(), deliveryActionPrincipal(), batch.Targets[0].Target)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.BuildNodeID = "web:build"
	batch.Targets[0] = snapshot
	if len(f.operations) != 0 {
		t.Fatal("freezing created Docker work")
	}
	batchRuntimeStep(t, service, &run, batch, 0, "waiting_execution")
	repo.status(run.NodeRuns[0].ExecutionTaskID, "completed")
	batchRuntimeStep(t, service, &run, batch, 0, "completed")
	batchRuntimeStep(t, service, &run, batch, 1, "waiting_execution")
	batchRuntimeStep(t, service, &run, batch, 1, "waiting_execution")
	if len(f.operations) != 1 || len(repo.plan.DockerSnapshots) != 1 {
		t.Fatal("preflight duplicated or snapshot absent")
	}
	saved := repo.plan.DockerSnapshots[0]
	f.complete(saved.PreflightOperationID)
	batchRuntimeStep(t, service, &run, batch, 1, "waiting_approval")
	if _, err := service.ConfirmDeliveryPlan(context.Background(), deliveryActionPrincipal(), repo.plan.ID); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("public plan bypass: %v", err)
	}
	if _, err := service.DecideDeliveryPlanApproval(context.Background(), deliveryActionPrincipal(), repo.plan.ID, domaindelivery.DeliveryPlanApprovalInput{Action: "approve"}); err != nil {
		t.Fatal(err)
	}
	batchRuntimeStep(t, service, &run, batch, 1, "completed")
	batchRuntimeStep(t, service, &run, batch, 2, "waiting_execution")
	// Rebuild the adapter and lose the in-memory node receipt after dispatch.
	copyService := *service
	service = &copyService
	run.NodeRuns[2].DockerOperationID = ""
	repo.plan.Status = "confirming"
	batchRuntimeStep(t, service, &run, batch, 2, "waiting_execution")
	if repo.plan.Status != "confirmed" {
		t.Fatalf("lost plan confirmation was not recovered: %s", repo.plan.Status)
	}
	if len(f.operations) != 2 || repo.createCount != 1 {
		t.Fatal("recovery duplicated plan or domain operation")
	}
	f.complete(saved.DeployOperationID)
	batchRuntimeStep(t, service, &run, batch, 2, "completed")
	batchRuntimeStep(t, service, &run, batch, 3, "waiting_execution")
	f.verdict = "satisfied"
	batchRuntimeStep(t, service, &run, batch, 3, "completed")
	if f.lastAssessment.ExpectedImages["api"] != saved.Images["api"] || f.lastAssessment.AfterOperationID != saved.DeployOperationID {
		t.Fatal("runtime assessment lost artifact or operation identity")
	}
	stopped, err := service.CancelDeliveryStage(context.Background(), run, batch, run.NodeRuns[1])
	if err != nil || stopped.Status != "canceled" || len(f.operations) != 2 {
		t.Fatalf("stop after preflight: %+v %v", stopped, err)
	}
}
