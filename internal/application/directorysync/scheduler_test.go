package directorysync

import (
	"context"
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

func TestSchedulerTickRecoversStaleLeasesBeforeWork(t *testing.T) {
	repository := &repositoryStub{}
	scheduler := NewScheduler(repository, New(repository, nil), func(context.Context, domain.Connection) (Connector, error) { return nil, nil })
	scheduler.tick(context.Background(), time.Now().UTC())
	if repository.recoveredEvents != 1 || repository.recoveredRuns != 1 {
		t.Fatalf("recovery calls events=%d runs=%d", repository.recoveredEvents, repository.recoveredRuns)
	}
}
