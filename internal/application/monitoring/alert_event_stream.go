package monitoring

import (
	"context"
	"fmt"
	"strings"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainalert "github.com/opensoha/soha/internal/domain/alert"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const alertEventStreamBatchSize = 200

func (s *Service) SubscribeEventSignals(
	ctx context.Context,
	principal domainidentity.Principal,
	clusterID string,
) (<-chan domainalert.AlertEventStreamSignal, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveAlertsView); err != nil {
		return nil, err
	}
	if s.alertEvents == nil {
		return nil, fmt.Errorf("%w: alert event repository is not configured", apperrors.ErrClusterUnready)
	}
	observedAt := time.Now().UTC()
	signals := make(chan domainalert.AlertEventStreamSignal, 64)
	signals <- domainalert.AlertEventStreamSignal{
		Type: "reset", ObservedAt: observedAt, ClusterID: strings.TrimSpace(clusterID), ResyncRequired: true,
	}
	interval := s.alertEventStreamInterval
	if interval <= 0 {
		interval = 2 * time.Second
	}
	go s.pollAlertEventSignals(ctx, signals, strings.TrimSpace(clusterID), observedAt, interval)
	return signals, nil
}

func (s *Service) pollAlertEventSignals(
	ctx context.Context,
	signals chan<- domainalert.AlertEventStreamSignal,
	clusterID string,
	cursorAt time.Time,
	interval time.Duration,
) {
	defer close(signals)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	cursorID := ""
	// ponytail: bounded DB polling is multi-replica safe; replace with LISTEN/NOTIFY if subscriber load becomes material.
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			items, err := s.alertEvents.ListEvents(ctx, domainalert.AlertEventFilter{
				ClusterID: clusterID, Limit: alertEventStreamBatchSize,
				UpdatedAfter: cursorAt, AfterID: cursorID, Ascending: true,
			})
			if err != nil {
				if !sendAlertEventSignal(ctx, signals, domainalert.AlertEventStreamSignal{
					Type: "error", ObservedAt: time.Now().UTC(), ClusterID: clusterID,
					Message: "alert event stream temporarily unavailable", ResyncRequired: true,
				}) {
					return
				}
				continue
			}
			for _, item := range items {
				if !sendAlertEventSignal(ctx, signals, alertEventChangedSignal(item)) {
					return
				}
				cursorAt = item.UpdatedAt
				cursorID = item.ID
			}
		}
	}
}

func alertEventChangedSignal(item domainalert.AlertEvent) domainalert.AlertEventStreamSignal {
	observedAt := item.UpdatedAt
	if observedAt.IsZero() {
		observedAt = item.LastSeenAt
	}
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}
	return domainalert.AlertEventStreamSignal{
		Type: "changed", ObservedAt: observedAt, ClusterID: item.ClusterID, Namespace: item.Namespace,
		EventID: item.ID, EventStatus: item.Status,
	}
}

func sendAlertEventSignal(
	ctx context.Context,
	signals chan<- domainalert.AlertEventStreamSignal,
	signal domainalert.AlertEventStreamSignal,
) bool {
	select {
	case signals <- signal:
		return true
	case <-ctx.Done():
		return false
	}
}
