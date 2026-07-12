package directorysync

import (
	"context"
	"testing"
	"time"

	domain "github.com/opensoha/soha/internal/domain/directorysync"
)

type repositoryStub struct {
	connection      domain.Connection
	policy          domain.Policy
	recoveredEvents int
	recoveredRuns   int
}

func (r *repositoryStub) CreateConnection(context.Context, domain.Connection, domain.Policy) (domain.Connection, error) {
	return r.connection, nil
}
func (r *repositoryStub) UpdateConnection(context.Context, domain.Connection, domain.Policy) (domain.Connection, error) {
	return r.connection, nil
}
func (r *repositoryStub) GetConnection(context.Context, string) (domain.Connection, domain.Policy, error) {
	return r.connection, r.policy, nil
}
func (*repositoryStub) ListConnections(context.Context) ([]domain.Connection, error) { return nil, nil }
func (*repositoryStub) ListRuns(context.Context, string, int) ([]domain.Run, error)  { return nil, nil }
func (*repositoryStub) GetActiveRun(context.Context, string) (domain.Run, error) {
	return domain.Run{}, nil
}
func (*repositoryStub) ListConflicts(context.Context, string, int) ([]domain.Conflict, error) {
	return nil, nil
}
func (*repositoryStub) ResolveConflict(context.Context, string, string, string, time.Time) error {
	return nil
}
func (*repositoryStub) UnlinkIdentity(context.Context, string, string, time.Time) (domain.IdentityLinkSuppression, error) {
	return domain.IdentityLinkSuppression{}, nil
}
func (*repositoryStub) SetWebhookCredential(context.Context, domain.WebhookCredential) error {
	return nil
}
func (*repositoryStub) GetWebhookCredential(context.Context, string) (domain.WebhookCredential, error) {
	return domain.WebhookCredential{}, nil
}
func (*repositoryStub) EnqueueEvent(context.Context, domain.EventEnvelope) (bool, error) {
	return true, nil
}
func (*repositoryStub) ClaimEvents(context.Context, int) ([]domain.EventEnvelope, error) {
	return nil, nil
}
func (*repositoryStub) CompleteEvent(context.Context, string, string, string, time.Time) error {
	return nil
}

func (r *repositoryStub) RecoverStaleEvents(context.Context, time.Time, time.Time) (int64, error) {
	r.recoveredEvents++
	return 0, nil
}
func (r *repositoryStub) RecoverStaleRuns(context.Context, time.Time, time.Time) (int64, error) {
	r.recoveredRuns++
	return 0, nil
}
func (*repositoryStub) SetSCIMToken(context.Context, string, string, time.Time) error { return nil }
func (*repositoryStub) ResolveSCIMConnection(context.Context, string) (string, error) { return "", nil }
func (*repositoryStub) SetSCIMTokenScoped(context.Context, string, string, []string, time.Time) error {
	return nil
}
func (*repositoryStub) ResolveSCIMConnectionForScope(context.Context, string, string) (string, error) {
	return "", nil
}
func (*repositoryStub) UpsertSCIMOrganization(context.Context, string, domain.Organization) error {
	return nil
}
func (*repositoryStub) DeleteSCIMOrganization(context.Context, string, string) error  { return nil }
func (*repositoryStub) UpsertSCIMPerson(context.Context, string, domain.Person) error { return nil }
func (*repositoryStub) DeleteSCIMPerson(context.Context, string, string) error        { return nil }
func (*repositoryStub) ReplaceSCIMMemberships(context.Context, string, string, []string) error {
	return nil
}
func (*repositoryStub) SCIMSnapshot(context.Context, string) (domain.Snapshot, error) {
	return domain.Snapshot{}, nil
}
func (*repositoryStub) SetConnectionCredential(context.Context, domain.ConnectionCredential) error {
	return nil
}
func (*repositoryStub) GetConnectionCredential(context.Context, string) (domain.ConnectionCredential, error) {
	return domain.ConnectionCredential{}, nil
}
func (*repositoryStub) CreateRun(_ context.Context, x domain.Run) (domain.Run, error) { return x, nil }
func (*repositoryStub) GetRun(context.Context, string) (domain.Run, error)            { return domain.Run{}, nil }
func (*repositoryStub) TransitionRun(context.Context, string, string, domain.RunStats, string, string) error {
	return nil
}
func (*repositoryStub) ListOrganizations(context.Context, string) ([]domain.Organization, error) {
	return nil, nil
}
func (*repositoryStub) ListPeople(context.Context, string) ([]domain.Person, error) { return nil, nil }
func (*repositoryStub) ListMemberships(context.Context, string) ([]domain.Membership, error) {
	return nil, nil
}
func (*repositoryStub) ApplyProjections(context.Context, string, []domain.Organization, []domain.Person, []domain.Membership, bool) error {
	return nil
}
func (*repositoryStub) CreateSuppression(context.Context, domain.IdentityLinkSuppression) error {
	return nil
}
func (*repositoryStub) FindActiveSuppression(context.Context, string, string, string, string) (*domain.IdentityLinkSuppression, error) {
	return nil, nil
}
func (*repositoryStub) ClearSuppression(context.Context, string, string, time.Time) error { return nil }

type connectorSpy struct{ peopleCalls, membershipCalls int }

func (*connectorSpy) Validate(context.Context, domain.Connection) (domain.Capabilities, error) {
	return domain.Capabilities{}, nil
}
func (*connectorSpy) ListOrganizations(context.Context, domain.Connection) ([]domain.Organization, string, error) {
	return []domain.Organization{{ExternalID: "root"}}, "cursor", nil
}
func (s *connectorSpy) ListPeople(context.Context, domain.Connection) ([]domain.Person, error) {
	s.peopleCalls++
	return []domain.Person{{ExternalID: "p"}}, nil
}
func (s *connectorSpy) ListMemberships(context.Context, domain.Connection) ([]domain.Membership, error) {
	s.membershipCalls++
	return nil, nil
}

func TestPullSnapshotDoesNotCallPeopleAPIsWhenDisabled(t *testing.T) {
	repo := &repositoryStub{connection: domain.Connection{ID: "c"}, policy: domain.DefaultPolicy("c")}
	connector := &connectorSpy{}
	snapshot, cursor, err := New(repo, nil).PullSnapshot(context.Background(), "c", connector)
	if err != nil {
		t.Fatal(err)
	}
	if cursor != "cursor" || len(snapshot.Organizations) != 1 {
		t.Fatalf("unexpected snapshot: %+v %s", snapshot, cursor)
	}
	if connector.peopleCalls != 0 || connector.membershipCalls != 0 {
		t.Fatal("people APIs called while disabled")
	}
}

func TestPullSnapshotCallsPeopleAPIsWhenEnabled(t *testing.T) {
	policy := domain.DefaultPolicy("c")
	policy.SyncPeople = true
	repo := &repositoryStub{connection: domain.Connection{ID: "c"}, policy: policy}
	connector := &connectorSpy{}
	snapshot, _, err := New(repo, nil).PullSnapshot(context.Background(), "c", connector)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.People) != 1 || connector.peopleCalls != 1 || connector.membershipCalls != 1 {
		t.Fatalf("people gate did not open: %+v", snapshot)
	}
}
