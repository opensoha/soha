package directorysync

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	domain "github.com/opensoha/soha/internal/domain/directorysync"
	"github.com/opensoha/soha/internal/platform/runtimeobs"
)

type ConnectorResolver func(context.Context, domain.Connection) (Connector, error)

type Scheduler struct {
	repository domain.Repository
	service    *Service
	resolve    ConnectorResolver
	interval   time.Duration
	mu         sync.Mutex
	lastFired  map[string]string
	metrics    *runtimeobs.Registry
}

func (s *Scheduler) SetInstrumentation(metrics *runtimeobs.Registry) { s.metrics = metrics }

func NewScheduler(repository domain.Repository, service *Service, resolve ConnectorResolver) *Scheduler {
	return &Scheduler{repository: repository, service: service, resolve: resolve, interval: 30 * time.Second, lastFired: map[string]string{}}
}

func (s *Scheduler) Start(ctx context.Context) {
	if s == nil || s.repository == nil || s.service == nil || s.resolve == nil {
		return
	}
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	s.tick(ctx, time.Now())
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			s.tick(ctx, now)
		}
	}
}

func (s *Scheduler) tick(ctx context.Context, now time.Time) {
	// Recover leases left behind by a terminated worker before claiming new work.
	_, _ = s.repository.RecoverStaleEvents(ctx, now.Add(-5*time.Minute), now.UTC())
	_, _ = s.repository.RecoverStaleRuns(ctx, now.Add(-10*time.Minute), now.UTC())
	s.processEvents(ctx, now)
	connections, err := s.repository.ListConnections(ctx)
	if err != nil {
		return
	}
	minute := now.Format("200601021504")
	for _, connection := range connections {
		if !connection.Enabled {
			continue
		}
		_, policy, err := s.repository.GetConnection(ctx, connection.ID)
		if err != nil || policy.Mode == domain.PolicyManual || !cronMatches(policy.Schedule, now) {
			continue
		}
		s.mu.Lock()
		key := connection.ID + ":" + minute
		if s.lastFired[key] != "" {
			s.mu.Unlock()
			continue
		}
		s.lastFired[key] = minute
		for previous := range s.lastFired {
			if !strings.HasSuffix(previous, ":"+minute) {
				delete(s.lastFired, previous)
			}
		}
		s.mu.Unlock()
		go s.run(ctx, connection)
	}
}

func (s *Scheduler) processEvents(ctx context.Context, now time.Time) {
	events, err := s.repository.ClaimEvents(ctx, 20)
	if err != nil {
		return
	}
	for _, event := range events {
		started := time.Now()
		if s.metrics != nil {
			s.metrics.RecordStart(runtimeobs.ComponentDirectorySync, event.ID, len(events), 1)
		}
		connection, policy, err := s.repository.GetConnection(ctx, event.ConnectionID)
		if err == nil && policy.Mode != domain.PolicyScheduledAndRealtime {
			err = fmt.Errorf("realtime sync is disabled for connection")
		}
		var connector Connector
		if err == nil {
			connector, err = s.resolve(ctx, connection)
		}
		var snapshot domain.Snapshot
		if err == nil {
			snapshot, _, err = s.service.PullSnapshot(ctx, connection.ID, connector)
		}
		if err == nil {
			_, _, err = s.service.ApplyTriggered(ctx, connection.ID, snapshot, "directory-webhook", "webhook")
		}
		status, summary := "succeeded", ""
		if err != nil {
			status, summary = "failed", err.Error()
		}
		_ = s.repository.CompleteEvent(ctx, event.ID, status, summary, now.UTC())
		if s.metrics != nil {
			outcome := runtimeobs.OutcomeSucceeded
			if err != nil {
				outcome = runtimeobs.OutcomeFailed
			}
			s.metrics.RecordFinish(runtimeobs.ComponentDirectorySync, event.ID, time.Since(started), 0, 1, outcome, err)
		}
	}
}

func (s *Scheduler) run(ctx context.Context, connection domain.Connection) {
	started := time.Now()
	operationID := "schedule:" + connection.ID + ":" + started.UTC().Format(time.RFC3339Nano)
	if s.metrics != nil {
		s.metrics.RecordStart(runtimeobs.ComponentDirectorySync, operationID, 0, 1)
	}
	connector, err := s.resolve(ctx, connection)
	if err != nil {
		if s.metrics != nil {
			s.metrics.RecordFinish(runtimeobs.ComponentDirectorySync, operationID, time.Since(started), 0, 1, runtimeobs.OutcomeFailed, err)
		}
		return
	}
	snapshot, _, err := s.service.PullSnapshot(ctx, connection.ID, connector)
	if err != nil {
		if s.metrics != nil {
			s.metrics.RecordFinish(runtimeobs.ComponentDirectorySync, operationID, time.Since(started), 0, 1, runtimeobs.OutcomeFailed, err)
		}
		return
	}
	_, _, err = s.service.ApplyTriggered(ctx, connection.ID, snapshot, "directory-scheduler", "schedule")
	if s.metrics != nil {
		outcome := runtimeobs.OutcomeSucceeded
		if err != nil {
			outcome = runtimeobs.OutcomeFailed
		}
		s.metrics.RecordFinish(runtimeobs.ComponentDirectorySync, operationID, time.Since(started), 0, 1, outcome, err)
	}
}

func cronMatches(expression string, at time.Time) bool {
	fields := strings.Fields(expression)
	if len(fields) != 5 {
		return false
	}
	values := []int{at.Minute(), at.Hour(), at.Day(), int(at.Month()), int(at.Weekday())}
	limits := [][2]int{{0, 59}, {0, 23}, {1, 31}, {1, 12}, {0, 6}}
	for i := range fields {
		if !cronFieldMatches(fields[i], values[i], limits[i][0], limits[i][1]) {
			return false
		}
	}
	return true
}

func cronFieldMatches(field string, value, min, max int) bool {
	for _, part := range strings.Split(field, ",") {
		part = strings.TrimSpace(part)
		if part == "*" {
			return true
		}
		if strings.HasPrefix(part, "*/") {
			step, err := strconv.Atoi(strings.TrimPrefix(part, "*/"))
			if err == nil && step > 0 && value%step == 0 {
				return true
			}
			continue
		}
		n, err := strconv.Atoi(part)
		if err == nil && n >= min && n <= max && n == value {
			return true
		}
	}
	return false
}

func ValidateSchedule(expression string) error {
	fields := strings.Fields(expression)
	if len(fields) != 5 {
		return fmt.Errorf("schedule must be a five-field cron expression")
	}
	limits := [][2]int{{0, 59}, {0, 23}, {1, 31}, {1, 12}, {0, 6}}
	for i, field := range fields {
		if field == "*" {
			continue
		}
		if strings.HasPrefix(field, "*/") {
			step, err := strconv.Atoi(strings.TrimPrefix(field, "*/"))
			if err != nil || step < 1 {
				return fmt.Errorf("invalid cron field %q", field)
			}
			continue
		}
		valid := true
		for _, part := range strings.Split(field, ",") {
			n, err := strconv.Atoi(part)
			if err != nil || n < limits[i][0] || n > limits[i][1] {
				valid = false
			}
		}
		if !valid {
			return fmt.Errorf("invalid cron field %q", field)
		}
	}
	return nil
}
