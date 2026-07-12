package directorysync

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrInvalidPolicy      = errors.New("invalid directory sync policy")
	ErrPeopleSyncDisabled = errors.New("directory people sync is disabled")
	ErrInvalidRunState    = errors.New("invalid directory sync run state")
	ErrInvalidSnapshot    = errors.New("invalid directory snapshot")
)

const (
	ProviderFeishu   = "feishu"
	ProviderWeCom    = "wecom"
	ProviderDingTalk = "dingtalk"
	ProviderLDAP     = "ldap"
	ProviderSCIM     = "scim"
	ProviderCustom   = "custom"

	ConnectionPending  = "pending"
	ConnectionHealthy  = "healthy"
	ConnectionDegraded = "degraded"
	ConnectionDisabled = "disabled"

	PolicyManual               = "manual"
	PolicyScheduled            = "scheduled"
	PolicyScheduledAndRealtime = "scheduled_and_realtime"
	ProvisionCreateAndLink     = "create_and_link"
	ProvisionReviewBeforeLink  = "review_before_link"
	DisableManagedOnly         = "managed_only"
	DisableNever               = "never"
	MissingArchive             = "archive"

	ProjectionActive    = "active"
	ProjectionSuspended = "suspended"
	ProjectionArchived  = "archived"

	RunQueued    = "queued"
	RunRunning   = "running"
	RunSucceeded = "succeeded"
	RunPartial   = "partial"
	RunFailed    = "failed"
	RunCanceled  = "canceled"

	ChangeCreate  = "create"
	ChangeUpdate  = "update"
	ChangeArchive = "archive"
	ChangeRestore = "restore"
	ChangeMove    = "move"
	ChangeNoop    = "noop"
)

type Capabilities struct {
	Organizations bool `json:"organizations"`
	People        bool `json:"people"`
	Memberships   bool `json:"memberships"`
	Events        bool `json:"events"`
}

type Connection struct {
	ID, Name, ProviderType, LoginProviderID, CredentialRef, Status string
	Enabled                                                        bool
	Capabilities                                                   Capabilities
	Metadata                                                       map[string]any
	LastValidatedAt                                                *time.Time
	CreatedBy, UpdatedBy                                           string
	CreatedAt, UpdatedAt                                           time.Time
}

type Policy struct {
	ConnectionID                                         string
	SyncOrganizations                                    bool
	SyncPeople                                           bool
	Mode, Schedule, FullReconcileSchedule, ProvisionMode string
	TrustedEmailDomains                                  []string
	VerifiedEmailAutoLink                                bool
	UserDisablePolicy, MissingObjectPolicy               string
	FieldMappings                                        map[string]string
	UpdatedBy                                            string
	UpdatedAt                                            time.Time
}

func DefaultPolicy(connectionID string) Policy {
	return Policy{ConnectionID: strings.TrimSpace(connectionID), SyncOrganizations: true, SyncPeople: false,
		Mode: PolicyScheduled, Schedule: "0 * * * *", ProvisionMode: ProvisionReviewBeforeLink, UserDisablePolicy: DisableManagedOnly,
		MissingObjectPolicy: MissingArchive, FieldMappings: map[string]string{}}
}

func (p Policy) Validate() error {
	if strings.TrimSpace(p.ConnectionID) == "" || !p.SyncOrganizations {
		return fmt.Errorf("%w: organizations must be enabled", ErrInvalidPolicy)
	}
	if p.Mode != PolicyManual && p.Mode != PolicyScheduled && p.Mode != PolicyScheduledAndRealtime {
		return fmt.Errorf("%w: unsupported mode %q", ErrInvalidPolicy, p.Mode)
	}
	if p.ProvisionMode != ProvisionCreateAndLink && p.ProvisionMode != ProvisionReviewBeforeLink {
		return fmt.Errorf("%w: unsupported provision mode %q", ErrInvalidPolicy, p.ProvisionMode)
	}
	if p.UserDisablePolicy != DisableManagedOnly && p.UserDisablePolicy != DisableNever {
		return fmt.Errorf("%w: unsupported user disable policy %q", ErrInvalidPolicy, p.UserDisablePolicy)
	}
	if p.MissingObjectPolicy != MissingArchive {
		return fmt.Errorf("%w: unsupported missing object policy", ErrInvalidPolicy)
	}
	return nil
}

type Organization struct {
	ID, ConnectionID, ExternalID, ExternalParentID, LocalTeamID, Name, Path, Status, SourceVersion, RawHash string
	FirstSeenAt, LastSeenAt                                                                                 time.Time
	ArchivedAt                                                                                              *time.Time
}

type Person struct {
	ID, ConnectionID, ExternalID, ProviderSubject, LocalUserID, Username, DisplayName, Email, Phone, AvatarURL string
	EmailVerified                                                                                              bool
	Status, SourceVersion, RawHash                                                                             string
	FirstSeenAt, LastSeenAt                                                                                    time.Time
	ArchivedAt                                                                                                 *time.Time
}

type Membership struct {
	ConnectionID, ExternalPersonID, ExternalOrganizationID, LocalUserID, LocalTeamID, Status string
	LastSeenAt                                                                               time.Time
}

type IdentityLinkSuppression struct {
	ID, UserID, ProviderType, ProviderID, ProviderUserID, Reason, CreatedBy, ClearedBy string
	CreatedAt                                                                          time.Time
	ClearedAt                                                                          *time.Time
}

type Conflict struct {
	ID, ConnectionID, ObjectType, ExternalID, Reason, Status, Resolution, ResolvedBy string
	CreatedAt                                                                        time.Time
	ResolvedAt                                                                       *time.Time
}

type WebhookCredential struct {
	ConnectionID, VerificationToken, EncryptKey string
}

type ConnectionCredential struct {
	ConnectionID, Username, Password string
}

type EventEnvelope struct {
	ID, ConnectionID, ProviderEventID, EventType, Status, ErrorSummary string
	OccurredAt, ReceivedAt                                             time.Time
	ProcessedAt, ClaimedAt, NextAttemptAt                              *time.Time
	Attempts                                                           int
}

const (
	SCIMScopeRead  = "scim.read"
	SCIMScopeWrite = "scim.write"
)

func (s IdentityLinkSuppression) Active() bool { return s.ClearedAt == nil }

type RunStats struct {
	OrganizationsCreated, OrganizationsUpdated, OrganizationsMoved, OrganizationsArchived   int
	PeopleCreated, PeopleUpdated, PeopleArchived, MembershipsUpdated, Suppressed, Conflicts int
}

type Run struct {
	ID, ConnectionID, Trigger, Mode, Status, CursorBefore, CursorAfter, IdempotencyKey string
	IncludePeople                                                                      bool
	Stats                                                                              RunStats
	ErrorCode, ErrorSummary, RequestedBy                                               string
	StartedAt, HeartbeatAt, FinishedAt                                                 *time.Time
	CreatedAt                                                                          time.Time
}

func CanTransitionRun(from, to string) bool {
	switch from {
	case RunQueued:
		return to == RunRunning || to == RunCanceled
	case RunRunning:
		return to == RunSucceeded || to == RunPartial || to == RunFailed || to == RunCanceled
	case RunPartial, RunFailed:
		return to == RunQueued
	default:
		return false
	}
}

type Snapshot struct {
	Organizations []Organization
	People        []Person
	Memberships   []Membership
}

type Change struct{ Kind, Action, ExternalID, Detail string }
type Plan struct {
	Organizations []Organization
	People        []Person
	Memberships   []Membership
	Changes       []Change
}
