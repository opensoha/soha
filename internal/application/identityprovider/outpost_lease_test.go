package identityprovider

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	domainprovider "github.com/opensoha/soha/internal/domain/identityprovider"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type outpostTestLease struct {
	token, digest   string
	version         int64
	issued, expires time.Time
}
type outpostLeaseMemoryRepo struct {
	*memoryRepo
	leases      map[string]outpostTestLease
	beforeGrant func()
}

func newOutpostLeaseMemoryRepo(t *testing.T) *outpostLeaseMemoryRepo {
	return &outpostLeaseMemoryRepo{memoryRepo: newMemoryRepo(t), leases: map[string]outpostTestLease{}}
}
func (r *outpostLeaseMemoryRepo) RecordOutpostRuntimeLease(ctx context.Context, id, token, digest string, version int64, issued, expires time.Time) error {
	if r.beforeGrant != nil {
		r.beforeGrant()
	}
	item, err := r.GetOutpost(ctx, id)
	if err != nil {
		return err
	}
	if token != item.TokenHash {
		return apperrors.ErrAccessDenied
	}
	if r.outpostVersions[id] != version || r.outpostDigests[id] != digest {
		return apperrors.ErrConflict
	}
	r.leases[id] = outpostTestLease{token, digest, version, issued, expires}
	return nil
}
func (r *outpostLeaseMemoryRepo) ValidateOutpostRuntimeLease(ctx context.Context, id, token string, version int64, expiry *time.Time) error {
	item, err := r.GetOutpost(ctx, id)
	if err != nil {
		return err
	}
	lease, ok := r.leases[id]
	now := time.Now()
	if !ok || token != item.TokenHash || lease.token != token || lease.version != version || !lease.expires.After(now) || (expiry != nil && (!expiry.After(now) || expiry.After(lease.expires))) {
		return apperrors.ErrConflict
	}
	return nil
}
func outpostLeaseFixture(t *testing.T) (*Service, *outpostLeaseMemoryRepo, domainprovider.Outpost, sohaapi.IdentityOutpostRuntimeConfig) {
	t.Helper()
	repo := newOutpostLeaseMemoryRepo(t)
	service := New(repo, &memoryUsers{}, identityProviderTestPermissions(), nil, "test-encryption-key-32-bytes-long")
	seed := sha256.Sum256([]byte("outpost-lease-test"))
	service.SetOutpostSigningKey("test-key", ed25519.NewKeyFromSeed(seed[:]))
	created, err := service.CreateOutpost(context.Background(), (&memoryUsers{}).principal(), domainprovider.OutpostInput{Name: "edge", Mode: "external"})
	ssoNoError(t, err)
	config, err := service.ClaimIdentityOutpostRuntime(context.Background(), created.Token, sohaapi.IdentityOutpostClaimRequest{AgentID: created.ID, SupportedProtocolVersion: "v1"})
	ssoNoError(t, err)
	return service, repo, created, *config
}
func TestOutpostHeartbeatEnforcesGrantedLease(t *testing.T) {
	for _, name := range []string{"valid", "future version", "ungranted version", "extended expiry", "expired", "missing expiry", "zero healthy", "failed first apply", "missing grant", "same version renewal"} {
		t.Run(name, func(t *testing.T) {
			service, repo, created, config := outpostLeaseFixture(t)
			request := sohaapi.IdentityOutpostHeartbeatRequest{AgentID: created.ID, ConfigurationVersion: config.ConfigurationVersion, ConfigurationExpiresAt: &config.ExpiresAt, Status: sohaapi.IdentityOutpostHeartbeatRequestStatusHealthy}
			wantOK := false
			switch name {
			case "valid":
				wantOK = true
			case "future version":
				request.ConfigurationVersion++
			case "ungranted version":
				repo.leases[created.ID] = outpostTestLease{}
			case "extended expiry":
				expiry := config.ExpiresAt.Add(time.Second)
				request.ConfigurationExpiresAt = &expiry
			case "expired":
				expiry := time.Now().Add(-time.Second)
				request.ConfigurationExpiresAt = &expiry
			case "missing expiry":
				request.ConfigurationExpiresAt = nil
			case "zero healthy":
				request.ConfigurationVersion = 0
				request.ConfigurationExpiresAt = nil
			case "failed first apply":
				request.ConfigurationVersion = 0
				request.ConfigurationExpiresAt = nil
				request.Status = sohaapi.IdentityOutpostHeartbeatRequestStatusUnavailable
				request.ErrorCode = "configuration_rejected"
				wantOK = true
			case "missing grant":
				delete(repo.leases, created.ID)
			case "same version renewal":
				_, err := service.ClaimIdentityOutpostRuntime(context.Background(), created.Token, sohaapi.IdentityOutpostClaimRequest{AgentID: created.ID, SupportedProtocolVersion: "v1"})
				ssoNoError(t, err)
				wantOK = true
			}
			_, err := service.HeartbeatIdentityOutpostRuntime(context.Background(), created.ID, created.Token, request)
			if wantOK {
				ssoNoError(t, err)
			} else {
				if err == nil {
					t.Fatal("accepted untrusted heartbeat")
				}
				item, getErr := repo.GetOutpost(context.Background(), created.ID)
				ssoNoError(t, getErr)
				if item.LastHeartbeatAt != nil {
					t.Fatal("rejected report changed observations")
				}
			}
		})
	}
}
func TestOutpostClaimRejectsRotationBeforeGrant(t *testing.T) {
	service, repo, created, _ := outpostLeaseFixture(t)
	repo.beforeGrant = func() {
		item := repo.outposts[created.ID]
		item.TokenHash = "rotated"
		repo.outposts[created.ID] = item
	}
	config, err := service.ClaimIdentityOutpostRuntime(context.Background(), created.Token, sohaapi.IdentityOutpostClaimRequest{AgentID: created.ID, SupportedProtocolVersion: "v1"})
	if config != nil || !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("claim after credential rotation = %v, %v", config, err)
	}
}
func TestOutpostClaimFailsClosedWithoutLeaseRepository(t *testing.T) {
	service, repo, created, _ := outpostLeaseFixture(t)
	service.repo = repo.memoryRepo
	config, err := service.ClaimIdentityOutpostRuntime(context.Background(), created.Token, sohaapi.IdentityOutpostClaimRequest{AgentID: created.ID, SupportedProtocolVersion: "v1"})
	if config != nil || !errors.Is(err, apperrors.ErrUnsupportedOperation) {
		t.Fatalf("claim without durable grant capability = %v, %v", config, err)
	}
}

func TestOutpostClaimRejectsConfigurationChangeBeforeGrant(t *testing.T) {
	service, repo, created, _ := outpostLeaseFixture(t)
	repo.beforeGrant = func() { repo.outpostVersions[created.ID]++ }
	config, err := service.ClaimIdentityOutpostRuntime(context.Background(), created.Token, sohaapi.IdentityOutpostClaimRequest{AgentID: created.ID, SupportedProtocolVersion: "v1"})
	if config != nil || !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("claim after configuration race = %v, %v", config, err)
	}
}
func TestOutpostAccessCheckRequiresCurrentGrant(t *testing.T) {
	for _, name := range []string{"missing", "expired", "rotated"} {
		t.Run(name, func(t *testing.T) {
			service, repo, created, config := outpostLeaseFixture(t)
			lease := repo.leases[created.ID]
			switch name {
			case "missing":
				delete(repo.leases, created.ID)
			case "expired":
				lease.expires = time.Now().Add(-time.Second)
				repo.leases[created.ID] = lease
			case "rotated":
				lease.token = "old-token"
				repo.leases[created.ID] = lease
			}
			_, err := service.CheckIdentityOutpostAccess(context.Background(), created.ID, created.Token, sohaapi.IdentityOutpostAccessCheckRequest{ConfigurationVersion: config.ConfigurationVersion})
			if !errors.Is(err, apperrors.ErrConflict) {
				t.Fatalf("check with %s grant = %v", name, err)
			}
		})
	}
}

func TestLegacyOutpostCheckCannotDowngradeSignedRegistration(t *testing.T) {
	service, repo, created, _ := outpostLeaseFixture(t)
	ctx := context.Background()
	_, err := service.ClaimOutpost(ctx, domainprovider.OutpostClaimInput{OutpostID: created.ID, Token: created.Token})
	ssoNoError(t, err)
	lease := repo.leases[created.ID]
	lease.expires = time.Now().Add(-time.Second)
	repo.leases[created.ID] = lease
	_, err = service.CheckOutpost(ctx, created.ID, domainprovider.OutpostCheckInput{Token: created.Token})
	if !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("expired signed registration used unsigned route: %v", err)
	}
}

func TestLegacyOutpostCannotDowngradeHistoricalSignedRegistrationOnRotation(t *testing.T) {
	service, repo, created, _ := outpostLeaseFixture(t)
	// A pre-0143 signed claim has its persisted protocol but no durable lease.
	delete(repo.leases, created.ID)
	ctx := context.Background()
	rotated, err := service.RotateOutpostToken(ctx, (&memoryUsers{}).principal(), created.ID)
	ssoNoError(t, err)
	if rotated.Token == "" || rotated.Token == created.Token {
		t.Fatal("rotation did not issue a new credential")
	}
	_, err = service.CheckOutpost(ctx, created.ID, domainprovider.OutpostCheckInput{Token: rotated.Token})
	if !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("rotation allowed an unsigned check before signed reclaim: %v", err)
	}
}
