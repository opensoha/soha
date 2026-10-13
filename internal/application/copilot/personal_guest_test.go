package copilot

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	guest "github.com/opensoha/soha-contracts/agent/guest"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type personalGuestMemory struct {
	mu       sync.Mutex
	tx       sync.Mutex
	item     domain.PersonalGuestRecord
	current  bool
	getErr   error
	audits   []domainaudit.Entry
	auditErr error
}

func (m *personalGuestMemory) WithinPersonalGuestTransaction(ctx context.Context, apply func(context.Context) error) error {
	m.tx.Lock()
	defer m.tx.Unlock()
	m.mu.Lock()
	before := m.item
	audits := append([]domainaudit.Entry{}, m.audits...)
	m.mu.Unlock()
	if err := apply(ctx); err != nil {
		m.mu.Lock()
		m.item, m.audits = before, audits
		m.mu.Unlock()
		return err
	}
	return nil
}

func (m *personalGuestMemory) CreatePersonalGuest(_ context.Context, item domain.PersonalGuestRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.current {
		return apperrors.ErrAccessDenied
	}
	if m.item.Binding.ID != "" {
		return apperrors.ErrConflict
	}
	m.item = item
	return nil
}

func (m *personalGuestMemory) GetPersonalGuest(context.Context, string) (domain.PersonalGuestRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.item, m.getErr
}

func (m *personalGuestMemory) CheckPersonalGuestBinding(context.Context, domain.PersonalGuestRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.current {
		return apperrors.ErrAccessDenied
	}
	return nil
}

func (m *personalGuestMemory) ConsumePersonalGuestEnrollment(_ context.Context, item domain.PersonalGuestRecord, hash string, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.current {
		return apperrors.ErrAccessDenied
	}
	if m.item.Status != "pending" || hash != m.item.ChallengeHash || item.EnrollmentID != m.item.EnrollmentID || !now.Before(m.item.EnrollmentExpiresAt) {
		return apperrors.ErrUnauthorized
	}
	m.item.Status = "registered"
	return nil
}

func (m *personalGuestMemory) RecordPersonalGuestHeartbeat(_ context.Context, _ domain.PersonalGuestRecord, heartbeat guest.Heartbeat, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.current {
		return apperrors.ErrAccessDenied
	}
	if heartbeat.Sequence <= m.item.LastSequence {
		return apperrors.ErrConflict
	}
	m.item.LastSequence, m.item.LastHeartbeatAt = heartbeat.Sequence, &now
	return nil
}

func (m *personalGuestMemory) Record(_ context.Context, entry domainaudit.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.auditErr != nil {
		return m.auditErr
	}
	m.audits = append(m.audits, entry)
	return nil
}

func TestPersonalGuestAuditFailureDoesNotConsumeChallenge(t *testing.T) {
	s, store, identity, request, authorization := personalGuestFixture(t)
	store.auditErr = errors.New("audit unavailable")
	if _, err := s.Enroll(context.Background(), identity, authorization, request); err == nil || store.item.Status != "pending" {
		t.Fatalf("audit failure committed enrollment: %v", err)
	}
	store.auditErr = nil
	if _, err := s.Enroll(context.Background(), identity, authorization, request); err != nil {
		t.Fatalf("audit recovery lost one-time enrollment: %v", err)
	}
}

func personalGuestFixture(t *testing.T) (*PersonalGuestService, *personalGuestMemory, networkidentity.Identity, guest.Enrollment, string) {
	t.Helper()
	now := time.Date(2026, 10, 11, 12, 0, 0, 123456789, time.UTC)
	binding := guest.Binding{SchemaVersion: guest.BindingVersion, ID: uuid.NewString(), Version: 1, AssistantID: uuid.NewString(), OwnerID: uuid.NewString(),
		Provider: "pve", ComputeRef: uuid.NewString(), ComputeGeneration: 1, ProviderIdentityHash: "sha256:" + strings.Repeat("c", 64), DataRef: uuid.NewString(), DataDiskSerial: "soha49data01", DataDiskBytes: 8 << 30,
		PolicyRevision: "sha256:" + strings.Repeat("a", 64), IssuedAt: now, ExpiresAt: now.Add(time.Hour)}
	store := &personalGuestMemory{current: true}
	service, err := NewPersonalGuestService(store, func(context.Context, guest.Binding) error { return nil }, store)
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return now }
	item, token, err := service.Prepare(context.Background(), binding, "sha256:"+strings.Repeat("b", 64), 2, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	identity := networkidentity.Identity{Scope: networkidentity.ScopePersonalGuest, Kind: "guest", ID: item.Binding.ID, CertificateFingerprint: item.CertificateFingerprint}
	return service, store, identity, guest.Enrollment{SchemaVersion: guest.EnrollmentVersion, EnrollmentID: item.EnrollmentID}, "Bearer " + token
}

func TestPersonalGuestConstructorRequiresAuthority(t *testing.T) {
	store := &personalGuestMemory{}
	var nilStore *personalGuestMemory
	for _, deps := range []struct {
		store PersonalGuestStore
		auth  func(context.Context, guest.Binding) error
		audit PersonalGuestAudit
	}{{nilStore, func(context.Context, guest.Binding) error { return nil }, store}, {store, nil, store}, {store, func(context.Context, guest.Binding) error { return nil }, nilStore}} {
		if _, err := NewPersonalGuestService(deps.store, deps.auth, deps.audit); err == nil {
			t.Fatal("missing required guest authority accepted")
		}
	}
}

func TestPersonalGuestEnrollsOnceAndRecoversBindingWithoutChallenge(t *testing.T) {
	s, store, identity, request, authorization := personalGuestFixture(t)
	ctx := context.Background()
	if _, err := s.Binding(ctx, identity); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("pending guest read binding: %v", err)
	}
	if _, err := s.Enroll(ctx, identity, "Bearer "+strings.Repeat("A", 43), request); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("wrong challenge accepted: %v", err)
	}
	binding, err := s.Enroll(ctx, identity, authorization, request)
	if err != nil || binding != store.item.Binding {
		t.Fatalf("enrollment: %v", err)
	}
	if _, err := s.Enroll(ctx, identity, authorization, request); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("challenge replay accepted: %v", err)
	}
	if recovered, err := s.Binding(ctx, identity); err != nil || recovered != binding {
		t.Fatalf("lost enrollment response recovery: %v", err)
	}
	encoded, err := json.Marshal(store.audits)
	if err != nil || strings.Contains(string(encoded), strings.TrimPrefix(authorization, "Bearer ")) || strings.Contains(string(encoded), store.item.ChallengeHash) || len(store.audits) != 2 {
		t.Fatal("registration audit missing or contains challenge material")
	}
}

func TestPersonalGuestRejectsWrongCertificateScopeAndExpiredChallenge(t *testing.T) {
	for name, mutate := range map[string]func(*PersonalGuestService, *networkidentity.Identity, *guest.Enrollment){
		"network credential": func(_ *PersonalGuestService, i *networkidentity.Identity, _ *guest.Enrollment) {
			i.Scope = networkidentity.ScopeNetworkControl
		},
		"wrong kind": func(_ *PersonalGuestService, i *networkidentity.Identity, _ *guest.Enrollment) { i.Kind = "endpoint" },
		"wrong binding": func(_ *PersonalGuestService, i *networkidentity.Identity, _ *guest.Enrollment) {
			i.ID = uuid.NewString()
		},
		"wrong certificate": func(_ *PersonalGuestService, i *networkidentity.Identity, _ *guest.Enrollment) {
			i.CertificateFingerprint = "sha256:" + strings.Repeat("c", 64)
		},
		"wrong enrollment": func(_ *PersonalGuestService, _ *networkidentity.Identity, e *guest.Enrollment) {
			e.EnrollmentID = uuid.NewString()
		},
		"expired challenge": func(s *PersonalGuestService, _ *networkidentity.Identity, _ *guest.Enrollment) {
			now := s.now().Add(time.Minute)
			s.now = func() time.Time { return now }
		},
	} {
		t.Run(name, func(t *testing.T) {
			s, store, identity, request, authorization := personalGuestFixture(t)
			mutate(s, &identity, &request)
			if _, err := s.Enroll(context.Background(), identity, authorization, request); !errors.Is(err, apperrors.ErrUnauthorized) || store.item.Status != "pending" {
				t.Fatalf("invalid guest consumed enrollment: %v", err)
			}
		})
	}
}

func TestPersonalGuestHeartbeatUsesCurrentAuthorizationAndFrozenGeneration(t *testing.T) {
	s, store, identity, request, authorization := personalGuestFixture(t)
	ctx := context.Background()
	if _, err := s.Enroll(ctx, identity, authorization, request); err != nil {
		t.Fatal(err)
	}
	heartbeat := guest.Heartbeat{SchemaVersion: guest.HeartbeatVersion, BindingID: identity.ID, BindingVersion: 1, ComputeGeneration: 1, Sequence: 1, ObservedAt: s.now()}
	if err := s.Heartbeat(ctx, identity, heartbeat); err != nil {
		t.Fatal(err)
	}
	if err := s.Heartbeat(ctx, identity, heartbeat); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("heartbeat replay accepted: %v", err)
	}
	heartbeat.Sequence++
	heartbeat.ComputeGeneration++
	if err := s.Heartbeat(ctx, identity, heartbeat); !errors.Is(err, apperrors.ErrConflict) || store.item.LastSequence != 1 {
		t.Fatalf("changed compute generation recorded: %v", err)
	}
	heartbeat.ComputeGeneration--
	s.authorizeOwner = func(context.Context, guest.Binding) error { return apperrors.ErrAccessDenied }
	if err := s.Heartbeat(ctx, identity, heartbeat); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("current permission revocation ignored: %v", err)
	}
	s.authorizeOwner = func(context.Context, guest.Binding) error { return nil }
	store.current = false
	if _, err := s.Binding(ctx, identity); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("revoked current owner read binding: %v", err)
	}
	if err := s.Heartbeat(ctx, identity, heartbeat); !errors.Is(err, apperrors.ErrAccessDenied) || store.item.LastSequence != 1 {
		t.Fatalf("revoked current owner wrote heartbeat: %v", err)
	}
}

func TestPersonalGuestEnrollmentConcurrentConsumption(t *testing.T) {
	s, _, identity, request, authorization := personalGuestFixture(t)
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			if _, err := s.Enroll(context.Background(), identity, authorization, request); err == nil {
				accepted.Add(1)
			} else if !errors.Is(err, apperrors.ErrUnauthorized) {
				t.Errorf("concurrent enrollment: %v", err)
			}
		})
	}
	wg.Wait()
	if accepted.Load() != 1 {
		t.Fatalf("enrollment consumed %d times", accepted.Load())
	}
}
