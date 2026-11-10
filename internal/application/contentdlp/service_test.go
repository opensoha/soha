package contentdlp

import (
	"context"
	"errors"
	"testing"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domain "github.com/opensoha/soha/internal/domain/contentdlp"
	domaindocker "github.com/opensoha/soha/internal/domain/docker"
	"github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type memoryStore struct {
	policy    domain.Policy
	events    []domain.Event
	commitErr error
	records   map[string]domain.EventRecord
	proofs    map[string]*domain.Proof
}

func (s *memoryStore) Policy(context.Context, string) (domain.Policy, error) { return s.policy, nil }
func (s *memoryStore) Save(context.Context, domain.Policy, domain.Event) (domain.Policy, error) {
	return domain.Policy{}, nil
}
func (s *memoryStore) Publish(context.Context, string, int64, domain.Event) (domain.Policy, error) {
	return domain.Policy{}, nil
}
func (s *memoryStore) Commit(_ context.Context, e domain.Event, _ time.Time) error {
	if s.commitErr != nil {
		return s.commitErr
	}
	s.events = append(s.events, e)
	return nil
}
func (s *memoryStore) Events(context.Context, string) ([]domain.Event, error) { return s.events, nil }

func (s *memoryStore) Within(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}
func (s *memoryStore) Event(_ context.Context, _, id string) (domain.EventRecord, error) {
	item, ok := s.records[id]
	if !ok {
		return item, apperrors.ErrNotFound
	}
	return item, nil
}
func (s *memoryStore) Proof(_ context.Context, hash string) (*domain.Proof, error) {
	return s.proofs[hash], nil
}
func (s *memoryStore) CommitTransfer(ctx context.Context, e domain.Event, b domain.Binding, proof *domain.Proof, _ time.Time) (domain.Decision, error) {
	if s.commitErr != nil {
		return domain.Decision{}, s.commitErr
	}
	if proof != nil && s.proofs[proof.TokenHash] != nil {
		e.Allowed = true
		e.RequiredAction = ""
		e.AuthorizationKind = proof.Kind
		e.OriginEventID = proof.Record.Event.ID
		delete(s.proofs, proof.TokenHash)
	}
	if s.records == nil {
		s.records = map[string]domain.EventRecord{}
	}
	s.records[e.ID] = domain.EventRecord{Event: e, Binding: b}
	return e.Decision, s.Commit(ctx, e, time.Time{})
}
func (s *memoryStore) AttachHold(context.Context, string, string) error { return nil }
func (s *memoryStore) IssueAuthorization(_ context.Context, record domain.EventRecord, _, kind, hash string, now time.Time) (time.Time, error) {
	if s.proofs == nil {
		s.proofs = map[string]*domain.Proof{}
	}
	s.proofs[hash] = &domain.Proof{TokenHash: hash, Kind: kind, Record: record}
	return now.Add(time.Minute), nil
}
func (s *memoryStore) SubmitAppeal(context.Context, domain.EventRecord, string, string, time.Time) (domain.Appeal, error) {
	return domain.Appeal{}, nil
}
func (s *memoryStore) ResolveAppeal(context.Context, domain.EventRecord, string, domain.ReviewInput, time.Time, time.Time) (domain.Appeal, error) {
	return domain.Appeal{}, nil
}
func (s *memoryStore) Cases(context.Context, string, string) ([]domain.Case, error) {
	return []domain.Case{}, nil
}

type projectReader struct{ project domaindocker.Project }

func (r projectReader) GetProject(context.Context, string) (domaindocker.Project, error) {
	return r.project, nil
}

type scopeAuthorizer struct{ allowed bool }

type dlpRoleReader struct{}

func (dlpRoleReader) ListRolePermissions(context.Context) (map[string][]string, error) {
	return map[string][]string{"dlp-test": {appaccess.PermDockerServicesView, PermissionReview}}, nil
}

func (s scopeAuthorizer) Authorize(context.Context, domainaccess.Request) (domainaccess.Decision, error) {
	return domainaccess.Decision{Allowed: s.allowed}, nil
}

func TestCheckPersistsBeforeReleaseAndRejectsAuditFailure(t *testing.T) {
	project := domaindocker.Project{ID: "project", HostID: "host", UpdatedAt: time.Now().UTC()}
	policy := domain.DefaultPolicy(project.ID)
	policy.Published = &domain.Content{Enabled: true, Mode: "block", Detectors: []string{"private_key"}, Rules: []domain.Rule{}}
	policy.PublishedVersion = 2
	policy.Revision = 2
	store := &memoryStore{policy: policy}
	service, err := New(store, projectReader{project}, scopeAuthorizer{true}, appaccess.NewPermissionResolver(dlpRoleReader{}))
	if err != nil {
		t.Fatal(err)
	}
	principal := identity.Principal{Roles: []string{"dlp-test"}, UserID: "user", PermissionKeys: []string{appaccess.PermDockerServicesView}}
	file := domaindocker.ProjectVolumeFileContent{ProjectID: project.ID, ServiceName: "app", Target: "/data", Path: "/file.txt", Content: "hello", SizeBytes: 5}
	snapshot := Snapshot{Policy: policy, Project: project}
	d, err := service.Check(context.Background(), principal, snapshot, file)
	if err != nil || d == nil || !d.Allowed || len(store.events) != 1 {
		t.Fatalf("decision=%+v error=%v events=%d", d, err, len(store.events))
	}
	store.commitErr = errors.New("durable event unavailable")
	if d, err := service.Check(context.Background(), principal, snapshot, file); err == nil || d != nil {
		t.Fatal("content released without durable decision")
	}
	store.commitErr = nil
	file.Content = "-----BEGIN PRIVATE KEY-----"
	file.SizeBytes = int64(len(file.Content))
	if d, err := service.Check(context.Background(), principal, snapshot, file); !errors.Is(err, apperrors.ErrAccessDenied) || d != nil || len(store.events) != 2 {
		t.Fatal("matched content not blocked and recorded")
	}
}

func TestCheckRejectsProjectScopeAndPolicyPublicationRace(t *testing.T) {
	project := domaindocker.Project{ID: "p", UpdatedAt: time.Now().UTC()}
	policy := domain.DefaultPolicy(project.ID)
	store := &memoryStore{policy: policy}
	service, err := New(store, projectReader{project}, scopeAuthorizer{false}, appaccess.NewPermissionResolver(dlpRoleReader{}))
	if err != nil {
		t.Fatal(err)
	}
	store.policy.PublishedVersion = 1
	if _, err := service.Check(context.Background(), identity.Principal{}, Snapshot{Policy: policy, Project: project}, domaindocker.ProjectVolumeFileContent{}); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("disabled-to-enabled publication race accepted")
	}
	policy.Published = &domain.Content{Enabled: true, Mode: "block", Detectors: []string{"private_key"}}
	if _, err := service.Check(context.Background(), identity.Principal{Roles: []string{"dlp-test"}, UserID: "u", PermissionKeys: []string{appaccess.PermDockerServicesView}}, Snapshot{Policy: policy, Project: project}, domaindocker.ProjectVolumeFileContent{}); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatal("project scope denial ignored")
	}
}
