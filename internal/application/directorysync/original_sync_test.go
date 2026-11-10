package directorysync

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	domain "github.com/opensoha/soha/internal/domain/directorysync"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const originalSyncKey = "d154b4d6-a26f-43a6-a150-d92af32f3d19"

type originalSyncRepository struct {
	*repositoryStub
	mu          sync.Mutex
	runs        map[string]domain.Run
	projections atomic.Int32
}

func newOriginalSyncRepository() *originalSyncRepository {
	return &originalSyncRepository{repositoryStub: &repositoryStub{connection: domain.Connection{ID: "owned", ProviderType: domain.ProviderLDAP}, policy: domain.DefaultPolicy("owned")}, runs: make(map[string]domain.Run)}
}
func (r *originalSyncRepository) ClaimSyncRequest(_ context.Context, run domain.Run) (domain.Run, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.runs {
		if existing.IdempotencyKey == run.IdempotencyKey {
			return existing, false, nil
		}
	}
	for _, existing := range r.runs {
		if existing.Status == domain.RunQueued || existing.Status == domain.RunRunning {
			return domain.Run{}, false, apperrors.ErrConflict
		}
	}
	r.runs[run.ID] = run
	return run, true, nil
}
func (r *originalSyncRepository) GetSyncRequest(_ context.Context, _ string, key string) (domain.Run, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, run := range r.runs {
		if run.IdempotencyKey == key {
			return run, nil
		}
	}
	return domain.Run{}, apperrors.ErrNotFound
}
func (r *originalSyncRepository) GetRun(_ context.Context, id string) (domain.Run, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	run, ok := r.runs[id]
	if !ok {
		return domain.Run{}, apperrors.ErrNotFound
	}
	return run, nil
}
func (r *originalSyncRepository) TransitionRun(_ context.Context, id, status string, stats domain.RunStats, code, summary string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	run, ok := r.runs[id]
	if !ok {
		return apperrors.ErrNotFound
	}
	if !domain.CanTransitionRun(run.Status, status) {
		return apperrors.ErrConflict
	}
	run.Status, run.Stats, run.ErrorCode, run.ErrorSummary = status, stats, code, summary
	now := time.Now().UTC()
	if status == domain.RunRunning {
		run.StartedAt = &now
	} else {
		run.FinishedAt = &now
	}
	r.runs[id] = run
	return nil
}
func (r *originalSyncRepository) ApplyProjections(context.Context, string, []domain.Organization, []domain.Person, []domain.Membership, bool) error {
	r.projections.Add(1)
	return nil
}

func (r *originalSyncRepository) TouchSyncRequest(_ context.Context, id string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	run := r.runs[id]
	if run.Status != domain.RunRunning {
		return false, apperrors.ErrConflict
	}
	return run.CancelRequested, nil
}
func (r *originalSyncRepository) RequestSyncCancellation(_ context.Context, id string) (domain.Run, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	run := r.runs[id]
	if run.Status != domain.RunRunning && run.Status != domain.RunQueued {
		return domain.Run{}, apperrors.ErrConflict
	}
	run.CancelRequested = true
	if run.Status == domain.RunQueued {
		run.Status = domain.RunCanceled
	}
	r.runs[id] = run
	return run, nil
}

func (r *originalSyncRepository) FinishCanceledSyncRequest(ctx context.Context, id, summary string) error {
	return r.TransitionRun(ctx, id, domain.RunCanceled, domain.RunStats{}, "canceled", summary)
}

func TestOriginalDirectoryCancellationKeepsSlotUntilCreatorStops(t *testing.T) {
	repo := newOriginalSyncRepository()
	connector := &blockingOriginalConnector{connectorSpy: &connectorSpy{}, entered: make(chan struct{}), release: make(chan struct{})}
	factory := &originalSyncFactory{connector: connector}
	service := New(repo, nil)
	completed := make(chan domain.Run, 1)
	go func() {
		run, err := service.SyncOriginal(t.Context(), "owned", "actor", "session:original", originalSyncKey, factory)
		if err != nil {
			t.Error(err)
		}
		completed <- run
	}()
	<-connector.entered
	stored, _, _ := originalSyncReference("actor", "session:original", originalSyncKey)
	active, err := repo.GetSyncRequest(t.Context(), "owned", stored)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.RequestOriginalCancellation(t.Context(), active.ID, "actor", "token:foreign"); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatal("foreign credential canceled original", err)
	}
	requested, err := service.RequestOriginalCancellation(t.Context(), active.ID, "actor", "session:original")
	if err != nil || !requested.CancelRequested || requested.Status != domain.RunRunning {
		t.Fatal("cancellation prematurely freed active slot", err)
	}
	if _, err := service.SyncOriginal(t.Context(), "owned", "actor", "session:original", "fb78023f-1376-4139-8595-cf4ddffce330", factory); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("new intent overlapped running canceled creator", err)
	}
	select {
	case stopped := <-completed:
		if stopped.Status != domain.RunCanceled || stopped.FinishedAt == nil || repo.projections.Load() != 0 {
			t.Fatal("cancellation did not stop before projection")
		}
	case <-time.After(6 * time.Second):
		t.Fatal("durable cancellation did not reach creator")
	}
	again, err := service.SyncOriginal(t.Context(), "owned", "actor", "session:original", originalSyncKey, factory)
	if err != nil || again.Status != domain.RunCanceled || factory.calls.Load() != 1 {
		t.Fatal("canceled original replayed", err)
	}
}

type originalSyncFactory struct {
	connector Connector
	calls     atomic.Int32
}

type changingOriginalFactory struct {
	repo      *originalSyncRepository
	connector Connector
}

func (f changingOriginalFactory) Connector(string) (Connector, error) {
	f.repo.connection.ProviderType = domain.ProviderFeishu
	return f.connector, nil
}

type failedOriginalProjector struct{}

func (failedOriginalProjector) Apply(context.Context, domain.Connection, domain.Policy, domain.Plan) error {
	return errors.New("private-projector-credential")
}

func TestOriginalDirectorySyncConfigurationFenceAndSanitizedProjectionFailure(t *testing.T) {
	for _, scenario := range []string{"configuration-changed", "projection-failed"} {
		t.Run(scenario, func(t *testing.T) {
			repo := newOriginalSyncRepository()
			connector := &connectorSpy{}
			var factory ConnectorFactory = &originalSyncFactory{connector: connector}
			var projector LocalProjector
			if scenario == "configuration-changed" {
				factory = changingOriginalFactory{repo, connector}
			} else {
				projector = failedOriginalProjector{}
			}
			run, err := New(repo, projector).SyncOriginal(t.Context(), "owned", "actor", "session:original", originalSyncKey, factory)
			if err != nil || run.Status != domain.RunFailed || run.ErrorCode != "original_outcome_unconfirmed" || repo.projections.Load() != 0 || run.ErrorSummary == "private-projector-credential" {
				t.Fatal("configuration or sanitized failure boundary lost", err)
			}
		})
	}
}

func (f *originalSyncFactory) Connector(string) (Connector, error) {
	f.calls.Add(1)
	return f.connector, nil
}

type blockingOriginalConnector struct {
	*connectorSpy
	entered, release chan struct{}
	err              error
}

func (c *blockingOriginalConnector) ListOrganizations(ctx context.Context, _ domain.Connection) ([]domain.Organization, string, error) {
	close(c.entered)
	select {
	case <-c.release:
	case <-ctx.Done():
		return nil, "", ctx.Err()
	}
	return nil, "", c.err
}

type originalSyncConcurrentOutcome struct {
	run domain.Run
	err error
}

func TestOriginalDirectorySyncConcurrentDuplicatesOnlyPullOnce(t *testing.T) {
	repo := newOriginalSyncRepository()
	connector := &blockingOriginalConnector{connectorSpy: &connectorSpy{}, entered: make(chan struct{}), release: make(chan struct{})}
	factory := &originalSyncFactory{connector: connector}
	service := New(repo, nil)
	result := make(chan originalSyncConcurrentOutcome, 1)
	go func() { originalDirectorySyncConcurrentDuplicatesOnlyPullOncePhase1(&t, &factory, &service, &result) }()
	select {
	case <-connector.entered:
	case <-t.Context().Done():
		t.Fatal("creator never reached connector")
	}
	var wait sync.WaitGroup
	for range 32 {
		wait.Go(func() { originalDirectorySyncConcurrentDuplicatesOnlyPullOncePhase2(&t, &factory, &service) })
	}
	wait.Wait()
	for _, operation := range []string{"get", "start"} {
		var err error
		if operation == "get" {
			_, err = service.GetOriginalSync(t.Context(), "owned", "actor", "token:another", originalSyncKey)
		} else {
			_, err = service.SyncOriginal(t.Context(), "owned", "actor", "token:another", originalSyncKey, factory)
		}
		if !errors.Is(err, apperrors.ErrAccessDenied) {
			t.Fatalf("different credential reached original: %v", err)
		}
	}
	if _, err := service.GetOriginalSync(t.Context(), "owned", "other", "session:original", originalSyncKey); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatal("actor namespace was not isolated", err)
	}
	if factory.calls.Load() != 1 || repo.projections.Load() != 0 {
		t.Fatal("duplicate pulled or projected")
	}
	close(connector.release)
	completed := <-result
	if completed.err != nil || completed.run.Status != domain.RunSucceeded || completed.run.FinishedAt == nil {
		t.Fatal("creator did not persist completion", completed.err)
	}
	assertTerminalDirectoryReplay(t, repo, factory, connector, completed)
}

func TestOriginalDirectorySyncOccupiedStatesNeverReplay(t *testing.T) {
	for _, status := range []string{domain.RunQueued, domain.RunRunning, domain.RunSucceeded, domain.RunFailed, domain.RunPartial, domain.RunCanceled} {
		t.Run(status, func(t *testing.T) {
			repo := newOriginalSyncRepository()
			stored, binding, err := originalSyncReference("actor", "token:original", originalSyncKey)
			if err != nil {
				t.Fatal(err)
			}
			repo.runs["original"] = domain.Run{ID: "original", ConnectionID: "owned", Status: status, RequestedBy: "actor", RequestCredentialID: binding, IdempotencyKey: stored}
			factory := &originalSyncFactory{connector: &connectorSpy{}}
			run, err := New(repo, nil).SyncOriginal(t.Context(), "owned", "actor", "token:original", originalSyncKey, factory)
			if err != nil || run.ID != "original" || run.Status != status || factory.calls.Load() != 0 || repo.projections.Load() != 0 {
				t.Fatal("occupied original was executed", err)
			}
		})
	}
}

func TestOriginalDirectorySyncFailureAndCancellationOccupyKey(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		t.Run(map[bool]string{false: "provider-failed", true: "canceled"}[canceled], func(t *testing.T) {
			repo := newOriginalSyncRepository()
			connector := &blockingOriginalConnector{connectorSpy: &connectorSpy{}, entered: make(chan struct{}), release: make(chan struct{}), err: errors.New("private-provider-detail")}
			factory := &originalSyncFactory{connector: connector}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if canceled {
				cancel()
			} else {
				close(connector.release)
			}
			run, err := New(repo, nil).SyncOriginal(ctx, "owned", "actor", "token:original", originalSyncKey, factory)
			if err != nil || run.Status != domain.RunFailed || run.ErrorCode != "original_outcome_unconfirmed" || run.FinishedAt == nil {
				t.Fatal("failed original not durably visible", err)
			}
			if run.ErrorSummary == connector.err.Error() {
				t.Fatal("provider error leaked")
			}
			again, err := New(repo, nil).SyncOriginal(t.Context(), "owned", "actor", "token:original", originalSyncKey, factory)
			if err != nil || again.ID != run.ID || factory.calls.Load() != 1 || repo.projections.Load() != 0 {
				t.Fatal("failed original replayed", err)
			}
		})
	}
}

func TestOriginalDirectorySyncRejectsInvalidReferenceBeforeConnector(t *testing.T) {
	for _, key := range []string{"", "unsafe/path", "00000000-0000-0000-0000-000000000000", "d154b4d6a26f43a6a150d92af32f3d19"} {
		factory := &originalSyncFactory{connector: &connectorSpy{}}
		_, err := New(newOriginalSyncRepository(), nil).SyncOriginal(t.Context(), "owned", "actor", "token:original", key, factory)
		if !errors.Is(err, apperrors.ErrInvalidArgument) || factory.calls.Load() != 0 {
			t.Fatal("invalid key reached provider", err)
		}
	}
	var absent *originalSyncFactory
	run, err := New(newOriginalSyncRepository(), nil).SyncOriginal(t.Context(), "owned", "actor", "token:original", originalSyncKey, absent)
	if err != nil || run.Status != domain.RunFailed {
		t.Fatal("typed nil factory not durably rejected", err)
	}
}

func originalDirectorySyncConcurrentDuplicatesOnlyPullOncePhase1(sharedT **testing.T, sharedFactory **originalSyncFactory, sharedService **Service, sharedResult *chan originalSyncConcurrentOutcome) {

	run, err := (*sharedService).SyncOriginal((*sharedT).Context(), "owned", "actor", "session:original", originalSyncKey, (*sharedFactory))
	(*sharedResult) <- originalSyncConcurrentOutcome{run, err}
}

func originalDirectorySyncConcurrentDuplicatesOnlyPullOncePhase2(sharedT **testing.T, sharedFactory **originalSyncFactory, sharedService **Service) {

	run, err := (*sharedService).SyncOriginal((*sharedT).Context(), "owned", "actor", "session:original", originalSyncKey, (*sharedFactory))
	if err != nil || run.Status != domain.RunRunning {
		(*sharedT).Errorf("duplicate did not observe running original: %s %v", run.Status, err)
	}
}

func assertTerminalDirectoryReplay(t *testing.T, repo *originalSyncRepository, factory *originalSyncFactory, connector *blockingOriginalConnector, completed originalSyncConcurrentOutcome) {
	t.Helper()
	// A newly constructed service simulates losing all request-local state.
	for range 3 {
		run, err := New(repo, nil).SyncOriginal(t.Context(), "owned", "actor", "session:original", originalSyncKey, factory)
		if err != nil || run.ID != completed.run.ID || run.Status != domain.RunSucceeded {
			t.Fatal("terminal original changed", err)
		}
	}
	if factory.calls.Load() != 1 || repo.projections.Load() != 1 || connector.peopleCalls != 0 || connector.membershipCalls != 0 {
		t.Fatal("replay or people-gate bypass")
	}
}
