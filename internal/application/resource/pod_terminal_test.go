package resource

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	contractresource "github.com/opensoha/soha/contracts/resource"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type terminalMemoryStore struct {
	mu             sync.Mutex
	records        map[string]domainresource.PodTerminalRecord
	finishFailures int
}

func (store *terminalMemoryStore) Create(_ context.Context, record domainresource.PodTerminalRecord) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.records == nil {
		store.records = map[string]domainresource.PodTerminalRecord{}
	}
	store.records[record.Lease.ID] = record
	return nil
}
func (store *terminalMemoryStore) Get(_ context.Context, actor, id string) (domainresource.PodTerminalRecord, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	record, ok := store.records[id]
	if !ok || record.ActorID != actor {
		return domainresource.PodTerminalRecord{}, apperrors.ErrNotFound
	}
	return record, nil
}
func (store *terminalMemoryStore) Consume(_ context.Context, actor, id, binding, ticket string) (domainresource.PodTerminalRecord, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	record, ok := store.records[id]
	if !ok || record.ActorID != actor || record.CredentialHash != binding || record.TicketHash != ticket || record.Lease.Status != "pending" {
		return domainresource.PodTerminalRecord{}, apperrors.ErrConflict
	}
	record.Lease.Status, record.TicketHash = "active", ""
	store.records[id] = record
	return record, nil
}
func (store *terminalMemoryStore) RequestClose(_ context.Context, actor, id string) (domainresource.PodTerminalRecord, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	record, ok := store.records[id]
	if !ok || record.ActorID != actor {
		return domainresource.PodTerminalRecord{}, apperrors.ErrNotFound
	}
	switch record.Lease.Status {
	case "pending":
		now := time.Now()
		record.Lease.Status, record.Lease.ClosedAt = "closed", &now
	case "active":
		record.Lease.Status = "closing"
	}
	record.TicketHash = ""
	store.records[id] = record
	return record, nil
}
func (store *terminalMemoryStore) Finish(_ context.Context, record domainresource.PodTerminalRecord) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.finishFailures > 0 {
		store.finishFailures--
		return errors.New("temporary storage failure")
	}
	old := store.records[record.Lease.ID]
	if old.Lease.Status != "active" && old.Lease.Status != "closing" {
		return apperrors.ErrConflict
	}
	store.records[record.Lease.ID] = record
	return nil
}

func TestNativeTerminalCloseRecoversOriginalFailedPersistenceWithoutReplay(t *testing.T) {
	service, store, direct, _, principal, access := terminalTestService(t)
	created, err := service.Create(t.Context(), principal, access, "cluster-a", "team-a", "app", contractresource.PodTerminalInput{ExpectedUID: "reviewed"})
	if err != nil {
		t.Fatal(err)
	}
	session, err := service.Acquire(t.Context(), principal, access, "cluster-a", "team-a", "app", created.Session.ID, created.Ticket)
	if err != nil {
		t.Fatal(err)
	}
	store.finishFailures = 1
	if _, err = session.Stream(io.NopCloser(strings.NewReader("")), io.Discard, nativeTerminalSizes{}); !errors.Is(err, apperrors.ErrServiceUnavailable) {
		t.Fatalf("lost persistence not reported: %v", err)
	}
	if err = session.Close(); err != nil {
		t.Fatal(err)
	}
	item, err := service.Get(t.Context(), principal, "cluster-a", "team-a", "app", created.Session.ID)
	if err != nil || item.Status != "closed" || item.Result == nil || item.Result.Status != "succeeded" || direct.calls.Load() != 1 {
		t.Fatalf("original outcome not recovered: %+v err=%v attempts=%d", item, err, direct.calls.Load())
	}
}
func (store *terminalMemoryStore) Cleanup(context.Context) error { return nil }

type nativeTerminalDirectStub struct {
	*podExecutionDirectStub
	stream func(context.Context, io.ReadCloser, io.Writer) (contractresource.PodTerminalResult, error)
	calls  atomic.Int64
}

func (stub *nativeTerminalDirectStub) CheckPodTerminal(context.Context, string) error { return nil }
func (stub *nativeTerminalDirectStub) StreamNativePodTerminal(ctx context.Context, _, _, _ string, _ contractresource.PodTerminalInput, stdin io.ReadCloser, stdout io.Writer, _ contractresource.NativeTerminalSizeQueue) (contractresource.PodTerminalResult, error) {
	stub.calls.Add(1)
	if stub.stream != nil {
		return stub.stream(ctx, stdin, stdout)
	}
	zero := int64(0)
	return contractresource.PodTerminalResult{Status: "succeeded", ExitCode: &zero, UIDVerifiedBeforeStart: true}, nil
}

type nativeTerminalSizes struct{}

func (nativeTerminalSizes) Next(context.Context) *contractresource.NativeTerminalSize {
	return &contractresource.NativeTerminalSize{Width: 80, Height: 24}
}

func terminalTestService(t *testing.T) (*PodTerminals, *terminalMemoryStore, *nativeTerminalDirectStub, *podExecutionIdentityStub, domainidentity.Principal, domainidentity.AccessContext) {
	t.Helper()
	workloads, _, oldDirect, identity := podExecutionTestService()
	direct := &nativeTerminalDirectStub{podExecutionDirectStub: oldDirect}
	workloads.directPods = direct
	store := &terminalMemoryStore{}
	service := newPodTerminals(workloads, store, t.Context())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := service.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	})
	principal := domainidentity.Principal{UserID: "actor", AccessTokenID: "original-key"}
	access := domainidentity.AccessContext{ExpiresAt: time.Now().Add(time.Hour)}
	return service, store, direct, identity, principal, access
}

func TestNativeTerminalSingleConnectionAndCredentialBinding(t *testing.T) {
	service, _, direct, _, principal, access := terminalTestService(t)
	created, err := service.Create(t.Context(), principal, access, "cluster-a", "team-a", "app", contractresource.PodTerminalInput{ExpectedUID: "reviewed", Width: 120, Height: 40})
	if err != nil {
		t.Fatal(err)
	}
	if len(created.Ticket) != 43 || created.Session.Target.Container != "main" {
		t.Fatal("invalid session target or ticket")
	}
	other := principal
	other.AccessTokenID = "rotated-key"
	if _, err = service.Acquire(t.Context(), other, access, "cluster-a", "team-a", "app", created.Session.ID, created.Ticket); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("another credential consumed ticket: %v", err)
	}
	var winners atomic.Int64
	var workers sync.WaitGroup
	for range 12 {
		workers.Go(func() {
			session, err := service.Acquire(t.Context(), principal, access, "cluster-a", "team-a", "app", created.Session.ID, created.Ticket)
			if err == nil {
				winners.Add(1)
				defer func() { _ = session.Close() }()
				if session.InitialSize().Width != 120 {
					t.Error("initial size lost")
				}
				if _, err = session.Stream(io.NopCloser(strings.NewReader("")), io.Discard, nativeTerminalSizes{}); err != nil {
					t.Error(err)
				}
			} else if !errors.Is(err, apperrors.ErrConflict) {
				t.Error(err)
			}
		})
	}
	workers.Wait()
	if winners.Load() != 1 || direct.calls.Load() != 1 {
		t.Fatalf("connections=%d provider attempts=%d", winners.Load(), direct.calls.Load())
	}
	item, err := service.Get(t.Context(), principal, "cluster-a", "team-a", "app", created.Session.ID)
	if err != nil || item.Status != "closed" || item.Result == nil || item.Result.Status != "succeeded" {
		t.Fatalf("metadata=%+v err=%v", item, err)
	}
}

func TestNativeTerminalRevocationAndCloseCancelStream(t *testing.T) {
	for _, scenario := range []string{"revoked", "close", "shutdown"} {
		t.Run(scenario, func(t *testing.T) {
			service, _, direct, identity, principal, access := terminalTestService(t)
			started := make(chan struct{})
			direct.stream = func(ctx context.Context, _ io.ReadCloser, stdout io.Writer) (contractresource.PodTerminalResult, error) {
				close(started)
				<-ctx.Done()
				if _, err := stdout.Write([]byte("must-not-leak")); err == nil {
					t.Error("output after cancellation")
				}
				return contractresource.PodTerminalResult{Status: "canceled", ReasonCode: "request_canceled"}, nil
			}
			created, err := service.Create(t.Context(), principal, access, "cluster-a", "team-a", "app", contractresource.PodTerminalInput{ExpectedUID: "reviewed"})
			if err != nil {
				t.Fatal(err)
			}
			session, err := service.Acquire(t.Context(), principal, access, "cluster-a", "team-a", "app", created.Session.ID, created.Ticket)
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan struct{})
			go func() {
				defer close(done)
				defer func() { _ = session.Close() }()
				_, _ = session.Stream(io.NopCloser(strings.NewReader("")), io.Discard, nativeTerminalSizes{})
			}()
			<-started
			switch scenario {
			case "revoked":
				identity.revoked.Store(true)
			case "close":
				if _, err := service.Close(t.Context(), principal, "cluster-a", "team-a", "app", created.Session.ID); err != nil {
					t.Fatal(err)
				}
			case "shutdown":
				ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
				defer cancel()
				if err := service.Shutdown(ctx); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case <-done:
			case <-time.After(4 * time.Second):
				t.Fatal("stream did not stop")
			}
			if direct.calls.Load() != 1 {
				t.Fatal("stream was replayed")
			}
		})
	}
}

func TestNativeTerminalAbortBeforeUpgradeCannotStartLater(t *testing.T) {
	service, _, direct, _, principal, access := terminalTestService(t)
	created, err := service.Create(t.Context(), principal, access, "cluster-a", "team-a", "app", contractresource.PodTerminalInput{ExpectedUID: "reviewed", TimeoutSeconds: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !created.Session.DeadlineAt.Equal(created.Session.TicketExpiresAt) {
		t.Fatal("short lease ticket exceeds deadline")
	}
	session, err := service.Acquire(t.Context(), principal, access, "cluster-a", "team-a", "app", created.Session.ID, created.Ticket)
	if err != nil {
		t.Fatal(err)
	}
	if err = session.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = session.Stream(io.NopCloser(strings.NewReader("")), io.Discard, nativeTerminalSizes{}); !errors.Is(err, apperrors.ErrConflict) || direct.calls.Load() != 0 {
		t.Fatalf("aborted lease started a shell: %v", err)
	}
}
