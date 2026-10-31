package directorysync

import (
	"context"
	"errors"
	"testing"
	"time"

	domain "github.com/opensoha/soha/internal/domain/directorysync"
)

type durableRunRepository struct {
	*repositoryStub
	run               domain.Run
	started, finished time.Time
	readErr           error
}

func (r *durableRunRepository) CreateRun(_ context.Context, run domain.Run) (domain.Run, error) {
	r.run = run
	return run, nil
}

func (r *durableRunRepository) TransitionRun(_ context.Context, _ string, status string, stats domain.RunStats, _, _ string) error {
	r.run.Status, r.run.Stats = status, stats
	if status == domain.RunRunning {
		r.run.StartedAt = &r.started
	} else {
		r.run.FinishedAt = &r.finished
	}
	return nil
}

func (r *durableRunRepository) GetRun(context.Context, string) (domain.Run, error) {
	if r.readErr != nil {
		return domain.Run{}, r.readErr
	}
	return r.run, nil
}

func TestDirectorySyncReturnsDurableCompletionRatherThanQueuedCopy(t *testing.T) {
	for _, mode := range []string{"full", "incremental"} {
		t.Run(mode, func(t *testing.T) {
			repo := &durableRunRepository{repositoryStub: &repositoryStub{connection: domain.Connection{ID: "owned"}, policy: domain.DefaultPolicy("owned")},
				started: time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC), finished: time.Date(2026, 1, 1, 1, 1, 0, 0, time.UTC)}
			service := New(repo, &deltaProjectorStub{})
			var run domain.Run
			var err error
			organization := domain.Organization{ExternalID: "org", Name: "Owned", Status: domain.ProjectionActive}
			if mode == "full" {
				run, _, err = service.Apply(t.Context(), "owned", domain.Snapshot{Organizations: []domain.Organization{organization}}, "actor")
			} else {
				run, err = service.ApplyDelta(t.Context(), "owned", domain.Delta{Action: domain.ChangeCreate, Organization: &organization}, "actor")
			}
			if err != nil || run.ID != repo.run.ID || run.Status != domain.RunSucceeded || run.StartedAt == nil || run.FinishedAt == nil || !run.StartedAt.Equal(repo.started) || !run.FinishedAt.Equal(repo.finished) || run.Stats.OrganizationsCreated != 1 {
				t.Fatalf("durable completion projection missing: %#v error=%v", run, err)
			}
		})
	}
}

func TestDirectorySyncDoesNotFabricateCompletionWhenReadbackFails(t *testing.T) {
	failure := errors.New("durable read unavailable")
	repo := &durableRunRepository{repositoryStub: &repositoryStub{connection: domain.Connection{ID: "owned"}, policy: domain.DefaultPolicy("owned")}, readErr: failure}
	run, _, err := New(repo, nil).Apply(t.Context(), "owned", domain.Snapshot{}, "actor")
	if !errors.Is(err, failure) || run.Status == domain.RunSucceeded || repo.run.Status != domain.RunSucceeded {
		t.Fatal("lost durable readback was represented as confirmed completion", err)
	}
}
