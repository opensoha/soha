package directorysync

import (
	"context"
	"time"
)

type Repository interface {
	CreateConnection(context.Context, Connection, Policy) (Connection, error)
	UpdateConnection(context.Context, Connection, Policy) (Connection, error)
	GetConnection(context.Context, string) (Connection, Policy, error)
	ListConnections(context.Context) ([]Connection, error)
	CreateRun(context.Context, Run) (Run, error)
	GetRun(context.Context, string) (Run, error)
	ListRuns(context.Context, string, int) ([]Run, error)
	GetActiveRun(context.Context, string) (Run, error)
	TransitionRun(context.Context, string, string, RunStats, string, string) error
	ListOrganizations(context.Context, string) ([]Organization, error)
	ListPeople(context.Context, string) ([]Person, error)
	ListMemberships(context.Context, string) ([]Membership, error)
	ApplyProjections(context.Context, string, []Organization, []Person, []Membership, bool) error
	CreateSuppression(context.Context, IdentityLinkSuppression) error
	FindActiveSuppression(context.Context, string, string, string, string) (*IdentityLinkSuppression, error)
	ClearSuppression(context.Context, string, string, time.Time) error
	ListConflicts(context.Context, string, int) ([]Conflict, error)
	ResolveConflict(context.Context, string, string, string, time.Time) error
	UnlinkIdentity(context.Context, string, string, time.Time) (IdentityLinkSuppression, error)
	SetWebhookCredential(context.Context, WebhookCredential) error
	GetWebhookCredential(context.Context, string) (WebhookCredential, error)
	EnqueueEvent(context.Context, EventEnvelope) (bool, error)
	ClaimEvents(context.Context, int) ([]EventEnvelope, error)
	CompleteEvent(context.Context, string, string, string, time.Time) error
	RecoverStaleEvents(context.Context, time.Time, time.Time) (int64, error)
	RecoverStaleRuns(context.Context, time.Time, time.Time) (int64, error)
	SetSCIMToken(context.Context, string, string, time.Time) error
	ResolveSCIMConnection(context.Context, string) (string, error)
	SetSCIMTokenScoped(context.Context, string, string, []string, time.Time) error
	ResolveSCIMConnectionForScope(context.Context, string, string) (string, error)
	UpsertSCIMOrganization(context.Context, string, Organization) error
	DeleteSCIMOrganization(context.Context, string, string) error
	UpsertSCIMPerson(context.Context, string, Person) error
	DeleteSCIMPerson(context.Context, string, string) error
	ReplaceSCIMMemberships(context.Context, string, string, []string) error
	SCIMSnapshot(context.Context, string) (Snapshot, error)
	SetConnectionCredential(context.Context, ConnectionCredential) error
	GetConnectionCredential(context.Context, string) (ConnectionCredential, error)
}
