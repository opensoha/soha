package copilot

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	contractenv "github.com/opensoha/soha-contracts/agent/environment"
	guest "github.com/opensoha/soha-contracts/agent/guest"
	appaudit "github.com/opensoha/soha/internal/application/audit"
	appcopilot "github.com/opensoha/soha/internal/application/copilot"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	config "github.com/opensoha/soha/internal/infrastructure/config"
	dbstore "github.com/opensoha/soha/internal/infrastructure/db"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	auditrepository "github.com/opensoha/soha/internal/repository/auditlog"
	"go.uber.org/zap"
)

func TestPersonalGuestWithPostgres(t *testing.T) {
	repo, ctx := retainedPersonalGuestRepository(t)
	for _, test := range []struct {
		name string
		run  func(*testing.T, *Repository, context.Context, domain.PersonalGuestRecord)
	}{
		{"single_use_enrollment", testGuestEnrollmentCAS},
		{"monotonic_heartbeat", testGuestHeartbeatCAS},
		{"current_access_and_owner", testGuestCurrentAccess},
		{"frozen_binding_and_generation", testGuestFrozenBinding},
		{"superseded_identity", testGuestSupersededIdentity},
		{"certificate_and_disk_fence", testGuestCertificateAndDiskFence},
		{"audit_atomicity", testGuestAuditAtomicity},
		{"renewal_sequence_and_authority", testGuestRenewal},
	} {
		t.Run(test.name, func(t *testing.T) {
			item := seedPersonalGuest(t, repo, ctx)
			test.run(t, repo, ctx, item)
			view, err := repo.GetPersonalEnvironment(ctx, item.Binding.OwnerID, item.Binding.AssistantID)
			if err != nil || view.State != "pending" || view.DataInitialized {
				t.Fatalf("liveness changed environment/data readiness: %+v %v", view, err)
			}
		})
	}
}

type guestFailingAudit struct {
	service *appaudit.Service
	fail    bool
}

func (a *guestFailingAudit) Record(ctx context.Context, entry domainaudit.Entry) error {
	if err := a.service.Record(ctx, entry); err != nil {
		return err
	}
	if a.fail {
		return errors.New("test audit failure after write")
	}
	return nil
}

func (a *guestFailingAudit) List(ctx context.Context, filter domainaudit.Filter) ([]domainaudit.Entry, error) {
	return a.service.List(ctx, filter)
}

func testGuestAuditAtomicity(t *testing.T, repo *Repository, ctx context.Context, item domain.PersonalGuestRecord) {
	audit := &guestFailingAudit{service: appaudit.New(auditrepository.New(repo.db), nil), fail: true}
	service, err := appcopilot.NewPersonalGuestService(repo, func(context.Context, guest.Binding) error { return nil }, audit)
	if err != nil {
		t.Fatal(err)
	}
	next := item.Binding
	next.ID, next.Version = uuid.NewString(), 2
	if _, _, err := service.Prepare(ctx, next, item.CertificateFingerprint, item.AccessVersion, time.Minute); err == nil {
		t.Fatal("audit failure returned a bootstrap grant")
	}
	if _, err := repo.GetPersonalGuest(ctx, next.ID); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("audit failure retained registration: %v", err)
	}
	identity := networkidentity.Identity{Scope: networkidentity.ScopePersonalGuest, Kind: "guest", ID: item.Binding.ID, CertificateFingerprint: item.CertificateFingerprint}
	request := guest.Enrollment{SchemaVersion: guest.EnrollmentVersion, EnrollmentID: item.EnrollmentID}
	if _, err := service.Enroll(ctx, identity, "Bearer "+strings.Repeat("A", 43), request); err == nil {
		t.Fatal("audit failure returned enrollment success")
	}
	current, err := repo.GetPersonalGuest(ctx, item.Binding.ID)
	if err != nil || current.Status != "pending" {
		t.Fatalf("audit failure consumed challenge: %v", err)
	}
	var count int
	if err := repo.db.WithContext(ctx).Raw(`SELECT COUNT(*) FROM audit_logs WHERE resource_name IN (?,?)`, item.Binding.ID, next.ID).Row().Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed registration audit committed: %d %v", count, err)
	}
	audit.fail = false
	if _, err := service.Enroll(ctx, identity, "Bearer "+strings.Repeat("A", 43), request); err != nil {
		t.Fatalf("challenge lost after audit rollback: %v", err)
	}
	if err := repo.db.WithContext(ctx).Raw(`SELECT COUNT(*) FROM audit_logs WHERE resource_name=? AND action='personal_guest.enroll'`, item.Binding.ID).Row().Scan(&count); err != nil || count != 1 {
		t.Fatalf("success has no durable audit: %d %v", count, err)
	}
}

// Retained test-only data honors no-delete acceptance runs; never uses development PG.
func retainedPersonalGuestRepository(t *testing.T) (*Repository, context.Context) {
	t.Helper()
	portText := os.Getenv("SOHA_PERSONAL_GUEST_TEST_POSTGRES_PORT")
	if portText == "" {
		t.Skip("set SOHA_PERSONAL_GUEST_TEST_POSTGRES_PORT to a dedicated retained test instance")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port == 5432 || port < 1024 || os.Getenv("SOHA_PERSONAL_GUEST_TEST_LAB_ID") == "" || len(os.Getenv("SOHA_PERSONAL_GUEST_TEST_POSTGRES_PASSWORD")) < 32 {
		t.Fatal("dedicated personal guest PostgreSQL identity is required")
	}
	cfg := config.DatabaseConfig{Driver: "postgres", Host: "127.0.0.1", Port: port, Name: "soha", User: "pgsql",
		Password: os.Getenv("SOHA_PERSONAL_GUEST_TEST_POSTGRES_PASSWORD"), SSLMode: "disable", MaxOpenConns: 12, MaxIdleConns: 4}
	admin, err := dbstore.New(cfg, zap.NewNop())
	if err != nil {
		t.Fatal("open dedicated guest PostgreSQL")
	}
	t.Cleanup(func() { _ = admin.Close() })
	cfg.Name = "personal_guest_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := admin.DB().Exec(`CREATE DATABASE "` + cfg.Name + `"`).Error; err != nil {
		t.Fatal(err)
	}
	store, err := dbstore.New(cfg, zap.NewNop())
	if err != nil {
		t.Fatal("open dedicated guest test database")
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	if err := store.MigrateFromFile(ctx, filepath.Join("..", "..", "..", "migrations", "postgres")); err != nil {
		t.Fatal(err)
	}
	t.Logf("retained test database: %s", cfg.Name)
	return New(store.DB()), ctx
}

func seedPersonalGuest(t *testing.T, repo *Repository, ctx context.Context) domain.PersonalGuestRecord {
	return seedPersonalGuestWithDiskQuota(t, repo, ctx, 8<<30)
}

func seedPersonalGuestWithDiskQuota(t *testing.T, repo *Repository, ctx context.Context, diskQuota int64) domain.PersonalGuestRecord {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Microsecond)
	owner, assistant, session := uuid.NewString(), uuid.NewString(), uuid.NewString()
	if _, err := repo.CreateSession(ctx, domain.Session{ID: session, CreatedBy: owner, Title: "guest test", Metadata: map[string]any{"conversationKind": "personal_main"}, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetOrCreatePersonalAssistant(ctx, domain.PersonalAssistant{ID: assistant, CreatedBy: owner, MainSessionID: session, Name: "guest test", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.RequestPersonalAgentAccess(ctx, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.DecidePersonalAgentAccess(ctx, owner, "test-admin", "approved", 1); err != nil {
		t.Fatal(err)
	}
	binding := guest.Binding{SchemaVersion: guest.BindingVersion, ID: uuid.NewString(), Version: 1, AssistantID: assistant, OwnerID: owner,
		Provider: "pve", ComputeRef: uuid.NewString(), ComputeGeneration: 1, ProviderIdentityHash: "sha256:" + strings.Repeat("c", 64), DataRef: uuid.NewString(), DataDiskSerial: "testdata01", DataDiskBytes: 8 << 30,
		PolicyRevision: "sha256:" + strings.Repeat("a", 64), IssuedAt: now, ExpiresAt: now.Add(time.Hour)}
	lease := contractenv.Lease{SchemaVersion: contractenv.LeaseVersion, ID: uuid.NewString(), RunID: uuid.NewString(), OwnerID: assistant,
		ScopeHash: binding.PolicyRevision, Mode: "restricted-write", Quota: contractenv.Quota{DiskBytes: diskQuota, MemoryBytes: 4 << 30, CPUUnits: 2000, MaxProcesses: 64}, IssuedAt: now, ExpiresAt: now.Add(time.Hour)}
	if err := (contractenv.Request{SchemaVersion: contractenv.RequestVersion, Action: "acquire", Lease: lease}).Validate(); err != nil {
		t.Fatal(err)
	}
	proof := &domain.PersonalVMIdentity{ProviderIdentityHash: binding.ProviderIdentityHash, DataIdentityHash: fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(binding.DataRef))), DataDiskSerial: binding.DataDiskSerial, DataDiskBytes: binding.DataDiskBytes}
	if _, err := repo.BindPersonalEnvironment(ctx, domain.PersonalEnvironmentRecord{AssistantID: assistant, CreatedBy: owner, TemplateID: "test-personal-vm", LeaseSeconds: 3600, Lease: lease, UpdatedAt: now,
		Backend: "vm", Provider: binding.Provider, PolicyRevision: binding.PolicyRevision, ComputeRef: binding.ComputeRef, ComputeGeneration: 1, DataRef: binding.DataRef, VMIdentity: proof}); err != nil {
		t.Fatal(err)
	}
	hash, err := guest.EnrollmentTokenHash("Bearer " + strings.Repeat("A", 43))
	if err != nil {
		t.Fatal(err)
	}
	item := domain.PersonalGuestRecord{Binding: binding, CertificateFingerprint: "sha256:" + strings.Repeat("b", 64), AccessVersion: 2,
		EnrollmentID: uuid.NewString(), ChallengeHash: hash, EnrollmentIssuedAt: now, EnrollmentExpiresAt: now.Add(time.Minute), Status: "pending"}
	if err := repo.CreatePersonalGuest(ctx, item); err != nil {
		t.Fatal(err)
	}
	return item
}

func testGuestEnrollmentCAS(t *testing.T, repo *Repository, ctx context.Context, item domain.PersonalGuestRecord) {
	var count atomic.Int32
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			err := repo.ConsumePersonalGuestEnrollment(ctx, item, item.ChallengeHash, time.Now().UTC())
			if err == nil {
				count.Add(1)
			} else if !errors.Is(err, apperrors.ErrUnauthorized) {
				t.Errorf("consume enrollment: %v", err)
			}
		})
	}
	wg.Wait()
	if count.Load() != 1 {
		t.Fatalf("challenge consumed %d times", count.Load())
	}
	current, err := repo.GetPersonalGuest(ctx, item.Binding.ID)
	if err != nil || current.Status != "registered" || current.LastSequence != 0 {
		t.Fatalf("durable enrollment recovery: %+v %v", current, err)
	}
}

func registeredGuestHeartbeat(t *testing.T, repo *Repository, ctx context.Context, item domain.PersonalGuestRecord) guest.Heartbeat {
	t.Helper()
	if err := repo.ConsumePersonalGuestEnrollment(ctx, item, item.ChallengeHash, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	return guest.Heartbeat{SchemaVersion: guest.HeartbeatVersion, BindingID: item.Binding.ID, BindingVersion: item.Binding.Version,
		ComputeGeneration: item.Binding.ComputeGeneration, Sequence: 1, ObservedAt: time.Now().UTC()}
}

func testGuestHeartbeatCAS(t *testing.T, repo *Repository, ctx context.Context, item domain.PersonalGuestRecord) {
	heartbeat := registeredGuestHeartbeat(t, repo, ctx, item)
	var count atomic.Int32
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			err := repo.RecordPersonalGuestHeartbeat(ctx, item, heartbeat, time.Now().UTC())
			if err == nil {
				count.Add(1)
			} else if !errors.Is(err, apperrors.ErrConflict) {
				t.Errorf("record heartbeat: %v", err)
			}
		})
	}
	wg.Wait()
	current, err := repo.GetPersonalGuest(ctx, item.Binding.ID)
	if count.Load() != 1 || err != nil || current.LastSequence != 1 || current.LastHeartbeatAt == nil {
		t.Fatalf("non-monotonic durable heartbeat: %d %+v %v", count.Load(), current, err)
	}
}

func testGuestCurrentAccess(t *testing.T, repo *Repository, ctx context.Context, item domain.PersonalGuestRecord) {
	forged := item
	forged.Binding.OwnerID = uuid.NewString()
	if err := repo.CheckPersonalGuestBinding(ctx, forged); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("foreign owner accepted: %v", err)
	}
	if _, err := repo.DecidePersonalAgentAccess(ctx, item.Binding.OwnerID, "test-admin", "revoked", 2); err != nil {
		t.Fatal(err)
	}
	if err := repo.ConsumePersonalGuestEnrollment(ctx, item, item.ChallengeHash, time.Now().UTC()); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("revoked activation consumed enrollment: %v", err)
	}
	if _, err := repo.RequestPersonalAgentAccess(ctx, item.Binding.OwnerID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.DecidePersonalAgentAccess(ctx, item.Binding.OwnerID, "test-admin", "approved", 4); err != nil {
		t.Fatal(err)
	}
	if err := repo.CheckPersonalGuestBinding(ctx, item); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("new activation revived old guest authority: %v", err)
	}
}

func testGuestFrozenBinding(t *testing.T, repo *Repository, ctx context.Context, item domain.PersonalGuestRecord) {
	heartbeat := registeredGuestHeartbeat(t, repo, ctx, item)
	if err := repo.db.WithContext(ctx).Exec(`UPDATE ai_personal_environments SET compute_generation=compute_generation+1 WHERE assistant_id=?`, item.Binding.AssistantID).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.RecordPersonalGuestHeartbeat(ctx, item, heartbeat, time.Now().UTC()); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("old compute callback accepted: %v", err)
	}
}

func testGuestSupersededIdentity(t *testing.T, repo *Repository, ctx context.Context, item domain.PersonalGuestRecord) {
	next := item
	next.Binding.ID, next.Binding.Version, next.EnrollmentID = uuid.NewString(), 2, uuid.NewString()
	if err := repo.CreatePersonalGuest(ctx, next); err != nil {
		t.Fatal(err)
	}
	if err := repo.ConsumePersonalGuestEnrollment(ctx, item, item.ChallengeHash, time.Now().UTC()); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("superseded certificate consumed challenge: %v", err)
	}
	if err := repo.CheckPersonalGuestBinding(ctx, next); err != nil {
		t.Fatalf("new identity revision blocked: %v", err)
	}
}

func testGuestCertificateAndDiskFence(t *testing.T, repo *Repository, ctx context.Context, item domain.PersonalGuestRecord) {
	for _, change := range []func(*domain.PersonalGuestRecord){
		func(r *domain.PersonalGuestRecord) { r.Binding.DataDiskSerial = "otherdata01" },
		func(r *domain.PersonalGuestRecord) { r.Binding.DataDiskBytes += 1 << 30 },
		func(r *domain.PersonalGuestRecord) {
			r.Binding.ProviderIdentityHash = "sha256:" + strings.Repeat("d", 64)
		},
	} {
		forged := item
		change(&forged)
		if err := repo.ConsumePersonalGuestEnrollment(ctx, forged, item.ChallengeHash, time.Now().UTC()); !errors.Is(err, apperrors.ErrUnauthorized) {
			t.Fatalf("altered frozen disk consumed challenge: %v", err)
		}
	}
	forged := item
	forged.CertificateFingerprint = "sha256:" + strings.Repeat("c", 64)
	if err := repo.ConsumePersonalGuestEnrollment(ctx, forged, item.ChallengeHash, time.Now().UTC()); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("changed certificate consumed challenge: %v", err)
	}
}
