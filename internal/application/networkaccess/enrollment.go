package networkaccess

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainnetworkruntime "github.com/opensoha/soha/internal/domain/networkruntime"
	"github.com/opensoha/soha/internal/platform/operationentry"
	"github.com/opensoha/soha/internal/platform/requestctx"
)

const defaultEnrollmentTTL = 10 * time.Minute

type EnrollmentStore interface {
	CreateEnrollment(context.Context, domainnetworkruntime.EnrollmentChallenge) error
	GetEnrollment(context.Context, string) (domainnetworkruntime.EnrollmentChallenge, error)
	ListEnrollments(context.Context, int) ([]domainnetworkruntime.EnrollmentChallenge, error)
	RevokeEnrollment(context.Context, string, time.Time) error
}

type EnrollmentInput struct {
	RuntimeID   string
	RuntimeKind string
	DeviceID    string
	SubjectID   string
	TTL         time.Duration
}

type EnrollmentService struct {
	store       EnrollmentStore
	permissions *appaccess.PermissionResolver
	audit       AuditRecorder
	operations  OperationRecorder
	now         func() time.Time
}

func NewEnrollmentService(store EnrollmentStore, permissions *appaccess.PermissionResolver, audit AuditRecorder, operations OperationRecorder) (*EnrollmentService, error) {
	for name, dependency := range map[string]any{
		"store": store, "permissions": permissions, "audit": audit, "operations": operations,
	} {
		if isNilDependency(dependency) {
			return nil, fmt.Errorf("network enrollment service: %s dependency is required", name)
		}
	}
	return &EnrollmentService{store: store, permissions: permissions, audit: audit, operations: operations, now: time.Now}, nil
}

func (s *EnrollmentService) Create(ctx context.Context, principal domainidentity.Principal, input EnrollmentInput) (domainnetworkruntime.EnrollmentSecret, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermNetworkAccessEnrollmentsCreate); err != nil {
		return domainnetworkruntime.EnrollmentSecret{}, err
	}
	input.RuntimeID = strings.TrimSpace(input.RuntimeID)
	input.RuntimeKind = strings.TrimSpace(input.RuntimeKind)
	input.DeviceID = strings.TrimSpace(input.DeviceID)
	input.SubjectID = strings.TrimSpace(input.SubjectID)
	if input.TTL == 0 {
		input.TTL = defaultEnrollmentTTL
	}
	if err := validateEnrollmentInput(principal, input); err != nil {
		return domainnetworkruntime.EnrollmentSecret{}, err
	}

	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return domainnetworkruntime.EnrollmentSecret{}, fmt.Errorf("generate enrollment token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(random)
	digest := sha256.Sum256([]byte(token))
	now := s.now().UTC()
	item := domainnetworkruntime.EnrollmentChallenge{
		ID: uuid.NewString(), ChallengeID: uuid.NewString(), ChallengeHash: fmt.Sprintf("sha256:%x", digest),
		RuntimeID: input.RuntimeID, RuntimeKind: input.RuntimeKind, DeviceID: input.DeviceID, SubjectID: input.SubjectID,
		Status: domainnetworkruntime.EnrollmentPending, ExpiresAt: now.Add(input.TTL), CreatedBy: principal.UserID, CreatedAt: now,
	}
	if err := s.store.CreateEnrollment(ctx, item); err != nil {
		return domainnetworkruntime.EnrollmentSecret{}, err
	}
	s.recordMutation(ctx, principal, "network_access.enrollments.create", item)
	return domainnetworkruntime.EnrollmentSecret{EnrollmentChallenge: publicEnrollment(item), Token: token}, nil
}

func (s *EnrollmentService) List(ctx context.Context, principal domainidentity.Principal, limit int) ([]domainnetworkruntime.EnrollmentChallenge, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermNetworkAccessEnrollmentsView); err != nil {
		return nil, err
	}
	if err := validateListFilter("", limit); err != nil {
		return nil, err
	}
	items, err := s.store.ListEnrollments(ctx, boundedLimit(limit))
	if err != nil {
		return nil, err
	}
	for index := range items {
		items[index] = publicEnrollment(items[index])
	}
	return items, nil
}

func (s *EnrollmentService) Get(ctx context.Context, principal domainidentity.Principal, id string) (domainnetworkruntime.EnrollmentChallenge, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermNetworkAccessEnrollmentsView); err != nil {
		return domainnetworkruntime.EnrollmentChallenge{}, err
	}
	id = strings.TrimSpace(id)
	if err := requiredText("enrollmentId", id, 128); err != nil {
		return domainnetworkruntime.EnrollmentChallenge{}, err
	}
	item, err := s.store.GetEnrollment(ctx, id)
	return publicEnrollment(item), err
}

func (s *EnrollmentService) Revoke(ctx context.Context, principal domainidentity.Principal, id string) (domainnetworkruntime.EnrollmentChallenge, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermNetworkAccessEnrollmentsRevoke); err != nil {
		return domainnetworkruntime.EnrollmentChallenge{}, err
	}
	id = strings.TrimSpace(id)
	if err := requiredText("enrollmentId", id, 128); err != nil {
		return domainnetworkruntime.EnrollmentChallenge{}, err
	}
	item, err := s.store.GetEnrollment(ctx, id)
	if err != nil {
		return domainnetworkruntime.EnrollmentChallenge{}, err
	}
	now := s.now().UTC()
	if err := s.store.RevokeEnrollment(ctx, item.ID, now); err != nil {
		return domainnetworkruntime.EnrollmentChallenge{}, err
	}
	item, err = s.store.GetEnrollment(ctx, item.ID)
	if err != nil {
		return domainnetworkruntime.EnrollmentChallenge{}, err
	}
	s.recordMutation(ctx, principal, "network_access.enrollments.revoke", item)
	return publicEnrollment(item), nil
}

func validateEnrollmentInput(principal domainidentity.Principal, input EnrollmentInput) error {
	for name, value := range map[string]string{
		"runtimeId": input.RuntimeID, "deviceId": input.DeviceID, "subjectId": input.SubjectID, "createdBy": principal.UserID,
	} {
		if err := requiredText(name, value, 128); err != nil {
			return err
		}
	}
	switch input.RuntimeKind {
	case "endpoint", "gateway", "nas":
	default:
		return invalid("runtimeKind is invalid")
	}
	if input.TTL < time.Minute || input.TTL > defaultEnrollmentTTL {
		return invalid("ttlSeconds must be between 60 and 600")
	}
	return nil
}

func publicEnrollment(item domainnetworkruntime.EnrollmentChallenge) domainnetworkruntime.EnrollmentChallenge {
	item.ChallengeHash = ""
	return item
}

func (s *EnrollmentService) recordMutation(ctx context.Context, principal domainidentity.Principal, action string, item domainnetworkruntime.EnrollmentChallenge) {
	meta := requestctx.FromContext(ctx)
	metadata := map[string]any{
		"enrollmentId": item.ID, "runtimeId": item.RuntimeID, "runtimeKind": item.RuntimeKind,
		"deviceId": item.DeviceID, "subjectId": item.SubjectID, "status": item.Status,
	}
	summary := action + " succeeded"
	_ = s.audit.Record(ctx, domainaudit.Entry{
		ActorID: principal.UserID, ActorName: principal.UserName, Roles: principal.Roles, Teams: principal.Teams,
		ResourceKind: "NetworkRuntimeEnrollment", ResourceName: item.RuntimeID, Action: action, Result: "success", Summary: summary,
		RequestPath: meta.Path, RequestMethod: meta.Method, RequestID: meta.RequestID, SourceIP: meta.SourceIP, Metadata: metadata,
	})
	_ = s.operations.Record(ctx, operationentry.New(ctx, principal, action, map[string]any{
		"module": "network_access", "resourceKind": "NetworkRuntimeEnrollment", "targetId": item.ID,
		"targetLabel": item.RuntimeID, "runtimeKind": item.RuntimeKind, "deviceId": item.DeviceID, "subjectId": item.SubjectID,
	}, "success", summary, nil))
}
