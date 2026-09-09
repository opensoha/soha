package networkaccess

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainnetworkruntime "github.com/opensoha/soha/internal/domain/networkruntime"
	domainoperation "github.com/opensoha/soha/internal/domain/operation"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type enrollmentStore struct {
	items   map[string]domainnetworkruntime.EnrollmentChallenge
	created domainnetworkruntime.EnrollmentChallenge
}

func (s *enrollmentStore) CreateEnrollment(_ context.Context, item domainnetworkruntime.EnrollmentChallenge) error {
	s.created = item
	if s.items == nil {
		s.items = map[string]domainnetworkruntime.EnrollmentChallenge{}
	}
	s.items[item.ID] = item
	return nil
}

func (s *enrollmentStore) GetEnrollment(_ context.Context, id string) (domainnetworkruntime.EnrollmentChallenge, error) {
	item, ok := s.items[id]
	if !ok {
		return domainnetworkruntime.EnrollmentChallenge{}, apperrors.ErrNotFound
	}
	return item, nil
}

func (s *enrollmentStore) ListEnrollments(context.Context, int) ([]domainnetworkruntime.EnrollmentChallenge, error) {
	items := make([]domainnetworkruntime.EnrollmentChallenge, 0, len(s.items))
	for _, item := range s.items {
		items = append(items, item)
	}
	return items, nil
}

func (s *enrollmentStore) RevokeEnrollment(_ context.Context, id string, now time.Time) error {
	item, ok := s.items[id]
	if !ok {
		return apperrors.ErrNotFound
	}
	if item.Status != domainnetworkruntime.EnrollmentPending {
		return apperrors.ErrConflict
	}
	item.Status, item.RevokedAt = domainnetworkruntime.EnrollmentRevoked, &now
	s.items[id] = item
	return nil
}

type enrollmentRoleReader map[string][]string

func (r enrollmentRoleReader) ListRolePermissions(context.Context) (map[string][]string, error) {
	return r, nil
}

type enrollmentOperationCapture struct{ entry domainoperation.Entry }

func (c *enrollmentOperationCapture) Record(_ context.Context, entry domainoperation.Entry) error {
	c.entry = entry
	return nil
}

func TestEnrollmentServiceCreatesOneTimeSecretWithoutLeakingDigest(t *testing.T) {
	store := &enrollmentStore{}
	audit := &captureAudit{}
	operations := &enrollmentOperationCapture{}
	permissions := appaccess.NewPermissionResolver(enrollmentRoleReader{"network-admin": {
		appaccess.PermNetworkAccessEnrollmentsCreate,
		appaccess.PermNetworkAccessEnrollmentsView,
		appaccess.PermNetworkAccessEnrollmentsRevoke,
	}})
	service, err := NewEnrollmentService(store, permissions, audit, operations)
	if err != nil {
		t.Fatalf("NewEnrollmentService() error = %v", err)
	}
	now := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	principal := domainidentity.Principal{UserID: "operator-1", UserName: "Operator", Roles: []string{"network-admin"}}

	secret, err := service.Create(context.Background(), principal, EnrollmentInput{
		RuntimeID: " endpoint-1 ", RuntimeKind: " endpoint ", DeviceID: " device-1 ", SubjectID: " user-1 ",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if len(secret.Token) < 32 || secret.ChallengeHash != "" {
		t.Fatalf("Create() returned unsafe secret metadata: token length=%d hash=%q", len(secret.Token), secret.ChallengeHash)
	}
	digest := sha256.Sum256([]byte(secret.Token))
	if store.created.ChallengeHash != fmt.Sprintf("sha256:%x", digest) {
		t.Fatalf("stored digest = %q, want token SHA-256", store.created.ChallengeHash)
	}
	if store.created.ExpiresAt != now.Add(10*time.Minute) || store.created.CreatedBy != principal.UserID {
		t.Fatalf("stored enrollment = %#v", store.created)
	}
	encodedAudit, _ := json.Marshal(audit.entry)
	if containsSensitiveEnrollmentValue(string(encodedAudit), secret.Token, store.created.ChallengeHash) {
		t.Fatalf("audit leaked enrollment credential: %s", encodedAudit)
	}
	encodedOperation, _ := json.Marshal(operations.entry)
	if containsSensitiveEnrollmentValue(string(encodedOperation), secret.Token, store.created.ChallengeHash) {
		t.Fatalf("operation leaked enrollment credential: %s", encodedOperation)
	}

	listed, err := service.List(context.Background(), principal, 100)
	if err != nil || len(listed) != 1 || listed[0].ChallengeHash != "" {
		t.Fatalf("List() = %#v, err=%v", listed, err)
	}
	got, err := service.Get(context.Background(), principal, secret.ID)
	if err != nil || got.ChallengeHash != "" {
		t.Fatalf("Get() = %#v, err=%v", got, err)
	}
	revoked, err := service.Revoke(context.Background(), principal, secret.ID)
	if err != nil || revoked.Status != domainnetworkruntime.EnrollmentRevoked || revoked.ChallengeHash != "" {
		t.Fatalf("Revoke() = %#v, err=%v", revoked, err)
	}
}

func TestEnrollmentServiceRejectsInvalidTTLAndRuntimeKind(t *testing.T) {
	store := &enrollmentStore{}
	permissions := appaccess.NewPermissionResolver(enrollmentRoleReader{"network-admin": {appaccess.PermNetworkAccessEnrollmentsCreate}})
	service, err := NewEnrollmentService(store, permissions, &captureAudit{}, &enrollmentOperationCapture{})
	if err != nil {
		t.Fatalf("NewEnrollmentService() error = %v", err)
	}
	principal := domainidentity.Principal{UserID: "operator-1", Roles: []string{"network-admin"}}
	for _, input := range []EnrollmentInput{
		{RuntimeID: "runtime-1", RuntimeKind: "worker", DeviceID: "device-1", SubjectID: "user-1"},
		{RuntimeID: "runtime-1", RuntimeKind: "endpoint", DeviceID: "device-1", SubjectID: "user-1", TTL: 11 * time.Minute},
	} {
		if _, err := service.Create(context.Background(), principal, input); !errors.Is(err, apperrors.ErrInvalidArgument) {
			t.Fatalf("Create(%#v) error = %v, want invalid argument", input, err)
		}
	}
	if store.created.ID != "" {
		t.Fatalf("invalid enrollment was persisted: %#v", store.created)
	}
}

func containsSensitiveEnrollmentValue(value string, sensitive ...string) bool {
	for _, item := range sensitive {
		if item != "" && len(item) >= 32 && strings.Contains(value, item) {
			return true
		}
	}
	return false
}
