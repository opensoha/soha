package contentdlp

import (
	"context"
	"errors"
	"testing"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domain "github.com/opensoha/soha/internal/domain/contentdlp"
	domaindocker "github.com/opensoha/soha/internal/domain/docker"
	"github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type stepUpStub struct {
	fresh map[string]bool
	err   error
}

func (s *stepUpStub) RequireRecentStepUp(_ context.Context, user, session string) error {
	if s.err != nil {
		return s.err
	}
	if user != "actor" || !s.fresh[session] {
		return apperrors.ErrMFARequired
	}
	return nil
}

func TestDLPReleaseRequiresFreshMFAAndIssuingSession(t *testing.T) {
	ctx := context.Background()
	project := domaindocker.Project{ID: "p", HostID: "h", UpdatedAt: time.Now().UTC()}
	policy := domain.DefaultPolicy(project.ID)
	policy.Published = &domain.Content{Enabled: true, Mode: "warn", RequireStepUp: true, Detectors: []string{"private_key"}, Rules: []domain.Rule{}}
	policy.PublishedVersion = 2
	store := &memoryStore{policy: policy}
	mfa := &stepUpStub{fresh: map[string]bool{}}
	service, err := New(store, projectReader{project}, scopeAuthorizer{true}, appaccess.NewPermissionResolver(dlpRoleReader{}), WithStepUp(mfa))
	if err != nil {
		t.Fatal(err)
	}
	p := identity.Principal{Roles: []string{"dlp-test"}, UserID: "actor", PermissionKeys: []string{appaccess.PermDockerServicesView}}
	file := domaindocker.ProjectVolumeFileContent{ProjectID: "p", ServiceName: "app", Target: "/data", Path: "/key.txt", Content: "-----BEGIN PRIVATE KEY-----"}
	file.SizeBytes = int64(len(file.Content))
	snapshot := Snapshot{Policy: policy, Project: project}
	_, err = service.Check(ctx, p, snapshot, file)
	var denied *Denial
	if !errors.As(err, &denied) || !denied.Decision.StepUpRequired || denied.Decision.RequiredAction != "acknowledge" {
		t.Fatalf("expected MFA plus acknowledgement: %v", err)
	}
	in := domain.AuthorizationInput{Action: "acknowledge", SessionID: "session-a"}
	if _, err := service.Authorize(ctx, p, "p", denied.Decision.EventID, in); !errors.Is(err, apperrors.ErrMFARequired) || len(store.proofs) != 0 {
		t.Fatal("issued a token without recent MFA", err)
	}
	mfa.fresh["session-a"] = true
	grant, err := service.Authorize(ctx, p, "p", denied.Decision.EventID, in)
	if err != nil {
		t.Fatal(err)
	}
	mfa.fresh["session-b"] = true
	if d, err := service.Check(ctx, p, snapshot, file, domain.TransferAccess{Token: grant.Token, SessionID: "session-b"}); !errors.Is(err, apperrors.ErrAccessDenied) || d != nil {
		t.Fatal("token crossed authenticated sessions", d, err)
	}
	mfa.fresh["session-a"] = false
	if d, err := service.Check(ctx, p, snapshot, file, domain.TransferAccess{Token: grant.Token, SessionID: "session-a"}); !errors.Is(err, apperrors.ErrMFARequired) || d != nil {
		t.Fatal("expired MFA released content", d, err)
	}
	mfa.err = errors.New("MFA database unavailable")
	if d, err := service.Check(ctx, p, snapshot, file, domain.TransferAccess{Token: grant.Token, SessionID: "session-a"}); err == nil || d != nil {
		t.Fatal("MFA error released content", d, err)
	}
	mfa.err = nil
	mfa.fresh["session-a"] = true
	d, err := service.Check(ctx, p, snapshot, file, domain.TransferAccess{Token: grant.Token, SessionID: "session-a"})
	if err != nil || d == nil || !d.Allowed || d.AuthorizationKind != "acknowledge" {
		t.Fatalf("verified exact release rejected: %+v %v", d, err)
	}
	if d, err := service.Check(ctx, p, snapshot, file, domain.TransferAccess{Token: grant.Token, SessionID: "session-a"}); !errors.Is(err, apperrors.ErrAccessDenied) || d != nil {
		t.Fatal("consumed MFA token reused", d, err)
	}
}

func TestDLPReleaseMFAUnavailableAndMissingSessionFailClosed(t *testing.T) {
	var typedNil *stepUpStub
	for _, verifier := range []StepUpVerifier{nil, typedNil} {
		if err := RequireReleaseMFA(context.Background(), verifier, "actor", "session"); !errors.Is(err, apperrors.ErrServiceUnavailable) {
			t.Fatal("unavailable verifier allowed release", err)
		}
	}
	if err := RequireReleaseMFA(context.Background(), &stepUpStub{}, "actor", ""); !errors.Is(err, apperrors.ErrMFARequired) {
		t.Fatal("sessionless access accepted", err)
	}
	policy := domain.Content{Enabled: true, Mode: "audit", RequireStepUp: true, Detectors: []string{"private_key"}, Rules: []domain.Rule{}}
	if ValidatePolicy(policy) == nil {
		t.Fatal("audit policy accepted an enforced MFA condition")
	}
}

func TestShadowReportsAnAddedMFAReleaseCondition(t *testing.T) {
	published := domain.Content{Enabled: true, Mode: "warn", Detectors: []string{"private_key"}, Rules: []domain.Rule{}}
	draft := published
	draft.RequireStepUp = true
	result, err := Shadow(context.Background(), draft, &published, 1, 1, domain.ShadowInput{ExpectedRevision: 1, Samples: [][]byte{[]byte("-----BEGIN PRIVATE KEY-----")}})
	if err != nil || result.Changed != 1 || !result.Samples[0].Draft.StepUpRequired || result.Samples[0].Published.StepUpRequired {
		t.Fatalf("MFA-only policy change was hidden: %+v %v", result, err)
	}
	draft.Enabled = false
	decision := shadowDecision(context.Background(), draft, []byte("-----BEGIN PRIVATE KEY-----"))
	if !decision.Allowed || decision.StepUpRequired {
		t.Fatalf("disabled policy still requires MFA: %+v", decision)
	}
}
