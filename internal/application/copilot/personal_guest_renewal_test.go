package copilot

import (
	"context"
	"errors"
	guest "github.com/opensoha/soha-contracts/agent/guest"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"strings"
	"testing"
	"time"
)

type renewalMemory struct {
	*personalGuestMemory
	pending  map[string]domain.PersonalGuestRenewal
	onLocked func()
}

func (m *renewalMemory) LockPersonalGuestRenewal(ctx context.Context, item domain.PersonalGuestRecord) error {
	if err := m.CheckPersonalGuestBinding(ctx, item); err != nil {
		return err
	}
	if m.item.Binding != item.Binding || m.item.CertificateFingerprint != item.CertificateFingerprint {
		return apperrors.ErrConflict
	}
	return nil
}
func (m *renewalMemory) GetPersonalGuestRenewal(_ context.Context, id string) (domain.PersonalGuestRenewal, error) {
	item, ok := m.pending[id]
	if !ok {
		return item, apperrors.ErrNotFound
	}
	return item, nil
}
func (m *renewalMemory) SavePersonalGuestRenewal(_ context.Context, item domain.PersonalGuestRenewal) error {
	m.pending[item.Result.RequestID] = item
	return nil
}
func (m *renewalMemory) CommitPersonalGuestRenewal(_ context.Context, _ domain.PersonalGuestRecord, item domain.PersonalGuestRenewal, _ time.Time) error {
	m.item.Binding, m.item.CertificateFingerprint = item.Result.Binding, item.NewFingerprint
	item.Committed = true
	m.pending[item.Result.RequestID] = item
	return nil
}

type renewalIssuer struct{ calls int }

func (i *renewalIssuer) Issue(context.Context, guest.Binding, string) (string, string, string, error) {
	i.calls++
	return "public-certificate", "public-ca", "sha256:" + strings.Repeat("d", 64), nil
}

func TestPersonalGuestRenewalPreservesSequenceAndCurrentAuthority(t *testing.T) {
	s, memory, identity, enrollment, authorization := personalGuestFixture(t)
	if _, err := s.Enroll(context.Background(), identity, authorization, enrollment); err != nil {
		t.Fatal(err)
	}
	memory.item.LastSequence = 154
	store := &renewalMemory{personalGuestMemory: memory, pending: map[string]domain.PersonalGuestRenewal{}}
	s.store = store
	issuer := &renewalIssuer{}
	s.ConfigureRenewal(issuer)
	input := guest.Renewal{SchemaVersion: guest.RenewalVersion, RequestID: "11111111-1111-4111-8111-111111111111", BindingVersion: memory.item.Binding.Version, CSRPEM: "signed-public-csr"}
	result, err := s.Renew(context.Background(), identity, input)
	if err != nil || memory.item.CertificateFingerprint != identity.CertificateFingerprint {
		t.Fatal("issuance prematurely promoted authority", err)
	}
	again, err := s.Renew(context.Background(), identity, input)
	if err != nil || again != result || issuer.calls != 1 {
		t.Fatal("lost response reissued certificate", err)
	}
	changed := input
	changed.CSRPEM = "different-key"
	if _, err := s.Renew(context.Background(), identity, changed); err == nil {
		t.Fatal("changed CSR adopted the old issuance")
	}
	nextIdentity := identity
	nextIdentity.CertificateFingerprint = store.pending[input.RequestID].NewFingerprint
	memory.current = false
	if _, err := s.CommitRenewal(context.Background(), nextIdentity, input.RequestID); err == nil {
		t.Fatal("revoked owner committed pending authority")
	}
	memory.current = true
	binding, err := s.CommitRenewal(context.Background(), nextIdentity, input.RequestID)
	if err != nil || binding != result.Binding || memory.item.LastSequence != 154 {
		t.Fatal("renewal changed identity or reset sequence", err)
	}
	if _, err := s.CommitRenewal(context.Background(), nextIdentity, input.RequestID); err != nil {
		t.Fatal("lost commit response cannot recover", err)
	}
	if _, err := s.Binding(context.Background(), identity); err == nil {
		t.Fatal("old certificate survived promotion")
	}
	if _, err := s.Binding(context.Background(), nextIdentity); err != nil {
		t.Fatal(err)
	}
}

func (m *renewalMemory) ReadLockedPersonalGuest(ctx context.Context, _ string) (domain.PersonalGuestRecord, error) {
	if m.onLocked != nil {
		m.onLocked()
	}
	return m.item, m.CheckPersonalGuestBinding(ctx, m.item)
}

func TestPersonalGuestRenewalRechecksExpiryAfterLock(t *testing.T) {
	s, memory, identity, enrollment, authorization := personalGuestFixture(t)
	if _, err := s.Enroll(t.Context(), identity, authorization, enrollment); err != nil {
		t.Fatal(err)
	}
	store := &renewalMemory{personalGuestMemory: memory, pending: map[string]domain.PersonalGuestRenewal{}}
	s.store = store
	s.ConfigureRenewal(&renewalIssuer{})
	input := guest.Renewal{SchemaVersion: guest.RenewalVersion, RequestID: "11111111-1111-4111-8111-111111111111", BindingVersion: 1, CSRPEM: "signed-public-csr"}
	result, err := s.Renew(t.Context(), identity, input)
	if err != nil {
		t.Fatal(err)
	}
	store.onLocked = func() { s.now = func() time.Time { return result.CommitBefore.Add(time.Second) } }
	next := identity
	next.CertificateFingerprint = store.pending[input.RequestID].NewFingerprint
	if _, err := s.CommitRenewal(t.Context(), next, input.RequestID); !errors.Is(err, domain.ErrPersonalGuestRenewalExpired) {
		t.Fatal("lock delay bypassed expiry", err)
	}
	if memory.item.Binding.Version != 1 {
		t.Fatal("expired pending authority promoted")
	}
}

func TestPersonalGuestExpiredRenewalKeepsCurrentIdentityRecoverable(t *testing.T) {
	s, memory, identity, enrollment, authorization := personalGuestFixture(t)
	if _, err := s.Enroll(context.Background(), identity, authorization, enrollment); err != nil {
		t.Fatal(err)
	}
	store := &renewalMemory{personalGuestMemory: memory, pending: map[string]domain.PersonalGuestRenewal{}}
	s.store = store
	s.ConfigureRenewal(&renewalIssuer{})
	input := guest.Renewal{SchemaVersion: guest.RenewalVersion, RequestID: "11111111-1111-4111-8111-111111111111", BindingVersion: 1, CSRPEM: "signed-public-csr"}
	result, err := s.Renew(context.Background(), identity, input)
	if err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return result.CommitBefore.Add(time.Second) }
	if _, err := s.Renew(context.Background(), identity, input); !errors.Is(err, domain.ErrPersonalGuestRenewalExpired) {
		t.Fatal("expired request did not produce explicit expiry", err)
	}
	if _, err := s.Binding(context.Background(), identity); err != nil {
		t.Fatal("unexpired original identity lost authority", err)
	}
	next := identity
	next.CertificateFingerprint = store.pending[input.RequestID].NewFingerprint
	if _, err := s.CommitRenewal(context.Background(), next, input.RequestID); !errors.Is(err, domain.ErrPersonalGuestRenewalExpired) {
		t.Fatal("expired pending certificate was committed", err)
	}
}
