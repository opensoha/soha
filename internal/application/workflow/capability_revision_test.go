package workflow

import (
	"context"
	"errors"
	"testing"

	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestCapabilityRevisionRetainsHistoryAndRequiresFreshVerification(t *testing.T) {
	service, repo, provider := newCapabilityExecutorFixture(t, "inconclusive")
	tickCapabilityFixture(t, service, repo)
	tickCapabilityFixture(t, service, repo)
	principal, _ := provider.CurrentExecutionPrincipal(context.Background(), "actor", "frozen-token")
	intent, _ := domainworkflow.CapabilityIntentFrom(repo.run)
	input := domainaigateway.CapabilityTaskRevisionInput{ExpectedVersion: repo.run.Version, Plan: intent.Input.Plan}
	if _, err := service.ResumeCapabilityTask(context.Background(), principal, repo.run.ID, input); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("old verification was accepted: %v", err)
	}
	input.Plan.Steps = append([]domainaigateway.CapabilityPlanStep(nil), input.Plan.Steps...)
	input.Plan.Steps[1].ID = "assess-again"
	input.Plan.VerificationSteps = []string{"assess-again"}
	resumed, err := service.ResumeCapabilityTask(context.Background(), principal, repo.run.ID, input)
	if err != nil || resumed.PlanVersion != 2 {
		t.Fatalf("resume: %+v %v", resumed, err)
	}
	if _, err := service.ResumeCapabilityTask(context.Background(), principal, repo.run.ID, input); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("stale revision accepted: %v", err)
	}
	archived, err := service.GetCapabilityTaskRevision(context.Background(), principal, repo.run.ID, 1)
	if err != nil || archived.Status != "inconclusive" || archived.Nodes[1].Invocation.Assessment.Verdict != "inconclusive" {
		t.Fatalf("lost history: %+v %v", archived, err)
	}
	provider.verdict = "satisfied"
	tickCapabilityFixture(t, service, repo)
	if repo.run.Status != "completed" || len(provider.calls) != 3 {
		t.Fatalf("old effects repeated or verification skipped: %v %s", provider.calls, repo.run.Status)
	}
}

func TestCapabilityRevisionCannotDiscardOrChangeUnresolvedEffects(t *testing.T) {
	_, repo, _ := newCapabilityExecutorFixture(t, "satisfied")
	intent, _ := domainworkflow.CapabilityIntentFrom(repo.run)
	repo.run.NodeRuns[0].DispatchAttempted = true
	repo.run.NodeRuns[0].Status = "blocked"
	for _, change := range []string{"remove", "input", "cancel"} {
		t.Run(change, func(t *testing.T) {
			candidate := intent
			candidate.Input.Plan.Steps = append([]domainaigateway.CapabilityPlanStep(nil), intent.Input.Plan.Steps...)
			run := repo.run
			run.NodeRuns = append([]domainworkflow.NodeRun(nil), run.NodeRuns...)
			switch change {
			case "remove":
				candidate.Input.Plan.Steps = candidate.Input.Plan.Steps[1:]
			case "input":
				candidate.Input.Plan.Steps[0].Call.Input = map[string]any{"changed": true}
			case "cancel":
				run.NodeRuns[0].ControlCall = &domainaigateway.ToolInvocationRequest{ToolName: "cancel"}
			}
			if _, err := reviseCapabilityRun(run, candidate); !errors.Is(err, apperrors.ErrConflict) {
				t.Fatalf("unresolved effect changed: %v", err)
			}
		})
	}
}

func TestCapabilityRevisionRetainsOpaqueSecretsWithoutRepeatingEffects(t *testing.T) {
	service, repo, provider := newCapabilityExecutorFixture(t, "inconclusive")
	tickCapabilityFixture(t, service, repo)
	tickCapabilityFixture(t, service, repo)
	intent, _ := domainworkflow.CapabilityIntentFrom(repo.run)
	intent.Input.Plan.Steps[0].Call.SecretRefs = map[string]string{"TOKEN": "soha://secrets/secret-1/versions/1"}
	repo.run.Metadata["capabilityIntent"] = intent
	principal, _ := provider.CurrentExecutionPrincipal(context.Background(), "actor", "frozen-token")
	view, err := service.GetCapabilityTask(context.Background(), principal, repo.run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Plan.Steps[0].Call.SecretRefs) != 0 {
		t.Fatal("public view exposed opaque secret references")
	}
	view.Plan.Steps[1].ID = "fresh-check"
	view.Plan.VerificationSteps = []string{"fresh-check"}
	view.Plan.Steps[0].Call.SecretRefs = map[string]string{}
	if _, err := service.ResumeCapabilityTask(context.Background(), principal, repo.run.ID, domainaigateway.CapabilityTaskRevisionInput{ExpectedVersion: view.Version, Plan: view.Plan}); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("explicit clear changed frozen step: %v", err)
	}
	view.Plan.Steps[0].Call.SecretRefs = nil
	if _, err := service.ResumeCapabilityTask(context.Background(), principal, repo.run.ID, domainaigateway.CapabilityTaskRevisionInput{ExpectedVersion: view.Version, Plan: view.Plan}); err != nil {
		t.Fatal(err)
	}
	resumed, _ := domainworkflow.CapabilityIntentFrom(repo.run)
	if resumed.Input.Plan.Steps[0].Call.SecretRefs["TOKEN"] != "soha://secrets/secret-1/versions/1" {
		t.Fatal("retained step lost its frozen secret reference")
	}
	if len(provider.calls) != 2 {
		t.Fatal("revision repeated an effect")
	}
}
