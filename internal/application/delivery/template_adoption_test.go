package delivery

import (
	"context"
	"errors"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainupdate "github.com/opensoha/soha/internal/domain/templateupdate"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"testing"
)

type adoptionOwner struct {
	frozen      domainupdate.FrozenCandidate
	err         error
	applied     int
	approvalErr error
}

func (o *adoptionOwner) Candidate(context.Context, domainidentity.Principal, string, bool) (domainupdate.FrozenCandidate, error) {
	return o.frozen, o.err
}
func (o *adoptionOwner) ReadCandidate(context.Context, domainidentity.Principal, string) (domainupdate.FrozenCandidate, error) {
	return o.frozen, o.err
}
func (o *adoptionOwner) AuthorizeApproval(context.Context, domainidentity.Principal, string) error {
	return o.approvalErr
}
func (o *adoptionOwner) Apply(ctx context.Context, _ domainidentity.Principal, _, _ string, receipt func(context.Context) error) error {
	o.applied++
	return receipt(ctx)
}

func TestTemplateAdoptionPlanUsesApprovalAndNoRuntimeTask(t *testing.T) {
	s, repo, builder, manifest, _, run, batch := batchRuntimeCheck(t)
	owner := &adoptionOwner{frozen: domainupdate.FrozenCandidate{PreviewID: "preview", TemplateDigest: "source", Candidate: sohaapi.TemplateUpdateCandidate{ID: "candidate", Digest: "frozen", Eligible: true, RequiresApproval: true, Target: sohaapi.TemplateUpdateTarget{ID: "binding", ApplicationID: "app", ServiceID: "svc", ApplicationEnvironmentID: "env", ReleaseTargetID: "target", TemplateID: "template", FromVersion: 1}, ToVersion: 4}}}
	s.SetTemplateUpdates(owner)
	target := domainworkflow.DeliveryTargetInput{ID: "web", ApplicationID: "app", ServiceID: "svc", ApplicationEnvironmentID: "env", ReleaseTargetID: "target", Action: "config_update", TemplateCandidateID: "candidate"}
	if _, err := s.freezeTemplateAdoption(context.Background(), deliveryActionPrincipal(), target); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("raw batch bypassed reviewed preview receipt")
	}
	snapshot, err := s.freezeTemplateAdoption(domainupdate.WithPreviewBatch(context.Background(), "preview"), deliveryActionPrincipal(), target)
	if err != nil {
		t.Fatal(err)
	}
	batch.Targets = []domainworkflow.DeliveryTargetSnapshot{snapshot}
	if _, err := s.CreateDeliveryPlan(context.Background(), deliveryActionPrincipal(), domaindelivery.DeliveryPlanInput{Source: "delivery_batch", Action: "deploy", TemplateCandidateID: "candidate"}); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("HTTP request forged a worker Plan")
	}
	batchRuntimeStep(t, s, &run, batch, 1, "waiting_approval")
	if !repo.plan.RequiresApproval || repo.plan.TemplateCandidateID != "candidate" || owner.applied != 0 {
		t.Fatal("approval or frozen identity missing")
	}
	if _, err := s.ConfirmDeliveryPlan(context.Background(), deliveryActionPrincipal(), repo.plan.ID); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("manual confirmation bypassed worker")
	}
	owner.approvalErr = apperrors.ErrAccessDenied
	if _, err := s.DecideDeliveryPlanApproval(context.Background(), deliveryActionPrincipal(), repo.plan.ID, domaindelivery.DeliveryPlanApprovalInput{Action: "approve"}); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatal("scoped approval bypassed")
	}
	owner.approvalErr = nil
	if _, err := s.DecideDeliveryPlanApproval(context.Background(), deliveryActionPrincipal(), repo.plan.ID, domaindelivery.DeliveryPlanApprovalInput{Action: "approve"}); err != nil {
		t.Fatal(err)
	}
	batchRuntimeStep(t, s, &run, batch, 1, "completed")
	batchRuntimeStep(t, s, &run, batch, 2, "completed")
	if repo.plan.Status != "confirmed" || owner.applied != 1 || builder.created != 0 || manifest.created != 0 || len(repo.tasks) != 0 {
		t.Fatal("adoption did not complete as configuration only")
	}
}

func (*adoptionOwner) AssessRelease(context.Context, domainidentity.Principal, sohaapi.TemplateUpdateKind, string, int64, int64) (*sohaapi.TemplateMigrationAssessment, error) {
	return nil, nil
}
func (*adoptionOwner) ReleaseReference(context.Context, domainidentity.Principal, string, int64) (*sohaapi.TemplateVersionReference, error) {
	return nil, nil
}
