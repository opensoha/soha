package resource

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	contractresource "github.com/opensoha/soha-contracts/resource"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type executionMemoryStore struct {
	mu          sync.Mutex
	records     map[string]domainresource.PodExecutionRecord
	finishError bool
}

func (s *executionMemoryStore) Get(_ context.Context, actor, id, key string) (domainresource.PodExecutionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, record := range s.records {
		if record.ActorID == actor && (id != "" && record.Receipt.ID == id || id == "" && record.KeyHash == key) {
			return record, nil
		}
	}
	return domainresource.PodExecutionRecord{}, apperrors.ErrNotFound
}
func (s *executionMemoryStore) Claim(_ context.Context, record domainresource.PodExecutionRecord) (domainresource.PodExecutionRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.records == nil {
		s.records = make(map[string]domainresource.PodExecutionRecord)
	}
	key := record.ActorID + "/" + record.KeyHash
	if old, ok := s.records[key]; ok {
		if old.InputHash != record.InputHash {
			return old, false, apperrors.ErrConflict
		}
		return old, false, nil
	}
	s.records[key] = record
	return record, true, nil
}
func (s *executionMemoryStore) Finish(_ context.Context, record domainresource.PodExecutionRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.finishError {
		return errors.New("storage unavailable")
	}
	s.records[record.ActorID+"/"+record.KeyHash] = record
	return nil
}
func (s *executionMemoryStore) Cleanup(context.Context) error { return nil }

type podExecutionIdentityStub struct{ revoked atomic.Bool }

func (s *podExecutionIdentityStub) CurrentStreamingPrincipal(_ context.Context, original domainidentity.Principal, _ domainidentity.AccessContext) (domainidentity.Principal, error) {
	if s.revoked.Load() {
		return original, apperrors.ErrUnauthorized
	}
	return original, nil
}

type podExecutionDirectStub struct {
	DirectPods
	calls   atomic.Int64
	execute func(context.Context, contractresource.PodArgvInput) (contractresource.PodArgvResult, error)
}

func (s *podExecutionDirectStub) InspectPodExecutionTarget(_ context.Context, _, ns, name, uid, container string) (contractresource.PodExecutionTarget, error) {
	if container == "" {
		container = "main"
	}
	return contractresource.PodExecutionTarget{Namespace: ns, PodName: name, UID: uid, Container: container, CheckedAt: time.Now()}, nil
}
func (s *podExecutionDirectStub) ExecutePodArgv(ctx context.Context, _, _, _ string, input contractresource.PodArgvInput) (contractresource.PodArgvResult, error) {
	s.calls.Add(1)
	if s.execute != nil {
		return s.execute(ctx, input)
	}
	return testPodExecutionResult(input), nil
}
func testPodExecutionResult(input contractresource.PodArgvInput) contractresource.PodArgvResult {
	code := int64(0)
	return contractresource.PodArgvResult{Target: contractresource.PodExecutionTarget{Namespace: "team-a", PodName: "app", UID: input.ExpectedUID, Container: input.Container, CheckedAt: time.Now()}, Output: contractresource.PodExecutionOutput{Status: "succeeded", ExitCode: &code, Stdout: "literal", StdoutBytes: 7, StdoutEncoding: "utf-8", StderrEncoding: "utf-8", MaxOutputBytes: input.MaxOutputBytes, UIDVerifiedBeforeStart: true}}
}
func podExecutionTestService() (*Workloads, *executionMemoryStore, *podExecutionDirectStub, *podExecutionIdentityStub) {
	permissions := exactPermissionAuthorizer{resourcePermissionKey("workloads", "Pod", domainaccess.ActionExec): true}
	access := conditionalService(&resourceUpdatePlanDirectStub{}, permissions).resourceAccess
	store, direct, identity := &executionMemoryStore{}, &podExecutionDirectStub{}, &podExecutionIdentityStub{}
	return &Workloads{resourceAccess: access, directPods: direct, executions: store, executionIdentity: identity}, store, direct, identity
}

var podExecutionTestPrincipal = domainidentity.Principal{UserID: "actor"}

func podExecutionTestInput() contractresource.PodArgvInput {
	return contractresource.PodArgvInput{ExpectedUID: "reviewed", Argv: []string{"printf", "$(not-a-shell)"}}
}

func TestPodExecutionOriginalKeyNeverRepeatsAnAttempt(t *testing.T) {
	for _, mode := range []string{"success", "transport-lost", "persistence-lost"} {
		t.Run(mode, func(t *testing.T) {
			service, store, direct, _ := podExecutionTestService()
			if mode == "transport-lost" {
				direct.execute = func(context.Context, contractresource.PodArgvInput) (contractresource.PodArgvResult, error) {
					return contractresource.PodArgvResult{}, errors.New("private-provider-marker")
				}
			}
			store.finishError = mode == "persistence-lost"
			first, err := service.ExecutePodArgv(t.Context(), podExecutionTestPrincipal, domainidentity.AccessContext{}, "cluster-a", "team-a", "app", "original", podExecutionTestInput())
			if mode == "persistence-lost" {
				if !errors.Is(err, apperrors.ErrServiceUnavailable) {
					t.Fatal(err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			for range 3 {
				item, err := service.ExecutePodArgv(t.Context(), podExecutionTestPrincipal, domainidentity.AccessContext{}, "cluster-a", "team-a", "app", "original", podExecutionTestInput())
				if err != nil || mode != "persistence-lost" && item.ID != first.ID || direct.calls.Load() != 1 {
					t.Fatalf("repeated attempt: %+v %v calls=%d", item, err, direct.calls.Load())
				}
			}
			changed := podExecutionTestInput()
			changed.Argv = []string{"different"}
			if _, err := service.ExecutePodArgv(t.Context(), podExecutionTestPrincipal, domainidentity.AccessContext{}, "cluster-a", "team-a", "app", "original", changed); !errors.Is(err, apperrors.ErrConflict) {
				t.Fatal(err)
			}
		})
	}
}

func TestPodExecutionConcurrentOriginalKeyRunsOnce(t *testing.T) {
	service, _, direct, _ := podExecutionTestService()
	var workers sync.WaitGroup
	errorsFound := make(chan error, 12)
	for range 12 {
		workers.Go(func() {
			_, err := service.ExecutePodArgv(t.Context(), podExecutionTestPrincipal, domainidentity.AccessContext{}, "cluster-a", "team-a", "app", "original", podExecutionTestInput())
			if err != nil {
				errorsFound <- err
			}
		})
	}
	workers.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Error(err)
	}
	if direct.calls.Load() != 1 {
		t.Fatalf("executed %d times", direct.calls.Load())
	}
}

func TestPodExecutionRevocationCancelsAndHidesOutput(t *testing.T) {
	service, _, direct, identity := podExecutionTestService()
	direct.execute = func(ctx context.Context, _ contractresource.PodArgvInput) (contractresource.PodArgvResult, error) {
		identity.revoked.Store(true)
		select {
		case <-ctx.Done():
			return contractresource.PodArgvResult{}, ctx.Err()
		case <-time.After(6 * time.Second):
			return contractresource.PodArgvResult{}, errors.New("revocation did not cancel")
		}
	}
	item, err := service.ExecutePodArgv(t.Context(), podExecutionTestPrincipal, domainidentity.AccessContext{}, "cluster-a", "team-a", "app", "original", podExecutionTestInput())
	if !errors.Is(err, apperrors.ErrAccessDenied) || item.Output != nil {
		t.Fatalf("revoked result: %+v %v", item, err)
	}
}

func TestPodExecutionPermissionAndReceiptOwnership(t *testing.T) {
	service, _, direct, _ := podExecutionTestService()
	item, err := service.ExecutePodArgv(t.Context(), podExecutionTestPrincipal, domainidentity.AccessContext{}, "cluster-a", "team-a", "app", "original", podExecutionTestInput())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.GetPodExecution(t.Context(), domainidentity.Principal{UserID: "other"}, "cluster-a", "team-a", "app", item.ID, ""); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := service.GetPodExecution(t.Context(), podExecutionTestPrincipal, "cluster-a", "team-a", "different", item.ID, ""); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatal(err)
	}
	service.authorizer = exactPermissionAuthorizer{}
	if _, err := service.GetPodExecution(t.Context(), podExecutionTestPrincipal, "cluster-a", "team-a", "app", item.ID, ""); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatal(err)
	}
	if _, err := service.ExecutePodArgv(t.Context(), podExecutionTestPrincipal, domainidentity.AccessContext{}, "cluster-a", "team-a", "app", "original", podExecutionTestInput()); !errors.Is(err, apperrors.ErrAccessDenied) || direct.calls.Load() != 1 {
		t.Fatal(err)
	}
}
