package deliverygovernance

import (
	"context"
	"errors"
	"testing"

	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

type evidenceFake struct {
	bundle    domaindelivery.ReleaseBundle
	tasks     []domaindelivery.ExecutionTask
	artifacts []domaindelivery.ExecutionArtifact
	err       error
}

func (f *evidenceFake) GetReleaseBundle(context.Context, string) (domaindelivery.ReleaseBundle, error) {
	return f.bundle, f.err
}
func (f *evidenceFake) ListExecutionTasks(context.Context, domaindelivery.ExecutionTaskFilter) ([]domaindelivery.ExecutionTask, error) {
	return f.tasks, f.err
}
func (f *evidenceFake) ListArtifacts(context.Context, domaindelivery.ArtifactFilter) ([]domaindelivery.ExecutionArtifact, error) {
	return f.artifacts, f.err
}

type auditFake struct {
	entries []domainaudit.Entry
	err     error
}

func (f *auditFake) Record(_ context.Context, entry domainaudit.Entry) error {
	f.entries = append(f.entries, entry)
	return f.err
}

func governanceRequest() Request {
	return Request{
		PlanID:                   "plan-1",
		ApplicationID:            "app-1",
		ApplicationEnvironmentID: "binding-1",
		Action:                   "deploy",
		ReleaseBundleID:          "bundle-1",
		RequiresValidation:       true,
		RequiresApproval:         true,
		ApprovalStatus:           "approved",
		AIStatus:                 "available",
	}
}

func governanceEvidence() *evidenceFake {
	return &evidenceFake{
		bundle: domaindelivery.ReleaseBundle{
			ID:                       "bundle-1",
			ApplicationID:            "app-1",
			ApplicationEnvironmentID: "binding-1",
			Status:                   "completed",
			ArtifactDigest:           "sha256:abc",
		},
		tasks:     []domaindelivery.ExecutionTask{{ID: "task-verify", TaskKind: "verify", Status: "completed"}},
		artifacts: []domaindelivery.ExecutionArtifact{{ID: "report-1", Kind: "test_report", Status: "passed"}},
	}
}

func TestEvaluateAllowsCompleteCandidateAndAuditsEvidence(t *testing.T) {
	audit := &auditFake{}
	service, err := New(governanceEvidence(), audit)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	decision, err := service.Evaluate(context.Background(), domainidentity.Principal{UserID: "u-1", UserName: "tester"}, governanceRequest())
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if !decision.Allowed || decision.Status != DecisionAllowed {
		t.Fatalf("decision = %+v, want allowed", decision)
	}
	if decision.Evidence.CandidateDigest != "sha256:abc" || len(decision.Evidence.ValidationTaskIDs) != 1 || len(decision.Evidence.ArtifactIDs) != 1 {
		t.Fatalf("evidence = %+v, want bundle/task/report evidence", decision.Evidence)
	}
	if len(audit.entries) != 1 || audit.entries[0].Result != DecisionAllowed {
		t.Fatalf("audit entries = %+v, want one allowed entry", audit.entries)
	}
}

func TestEvaluateBlocksFailedValidationAndArtifact(t *testing.T) {
	evidence := governanceEvidence()
	evidence.tasks[0].Status = "failed"
	evidence.artifacts[0].Metadata = map[string]any{"conclusion": "failed"}
	audit := &auditFake{}
	service, _ := New(evidence, audit)
	decision, err := service.Evaluate(context.Background(), domainidentity.Principal{UserID: "u-1"}, governanceRequest())
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if decision.Allowed || decision.Status != DecisionBlocked || len(decision.Reasons) < 2 {
		t.Fatalf("decision = %+v, want blocked with task and artifact reasons", decision)
	}
	if audit.entries[0].Result != DecisionBlocked {
		t.Fatalf("audit result = %q, want blocked", audit.entries[0].Result)
	}
}

func TestEvaluateReportsPendingValidation(t *testing.T) {
	evidence := governanceEvidence()
	evidence.tasks[0].Status = "running"
	service, _ := New(evidence, &auditFake{})
	decision, err := service.Evaluate(context.Background(), domainidentity.Principal{}, governanceRequest())
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if decision.Allowed || decision.Status != DecisionPending {
		t.Fatalf("decision = %+v, want pending", decision)
	}
}

func TestEvaluateFailsClosedWhenAuditFails(t *testing.T) {
	audit := &auditFake{err: errors.New("audit unavailable")}
	service, _ := New(governanceEvidence(), audit)
	if _, err := service.Evaluate(context.Background(), domainidentity.Principal{}, governanceRequest()); err == nil {
		t.Fatal("Evaluate() returned nil error when audit failed")
	}
}

func TestNewRejectsMissingDependencies(t *testing.T) {
	if _, err := New(nil, &auditFake{}); err == nil {
		t.Fatal("New(nil, audit) returned nil error")
	}
	if _, err := New(governanceEvidence(), nil); err == nil {
		t.Fatal("New(evidence, nil) returned nil error")
	}
}
