package networkaccess

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainnetworkaccess "github.com/opensoha/soha/internal/domain/networkaccess"
	domainnetworkruntime "github.com/opensoha/soha/internal/domain/networkruntime"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type accessGrantStoreStub struct {
	items   map[string]domainnetworkruntime.AccessGrant
	created domainnetworkruntime.AccessGrant
}

func (s *accessGrantStoreStub) CreateAccessGrant(_ context.Context, grant domainnetworkruntime.AccessGrant) error {
	s.created = grant
	if s.items == nil {
		s.items = map[string]domainnetworkruntime.AccessGrant{}
	}
	s.items[grant.ID] = grant
	return nil
}

func (s *accessGrantStoreStub) GetAccessGrant(_ context.Context, id string, now time.Time) (domainnetworkruntime.AccessGrant, error) {
	grant, ok := s.items[id]
	if !ok {
		return domainnetworkruntime.AccessGrant{}, apperrors.ErrNotFound
	}
	if grant.Status == domainnetworkruntime.AccessGrantIssued && !grant.ExpiresAt.After(now) {
		grant.Status, grant.TokenHash = domainnetworkruntime.AccessGrantExpired, ""
		s.items[id] = grant
	}
	return grant, nil
}

func (s *accessGrantStoreStub) ListAccessGrants(_ context.Context, _ domainnetworkruntime.AccessGrantFilter, now time.Time) ([]domainnetworkruntime.AccessGrant, error) {
	items := make([]domainnetworkruntime.AccessGrant, 0, len(s.items))
	for id := range s.items {
		grant, _ := s.GetAccessGrant(context.Background(), id, now)
		items = append(items, grant)
	}
	return items, nil
}

func (s *accessGrantStoreStub) RevokeAccessGrant(_ context.Context, id string, now time.Time) error {
	grant, ok := s.items[id]
	if !ok {
		return apperrors.ErrNotFound
	}
	if grant.Status != domainnetworkruntime.AccessGrantIssued {
		return apperrors.ErrConflict
	}
	grant.Status, grant.TokenHash, grant.RevokedAt = domainnetworkruntime.AccessGrantRevoked, "", &now
	s.items[id] = grant
	return nil
}

type accessGrantPolicyStoreStub struct {
	subject   domainnetworkaccess.Subject
	device    domainnetworkaccess.Device
	site      domainnetworkaccess.Site
	space     domainnetworkaccess.Space
	resources map[string]domainnetworkaccess.Resource
	snapshot  domainnetworkaccess.PolicySnapshot
}

func (s *accessGrantPolicyStoreStub) GetSubject(context.Context, string) (domainnetworkaccess.Subject, error) {
	return s.subject, nil
}
func (s *accessGrantPolicyStoreStub) GetDevice(context.Context, string) (domainnetworkaccess.Device, error) {
	return s.device, nil
}
func (s *accessGrantPolicyStoreStub) GetSite(context.Context, string) (domainnetworkaccess.Site, error) {
	return s.site, nil
}
func (s *accessGrantPolicyStoreStub) GetSpace(context.Context, string) (domainnetworkaccess.Space, error) {
	return s.space, nil
}
func (s *accessGrantPolicyStoreStub) GetResource(_ context.Context, id string) (domainnetworkaccess.Resource, error) {
	resource, ok := s.resources[id]
	if !ok {
		return domainnetworkaccess.Resource{}, apperrors.ErrNotFound
	}
	return resource, nil
}
func (s *accessGrantPolicyStoreStub) GetPolicySnapshot(context.Context) (domainnetworkaccess.PolicySnapshot, error) {
	return s.snapshot, nil
}

type stepUpStub struct{ err error }

func (s stepUpStub) RequireRecentStepUp(context.Context, string, string) error { return s.err }

func TestAccessGrantServiceCreatesMFAAndPolicyBoundOneTimeSecret(t *testing.T) {
	now := time.Date(2026, 9, 3, 8, 0, 0, 0, time.UTC)
	store := &accessGrantStoreStub{}
	policy := accessGrantPolicyFixture(domainnetworkaccess.ModeExternalDirectZTNA)
	audit := &captureAudit{}
	operations := &enrollmentOperationCapture{}
	permissions := appaccess.NewPermissionResolver(enrollmentRoleReader{"network-user": {
		appaccess.PermNetworkAccessAccessGrantsCreate,
		appaccess.PermNetworkAccessAccessGrantsView,
		appaccess.PermNetworkAccessAccessGrantsRevoke,
	}})
	service, err := NewAccessGrantService(store, policy, stepUpStub{}, permissions, audit, operations)
	if err != nil {
		t.Fatalf("NewAccessGrantService() error = %v", err)
	}
	service.now = func() time.Time { return now }
	principal := domainidentity.Principal{UserID: "user-1", UserName: "User", Roles: []string{"network-user"}}

	secret, err := service.Create(context.Background(), principal, "session-1", AccessGrantInput{
		DeviceID: "device-1", SiteID: "site-1", NetworkSpaceID: "space-1",
		Mode: domainnetworkaccess.ModeExternalDirectZTNA, ResourceIDs: []string{"resource-1"},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if len(secret.Token) < 32 || secret.Grant.TokenHash != "" || secret.Grant.AuthSessionID != "" {
		t.Fatalf("Create() returned unsafe grant: %#v", secret)
	}
	digest := sha256.Sum256([]byte(secret.Token))
	if store.created.TokenHash != fmt.Sprintf("sha256:%x", digest) || store.created.AuthSessionID != "session-1" {
		t.Fatalf("stored grant binding = %#v", store.created)
	}
	if store.created.ExpiresAt != now.Add(defaultAccessGrantTTL) || store.created.PolicyVersion != 7 || store.created.SubjectID != principal.UserID {
		t.Fatalf("stored grant = %#v", store.created)
	}
	encodedAudit, _ := json.Marshal(audit.entry)
	encodedOperation, _ := json.Marshal(operations.entry)
	if containsSensitiveEnrollmentValue(string(encodedAudit), secret.Token, store.created.TokenHash) || containsSensitiveEnrollmentValue(string(encodedOperation), secret.Token, store.created.TokenHash) {
		t.Fatal("grant credential leaked to audit or operation")
	}

	listed, err := service.List(context.Background(), principal, domainnetworkruntime.AccessGrantFilter{})
	if err != nil || len(listed) != 1 || listed[0].TokenHash != "" || listed[0].AuthSessionID != "" {
		t.Fatalf("List() = %#v, err=%v", listed, err)
	}
	revoked, err := service.Revoke(context.Background(), principal, secret.Grant.ID)
	if err != nil || revoked.Status != domainnetworkruntime.AccessGrantRevoked || revoked.TokenHash != "" {
		t.Fatalf("Revoke() = %#v, err=%v", revoked, err)
	}
}

func TestAccessGrantServiceFailsClosedWithoutStepUpOrMatchingPolicy(t *testing.T) {
	permissions := appaccess.NewPermissionResolver(enrollmentRoleReader{"network-user": {appaccess.PermNetworkAccessAccessGrantsCreate}})
	principal := domainidentity.Principal{UserID: "user-1", Roles: []string{"network-user"}}
	input := AccessGrantInput{DeviceID: "device-1", SiteID: "site-1", NetworkSpaceID: "space-1", Mode: domainnetworkaccess.ModeExternalVPNZTNA, ResourceIDs: []string{"resource-1"}}

	store := &accessGrantStoreStub{}
	service, err := NewAccessGrantService(store, accessGrantPolicyFixture(input.Mode), stepUpStub{err: apperrors.ErrMFARequired}, permissions, &captureAudit{}, &enrollmentOperationCapture{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Create(context.Background(), principal, "session-1", input); !errors.Is(err, apperrors.ErrMFARequired) {
		t.Fatalf("Create() error = %v, want MFA required", err)
	}
	if store.created.ID != "" {
		t.Fatal("grant persisted without step-up")
	}

	policy := accessGrantPolicyFixture(input.Mode)
	policy.snapshot.Policies[0].Subjects.Users = []string{"another-user"}
	service, _ = NewAccessGrantService(store, policy, stepUpStub{}, permissions, &captureAudit{}, &enrollmentOperationCapture{})
	if _, err := service.Create(context.Background(), principal, "session-1", input); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("Create() error = %v, want access denied", err)
	}
	if store.created.ID != "" {
		t.Fatal("grant persisted after policy denial")
	}
}

func accessGrantPolicyFixture(mode string) *accessGrantPolicyStoreStub {
	return &accessGrantPolicyStoreStub{
		subject: domainnetworkaccess.Subject{UserID: "user-1", Status: domainnetworkaccess.StatusActive},
		device:  domainnetworkaccess.Device{ID: "device-1", OwnerUserID: "user-1", SiteID: "site-1", Status: domainnetworkaccess.DeviceStatusActive, PostureStatus: domainnetworkaccess.PostureCompliant},
		site:    domainnetworkaccess.Site{ID: "site-1", Status: domainnetworkaccess.StatusActive},
		space:   domainnetworkaccess.Space{ID: "space-1", SiteID: "site-1", CIDRs: []string{"10.20.0.0/16"}, Status: domainnetworkaccess.StatusActive},
		resources: map[string]domainnetworkaccess.Resource{
			"resource-1": {ID: "resource-1", SpaceID: "space-1", Kind: "ip", Target: "10.20.0.10", Protocol: "tcp", Ports: []int{443}, Protected: true, PathMode: domainnetworkaccess.PathAutomatic},
		},
		snapshot: domainnetworkaccess.PolicySnapshot{PolicyVersion: 7, Policies: []domainnetworkaccess.Policy{{
			ID: "allow-resource", Enabled: true, Priority: 100, Effect: domainnetworkaccess.PolicyEffectAllow,
			Subjects: domainnetworkaccess.PolicySubjects{Users: []string{"user-1"}}, SiteIDs: []string{"site-1"}, ResourceIDs: []string{"resource-1"},
			Modes: []string{mode}, DeviceStatuses: []string{domainnetworkaccess.DeviceStatusActive}, PostureStatuses: []string{domainnetworkaccess.PostureCompliant}, AccessProfile: domainnetworkaccess.ProfileFull,
		}}},
	}
}
