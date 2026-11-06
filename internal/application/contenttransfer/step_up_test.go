package contenttransfer

import (
	"context"
	"errors"
	"testing"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	appdlp "github.com/opensoha/soha/internal/application/contentdlp"
	access "github.com/opensoha/soha/internal/domain/access"
	dlp "github.com/opensoha/soha/internal/domain/contentdlp"
	d "github.com/opensoha/soha/internal/domain/contenttransfer"
	"github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type releaseStore struct {
	Store
	record  d.Record
	dest    d.Destination
	updates int
}

func (s *releaseStore) Record(context.Context, string) (d.Record, error) { return s.record, nil }
func (s *releaseStore) Destination(context.Context, string) (d.Destination, error) {
	return s.dest, nil
}
func (s *releaseStore) WithCurrent(ctx context.Context, _ []d.Gate, fn func(context.Context) error) error {
	return fn(ctx)
}
func (s *releaseStore) Update(_ context.Context, r d.Record, _ int64, _, _ string) error {
	s.record = r
	s.updates++
	return nil
}

type releaseScope struct{}

type releaseRoles struct{}

func (releaseRoles) ListRolePermissions(context.Context) (map[string][]string, error) {
	return map[string][]string{"transfer-test": {View, Create}}, nil
}

func (releaseScope) Authorize(context.Context, access.Request) (access.Decision, error) {
	return access.Decision{Allowed: true}, nil
}

type releaseMFA struct{ fresh bool }

func (m *releaseMFA) RequireRecentStepUp(_ context.Context, user, session string) error {
	if !m.fresh || user != "actor" || session == "" {
		return apperrors.ErrMFARequired
	}
	return nil
}

func TestManagedReleaseRequiresMFAOnIssueAndUse(t *testing.T) {
	ctx := context.Background()
	id, destination := "11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222"
	p := identity.Principal{UserID: "actor", Roles: []string{"transfer-test"}, PermissionKeys: []string{View, Create}}
	gate := d.Gate{DestinationID: destination, PolicyVersion: 3}
	op := operation{id: "retry", action: "upload", destinationID: destination, name: "secret.txt", key: "object", content: []byte("-----BEGIN PRIVATE KEY-----"), sessionID: "session-a"}
	r := d.Record{ID: id, ActorID: p.UserID, DestinationID: destination, Action: "upload", State: "blocked", Revision: 1, Gates: []d.Gate{gate}, Decision: &dlp.Decision{Status: "matched", RequiredAction: "acknowledge", StepUpRequired: true}, Private: d.Private{Key: make([]byte, 32), Backends: []string{"backend-v1"}, ExpiresAt: time.Now().Add(time.Hour)}}
	r.Private.Tag = operationTag(r.Private.Key, p, op, r.Gates, r.Private.Backends)
	store := &releaseStore{record: r, dest: d.Destination{ID: destination, PublishedVersion: 3}}
	mfa := &releaseMFA{}
	s := &Service{store: store, scope: releaseScope{}, permissions: appaccess.NewPermissionResolver(releaseRoles{}), stepUp: mfa}
	if _, err := s.Authorize(ctx, p, id, "acknowledge", "session-a"); !errors.Is(err, apperrors.ErrMFARequired) || store.updates != 0 {
		t.Fatal("authorization mutated before MFA", err)
	}
	mfa.fresh = true
	grant, err := s.Authorize(ctx, p, id, "acknowledge", "session-a")
	if err != nil {
		t.Fatal(err)
	}
	if store.record.Private.TokenHash == appdlp.ReleaseTokenHash(grant.Token, "", false) {
		t.Fatal("MFA token was not session-bound")
	}
	op.authorization = &d.Authorization{AttemptID: id, Token: grant.Token}
	current := r
	if proof, err := s.proof(ctx, p, op, current); err != nil || proof == nil {
		t.Fatal("verified proof rejected", err)
	}
	op.sessionID = "session-b"
	if proof, err := s.proof(ctx, p, op, current); !errors.Is(err, apperrors.ErrConflict) || proof != nil {
		t.Fatal("cross-session proof accepted", err)
	}
	op.sessionID = "session-a"
	mfa.fresh = false
	if proof, err := s.proof(ctx, p, op, current); !errors.Is(err, apperrors.ErrMFARequired) || proof != nil {
		t.Fatal("expired MFA proof accepted", err)
	}
}

func TestManagedCompositeMFAKeepsStrictGate(t *testing.T) {
	op := operation{name: "file.txt", content: []byte("-----BEGIN PRIVATE KEY-----"), gates: []scopeGate{
		{destination: d.Destination{Published: &d.Config{Policy: d.Policy{Mode: "warn", RequireStepUp: true, Detectors: []string{"private_key"}}}}},
		{destination: d.Destination{Published: &d.Config{Policy: d.Policy{Mode: "block", Detectors: []string{"private_key"}}}}},
	}}
	decision, _ := evaluate(context.Background(), op)
	if decision.Allowed || !decision.StepUpRequired || decision.RequiredAction != "appeal" {
		t.Fatalf("MFA replaced independent approval: %+v", decision)
	}
}
