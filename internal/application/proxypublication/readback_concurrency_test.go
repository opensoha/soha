package proxypublication

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	api "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	access "github.com/opensoha/soha/internal/domain/networkaccess"
	proxy "github.com/opensoha/soha/internal/domain/networkproxy"
	domain "github.com/opensoha/soha/internal/domain/proxypublication"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

// The first two reads receive independent snapshots at the same version. Saves
// enforce both public revision and private version, like the SQL repository.
type concurrentReadbackStore struct {
	*bundleFake
	mu                       sync.Mutex
	record                   domain.BundleRecord
	reads, writes, conflicts int
	saveCalls                int
	bothRead                 chan struct{}
	beforeSave               func(*concurrentReadbackStore)
	afterConflict            func()
	policyError              error
}

func cloneReadbackRecord(record domain.BundleRecord) (domain.BundleRecord, error) {
	raw, err := json.Marshal(record)
	if err != nil {
		return domain.BundleRecord{}, err
	}
	var copy domain.BundleRecord
	if err = json.Unmarshal(raw, &copy); err != nil {
		return copy, err
	}
	copy.Version = record.Version
	return copy, nil
}

func (s *concurrentReadbackStore) GetBundle(ctx context.Context, _ string, _ string) (domain.BundleRecord, error) {
	s.mu.Lock()
	record, err := cloneReadbackRecord(s.record)
	s.reads++
	read := s.reads
	if read == 2 && s.bothRead != nil {
		close(s.bothRead)
	}
	s.mu.Unlock()
	if err != nil {
		return record, err
	}
	if read <= 2 && s.bothRead != nil {
		select {
		case <-s.bothRead:
		case <-ctx.Done():
			return record, ctx.Err()
		}
	}
	return record, nil
}

func (s *concurrentReadbackStore) SaveBundle(_ context.Context, record domain.BundleRecord, expected int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.saveCalls++
	if s.beforeSave != nil {
		s.beforeSave(s)
	}
	if s.policyError != nil {
		return s.policyError
	}
	if expected != s.record.Public.Revision || record.Version != s.record.Version {
		s.conflicts++
		if s.afterConflict != nil {
			s.afterConflict()
		}
		return ErrBundleCASConflict
	}
	copy, err := cloneReadbackRecord(record)
	if err != nil {
		return err
	}
	copy.Version++
	s.record = copy
	s.writes++
	return nil
}

type readbackProtection struct {
	destinationFake
	value *access.ProtectedDestinations
	err   error
}

func (p readbackProtection) CurrentProtection(context.Context) (*access.ProtectedDestinations, error) {
	return p.value, p.err
}

func readbackRetryFixture() (*Bundles, *concurrentReadbackStore, api.NetworkProxyBundleReadback) {
	b, base, _ := ingressFixture()
	base.record.Version = 1
	base.record.Public.State = "prepare"
	base.record.Readbacks = map[string]api.NetworkProxyBundleReadback{}
	s := &concurrentReadbackStore{bundleFake: base, record: base.record}
	b.store = s
	input := api.NetworkProxyBundleReadback{BundleID: "bundle-1", Revision: 1, ConfigurationRevision: 1, ServerHash: base.record.Public.ServerHash, ProcessEpoch: "server-epoch", ObservedAt: b.now(), IngressReady: true, ProtectionReady: true}
	return b, s, input
}

func TestServerReadyRejectsInvalidStateInitiallyAndAfterCAS(t *testing.T) {
	faults := []string{"runtime revoked", "revoked", "failed", "unknown state", "prepared expired", "pool owner", "pool hash", "member removed", "member stopped", "configuration revision", "hash drift", "native epoch", "lease missing", "lease expired", "last seen missing", "last seen stale", "last seen future", "protection floor", "protection missing", "protection zero", "protection unavailable", "source changed", "org changed", "public revision", "version not advanced", "second CAS"}
	for _, retry := range []bool{false, true} {
		for _, fault := range faults {
			if !retry && (fault == "version not advanced" || fault == "second CAS") {
				continue
			}
			t.Run(fmt.Sprintf("retry=%v/%s", retry, fault), func(t *testing.T) {
				b, store, input := readbackRetryFixture()
				now := b.now()
				mutate := func() { mutateReadbackState(t, fault, b, store, now) }
				if retry {
					store.beforeSave = func(s *concurrentReadbackStore) {
						if s.saveCalls == 1 {
							s.record.Version++
							s.record.Readbacks["other-member"] = input
						}
					}
					store.afterConflict = mutate
				} else {
					mutate()
				}
				err := b.ServerReady(context.Background(), networkidentity.Identity{ID: "proxy-1"}, "master", input)
				if err == nil || store.writes != 0 || store.saveCalls > 2 {
					t.Fatalf("invalid readback saved: err=%v saves=%d writes=%d", err, store.saveCalls, store.writes)
				}
				if retry {
					if _, ok := store.record.Readbacks["other-member"]; !ok {
						t.Fatal("concurrent member receipt lost")
					}
				}
				if fault == "second CAS" && (store.saveCalls != 2 || !errors.Is(err, ErrBundleCASConflict)) {
					t.Fatalf("second CAS conflict swallowed: calls=%d err=%v", store.saveCalls, err)
				}
				if !retry && fault == "org changed" && (store.saveCalls != 1 || store.reads != 1) {
					t.Fatal("general conflict retried")
				}
			})
		}
	}
}

func TestServerReadyPreservesActivatedStateAndCurrentPeerReceiptOnRetry(t *testing.T) {
	b, store, input := readbackRetryFixture()
	store.beforeSave = func(s *concurrentReadbackStore) {
		if s.saveCalls == 1 {
			s.record.Version++
			s.record.Readbacks["other-member"] = input
			s.record.Public.State = "activated"
			s.record.Public.ActiveRevision = 1
			s.record.Public.ValidUntil = b.now().Add(-time.Hour)
			s.record.ActivationRequest = "current-activation"
		}
	}
	if err := b.ServerReady(context.Background(), networkidentity.Identity{ID: "proxy-1"}, "master", input); err != nil {
		t.Fatal(err)
	}
	if store.saveCalls != 2 || store.writes != 1 || len(store.record.Readbacks) != 2 || store.record.Public.State != "activated" || store.record.ActivationRequest != "current-activation" {
		t.Fatalf("latest activated state or peer receipt lost: %+v", store.record)
	}
}

func TestServerReadyConcurrentMembersPreserveBothReadbacks(t *testing.T) {
	for _, state := range []string{"prepare", "activated"} {
		t.Run(state, func(t *testing.T) {
			b, base, _ := ingressFixture()
			now := b.now()
			base.record.Version = 1
			base.record.Public.State = api.NetworkProxyBundleState(state)
			base.record.Public.ServerHash = "sha256:" + strings.Repeat("a", 64)
			base.record.Readbacks = map[string]api.NetworkProxyBundleReadback{}
			base.record.Pool.Members = []domain.PoolMember{{RuntimeID: "proxy-1", Running: true, Serving: true}, {RuntimeID: "proxy-2", Running: true, Serving: true}}
			fixtureValue[*poolFake](t, b.pool).pool = base.record.Pool
			states := fixtureValue[desiredFake](t, b.desired)
			first := states.states[0]
			first.ContentHash = base.record.Public.ServerHash
			first.ProcessEpoch = "server-1"
			second := first
			second.InstanceID, second.ProcessEpoch = "proxy-2", "server-2"
			b.desired = desiredFake{states: []proxy.MemberConfigurationState{first, second}}
			store := &concurrentReadbackStore{bundleFake: base, record: base.record, bothRead: make(chan struct{})}
			b.store = store
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			results := make(chan error, 2)
			for _, member := range []struct{ id, epoch string }{{"proxy-1", "server-1"}, {"proxy-2", "server-2"}} {
				go func(id, epoch string) {
					results <- b.ServerReady(ctx, networkidentity.Identity{ID: id}, "master", api.NetworkProxyBundleReadback{BundleID: "bundle-1", Revision: 1, ConfigurationRevision: 1, ServerHash: base.record.Public.ServerHash, ProcessEpoch: epoch, ObservedAt: now, IngressReady: true, ProtectionReady: true})
				}(member.id, member.epoch)
			}
			failures, casConflicts := 0, 0
			for i := 0; i < 2; i++ {
				if err := <-results; err != nil {
					failures++
					if errors.Is(err, apperrors.ErrConflict) {
						casConflicts++
					} else {
						t.Errorf("unexpected readback error: %v", err)
					}
				}
			}
			store.mu.Lock()
			defer store.mu.Unlock()
			if failures != 0 || store.writes != 2 || len(store.record.Readbacks) != 2 {
				t.Fatalf("concurrent qualified readbacks did not both persist: failures=%d conflict_errors=%d reads=%d writes=%d strict_CAS_conflicts=%d receipts=%d version=%d", failures, casConflicts, store.reads, store.writes, store.conflicts, len(store.record.Readbacks), store.record.Version)
			}
			want := api.NetworkProxyBundleState("client_eligible")
			if state == "activated" {
				want = "activated"
			}
			if store.record.Public.State != want || store.record.Public.Revision != 1 || store.record.Version != 3 {
				t.Fatalf("publication state regressed: %+v", store.record.Public)
			}
		})
	}
}

func mutateReadbackState(t *testing.T, fault string, b *Bundles, store *concurrentReadbackStore, now time.Time) {
	t.Helper()
	switch fault {
	case "runtime revoked":
		b.runtime = runtimeFake{err: apperrors.ErrAccessDenied}
	case "revoked", "failed":
		store.record.Public.State = api.NetworkProxyBundleState(fault)
	case "unknown state":
		store.record.Public.State = "unexpected"
	case "prepared expired":
		store.record.Public.ValidUntil = now.Add(-time.Second)
	case "pool owner":
		fixtureValue[*poolFake](t, b.pool).pool.OwnerEpoch++
	case "pool hash":
		fixtureValue[*poolFake](t, b.pool).pool.MemberSetHash = "other"
	case "member removed":
		fixtureValue[*poolFake](t, b.pool).pool.Members = nil
	case "member stopped":
		fixtureValue[*poolFake](t, b.pool).pool.Members[0].Running = false
	case "protection floor":
		b.destination = readbackProtection{value: &access.ProtectedDestinations{PolicyVersion: 2}}
	case "protection missing":
		b.destination = readbackProtection{}
	case "protection zero":
		b.destination = readbackProtection{value: &access.ProtectedDestinations{}}
	case "protection unavailable":
		b.destination = readbackProtection{err: apperrors.ErrServiceUnavailable}
	case "source changed":
		store.policyError = apperrors.ErrAccessDenied
	case "org changed":
		store.policyError = apperrors.ErrConflict
	case "public revision":
		store.record.Public.Revision++
	case "version not advanced":
		store.record.Version = 1
	case "second CAS":
		store.beforeSave = func(s *concurrentReadbackStore) { s.record.Version++ }
	default:
		mutateReadbackConfiguration(t, fault, b, now)
	}
}

func mutateReadbackConfiguration(t *testing.T, fault string, b *Bundles, now time.Time) {
	t.Helper()
	state := fixtureValue[desiredFake](t, b.desired)
	current := &state.states[0]
	switch fault {
	case "configuration revision":
		current.DesiredRevision++
		current.AppliedRevision++
	case "hash drift":
		current.AppliedHash = "drift"
	case "native epoch":
		current.ProcessEpoch = "different-boot"
	case "lease missing":
		current.ConfigurationValidUntil = nil
	case "lease expired":
		current.ConfigurationValidUntil = &now
	case "last seen missing":
		current.LastSeenAt = nil
	case "last seen stale":
		old := now.Add(-31 * time.Second)
		current.LastSeenAt = &old
	case "last seen future":
		future := now.Add(6 * time.Second)
		current.LastSeenAt = &future
	}
	b.desired = state
}
