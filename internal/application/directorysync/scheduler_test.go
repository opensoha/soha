package directorysync

import (
	"context"
	"fmt"
	"testing"
	"time"

	domain "github.com/opensoha/soha/internal/domain/directorysync"
)

func TestCronMatches(t *testing.T) {
	at := time.Date(2026, 7, 12, 14, 30, 0, 0, time.Local)
	for _, expression := range []string{"30 14 * * *", "*/15 * * * *", "0,30 14 * * 0"} {
		if !cronMatches(expression, at) {
			t.Fatalf("cronMatches(%q) = false", expression)
		}
	}
	if cronMatches("0 1 * * *", at) || cronMatches("invalid", at) {
		t.Fatal("unexpected cron match")
	}
}

func TestSchedulerProcessEventsMarksUnknownEventForFullReconcile(t *testing.T) {
	policy := domain.DefaultPolicy("c1")
	policy.Mode = domain.PolicyScheduledAndRealtime
	repository := &repositoryStub{connection: domain.Connection{ID: "c1"}, policy: policy, events: []domain.EventEnvelope{{ID: "e1", ConnectionID: "c1", EventType: "unknown"}}}
	connector := &connectorSpy{deltaErr: fmt.Errorf("%w: unknown event", domain.ErrReconcileRequired)}
	scheduler := NewScheduler(repository, New(repository, &deltaProjectorStub{}), func(context.Context, domain.Connection) (Connector, error) { return connector, nil })
	scheduler.processEvents(context.Background(), time.Now().UTC())
	if repository.completedStatus != "failed" || repository.reconcileReason == "" || connector.organizationCalls != 0 {
		t.Fatalf("event=%q reconcile=%q organizationCalls=%d", repository.completedStatus, repository.reconcileReason, connector.organizationCalls)
	}
}

func TestSchedulerTickRecoversStaleLeasesBeforeWork(t *testing.T) {
	repository := &repositoryStub{}
	scheduler := NewScheduler(repository, New(repository, nil), func(context.Context, domain.Connection) (Connector, error) { return nil, nil })
	scheduler.tick(context.Background(), time.Now().UTC())
	if repository.recoveredEvents != 1 || repository.recoveredRuns != 1 {
		t.Fatalf("recovery calls events=%d runs=%d", repository.recoveredEvents, repository.recoveredRuns)
	}
}

func TestSchedulerProcessEventsAppliesDeltaWithoutPullingSnapshot(t *testing.T) {
	policy := domain.DefaultPolicy("c1")
	policy.Mode = domain.PolicyScheduledAndRealtime
	repository := &repositoryStub{connection: domain.Connection{ID: "c1"}, policy: policy, events: []domain.EventEnvelope{{ID: "e1", ConnectionID: "c1", EventType: "contact.department.updated_v3"}}}
	connector := &connectorSpy{}
	projector := &deltaProjectorStub{}
	scheduler := NewScheduler(repository, New(repository, projector), func(context.Context, domain.Connection) (Connector, error) { return connector, nil })
	scheduler.processEvents(context.Background(), time.Now().UTC())
	if connector.deltaCalls != 1 || connector.organizationCalls != 0 || connector.peopleCalls != 0 || connector.membershipCalls != 0 {
		t.Fatalf("connector calls delta=%d organizations=%d people=%d memberships=%d", connector.deltaCalls, connector.organizationCalls, connector.peopleCalls, connector.membershipCalls)
	}
	if projector.calls != 1 || repository.completedStatus != "succeeded" || repository.incrementalAt == nil {
		t.Fatalf("projector=%d event=%q incrementalAt=%v", projector.calls, repository.completedStatus, repository.incrementalAt)
	}
}
