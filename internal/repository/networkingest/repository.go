package networkingest

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	domainnetworkingest "github.com/opensoha/soha/internal/domain/networkingest"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Repository struct{ db *gorm.DB }

func New(db *gorm.DB) *Repository { return &Repository{db: db} }

func (r *Repository) Append(ctx context.Context, events []domainnetworkingest.Event) (domainnetworkingest.Result, error) {
	if len(events) == 0 {
		return domainnetworkingest.Result{}, nil
	}
	producerID, producerKind := events[0].ProducerID, events[0].ProducerKind
	for _, event := range events[1:] {
		if event.ProducerID != producerID || event.ProducerKind != producerKind {
			return domainnetworkingest.Result{}, fmt.Errorf("one ingest append may contain only one producer")
		}
	}

	result := domainnetworkingest.Result{}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// ponytail: serialize one producer's batches with a PostgreSQL advisory lock;
		// shard only if a single producer becomes a measured throughput bottleneck.
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext(?))`, producerID).Error; err != nil {
			return err
		}
		ids := make([]string, 0, len(events))
		incoming := make(map[string]domainnetworkingest.Event, len(events))
		for _, event := range events {
			ids = append(ids, event.EventID)
			incoming[event.EventID] = event
		}
		var existing []eventRow
		if err := tx.Select("event_id", "event_hash").Where("producer_id = ? AND event_id IN ?", producerID, ids).Find(&existing).Error; err != nil {
			return err
		}
		for _, row := range existing {
			if incoming[row.EventID].EventHash != row.EventHash {
				return apperrors.NewBusiness(apperrors.ErrConflict, "ingest_event_conflict", "An event ID was already stored with different content.", "同一事件 ID 已保存了不同内容。")
			}
			delete(incoming, row.EventID)
		}
		rows := make([]eventRow, 0, len(incoming))
		newEvents := make([]domainnetworkingest.Event, 0, len(incoming))
		for _, event := range events {
			if _, exists := incoming[event.EventID]; !exists {
				continue
			}
			rows = append(rows, newEventRow(event))
			newEvents = append(newEvents, event)
		}
		if len(rows) > 0 {
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(rows, 250).Error; err != nil {
				return err
			}
		}
		if err := updateProducerState(tx, producerID, producerKind, events, newEvents); err != nil {
			return err
		}
		result.Accepted = len(rows)
		result.Duplicate = len(existing)
		return nil
	})
	return result, err
}

func (r *Repository) DeleteBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	result := r.db.WithContext(ctx).Where("received_at < ?", cutoff).Delete(&eventRow{})
	return result.RowsAffected, result.Error
}

func (r *Repository) Summary(ctx context.Context, filter domainnetworkingest.SummaryFilter) (domainnetworkingest.Summary, error) {
	// ponytail: bounded live aggregates avoid a rollup table; add one only after measured query latency warrants it.
	summary := domainnetworkingest.Summary{From: filter.From, To: filter.To, Producers: []domainnetworkingest.ProducerSummary{}, ProxyFlows: []domainnetworkingest.ProxyFlowSummary{}}
	counts := struct {
		EventCount             int64
		HeartbeatCount         int64
		RadiusAccountingCount  int64
		NetworkFlowCount       int64
		ConnectionSummaryCount int64
		ProxyFlowCount         int64
		UploadBytes            int64
		DownloadBytes          int64
		ActiveConnections      int64
	}{}
	if err := r.db.WithContext(ctx).Raw(`
WITH scoped AS (
    SELECT producer_id, event_type, occurred_at, sequence, payload
    FROM public.network_ingest_events
    WHERE occurred_at >= ? AND occurred_at < ? AND (? = '' OR producer_id = ?)
      AND event_type NOT LIKE 'vpn.%'
), latest_proxy AS (
    SELECT DISTINCT ON (producer_id)
        (payload ->> 'activeConnections')::bigint AS active_connections
    FROM scoped
    WHERE event_type = 'proxy.flow.aggregate'
    ORDER BY producer_id, occurred_at DESC, sequence DESC
)
SELECT
    count(*)::bigint AS event_count,
    count(*) FILTER (WHERE event_type = 'runtime.heartbeat')::bigint AS heartbeat_count,
    count(*) FILTER (WHERE event_type = 'radius.accounting')::bigint AS radius_accounting_count,
    count(*) FILTER (WHERE event_type = 'network.flow.aggregate')::bigint AS network_flow_count,
    count(*) FILTER (WHERE event_type = 'network.connection.summary')::bigint AS connection_summary_count,
    count(*) FILTER (WHERE event_type = 'proxy.flow.aggregate')::bigint AS proxy_flow_count,
    coalesce(sum(CASE WHEN event_type = 'proxy.flow.aggregate' THEN (payload ->> 'uploadBytes')::bigint ELSE 0 END), 0)::bigint AS upload_bytes,
    coalesce(sum(CASE WHEN event_type = 'proxy.flow.aggregate' THEN (payload ->> 'downloadBytes')::bigint ELSE 0 END), 0)::bigint AS download_bytes,
    (SELECT coalesce(sum(active_connections), 0)::bigint FROM latest_proxy) AS active_connections
FROM scoped`, filter.From, filter.To, filter.ProducerID, filter.ProducerID).Scan(&counts).Error; err != nil {
		return domainnetworkingest.Summary{}, err
	}
	summary.EventCount = counts.EventCount
	summary.HeartbeatCount = counts.HeartbeatCount
	summary.RadiusAccountingCount = counts.RadiusAccountingCount
	summary.NetworkFlowCount = counts.NetworkFlowCount
	summary.ConnectionSummaryCount = counts.ConnectionSummaryCount
	summary.ProxyFlowCount = counts.ProxyFlowCount
	summary.UploadBytes = counts.UploadBytes
	summary.DownloadBytes = counts.DownloadBytes
	summary.ActiveConnections = counts.ActiveConnections
	if err := r.db.WithContext(ctx).Raw(`
SELECT state.producer_id, state.producer_kind, state.last_seen_at, state.gap_count, state.regression_count
FROM public.network_ingest_producer_state state
WHERE (? = '' OR state.producer_id = ?)
  AND EXISTS (
      SELECT 1 FROM public.network_ingest_events event
      WHERE event.producer_id = state.producer_id AND event.occurred_at >= ? AND event.occurred_at < ?
  )
ORDER BY state.last_seen_at DESC, state.producer_id
LIMIT ?`, filter.ProducerID, filter.ProducerID, filter.From, filter.To, filter.Limit).Scan(&summary.Producers).Error; err != nil {
		return domainnetworkingest.Summary{}, err
	}
	if err := r.db.WithContext(ctx).Raw(`
SELECT
    producer_id,
    payload ->> 'engine' AS engine,
    payload ->> 'profileId' AS profile_id,
    (payload ->> 'profileRevision')::integer AS profile_revision,
    payload ->> 'mode' AS mode,
    payload ->> 'selectedProxy' AS selected_proxy,
    sum((payload ->> 'uploadBytes')::bigint)::bigint AS upload_bytes,
    sum((payload ->> 'downloadBytes')::bigint)::bigint AS download_bytes,
    (array_agg((payload ->> 'activeConnections')::bigint ORDER BY occurred_at DESC, sequence DESC))[1] AS active_connections,
    max(occurred_at) AS last_occurred_at
FROM public.network_ingest_events
WHERE event_type = 'proxy.flow.aggregate' AND occurred_at >= ? AND occurred_at < ? AND (? = '' OR producer_id = ?)
GROUP BY producer_id, payload ->> 'engine', payload ->> 'profileId', (payload ->> 'profileRevision')::integer, payload ->> 'mode', payload ->> 'selectedProxy'
ORDER BY last_occurred_at DESC, producer_id, profile_id
LIMIT ?`, filter.From, filter.To, filter.ProducerID, filter.ProducerID, filter.Limit).Scan(&summary.ProxyFlows).Error; err != nil {
		return domainnetworkingest.Summary{}, err
	}
	return summary, nil
}

type jsonDocument []byte

func (value jsonDocument) Value() (driver.Value, error) {
	if !json.Valid(value) {
		return nil, fmt.Errorf("invalid JSON document")
	}
	return string(value), nil
}

func (value *jsonDocument) Scan(raw any) error {
	switch typed := raw.(type) {
	case []byte:
		*value = append((*value)[:0], typed...)
	case string:
		*value = append((*value)[:0], typed...)
	default:
		return fmt.Errorf("scan JSON document from %T", raw)
	}
	return nil
}

type eventRow struct {
	ProducerID   string       `gorm:"primaryKey;column:producer_id"`
	EventID      string       `gorm:"primaryKey;column:event_id"`
	EventHash    string       `gorm:"column:event_hash"`
	BatchID      string       `gorm:"column:batch_id"`
	ProducerKind string       `gorm:"column:producer_kind"`
	EventType    string       `gorm:"column:event_type"`
	Sequence     int64        `gorm:"column:sequence"`
	OccurredAt   time.Time    `gorm:"column:occurred_at"`
	ReceivedAt   time.Time    `gorm:"column:received_at"`
	Payload      jsonDocument `gorm:"column:payload;type:jsonb"`
}

func (eventRow) TableName() string { return "network_ingest_events" }

func newEventRow(event domainnetworkingest.Event) eventRow {
	return eventRow{
		ProducerID: event.ProducerID, EventID: event.EventID, EventHash: event.EventHash,
		BatchID: event.BatchID, ProducerKind: event.ProducerKind, EventType: event.EventType,
		Sequence: event.Sequence, OccurredAt: event.OccurredAt, ReceivedAt: event.ReceivedAt,
		Payload: jsonDocument(event.Payload),
	}
}

type producerState struct {
	ProducerID      string    `gorm:"primaryKey;column:producer_id"`
	ProducerKind    string    `gorm:"column:producer_kind"`
	LastSequence    int64     `gorm:"column:last_sequence"`
	GapCount        int64     `gorm:"column:gap_count"`
	RegressionCount int64     `gorm:"column:regression_count"`
	LastSeenAt      time.Time `gorm:"column:last_seen_at"`
	UpdatedAt       time.Time `gorm:"column:updated_at"`
}

func (producerState) TableName() string { return "network_ingest_producer_state" }

func updateProducerState(tx *gorm.DB, producerID, producerKind string, allEvents, newEvents []domainnetworkingest.Event) error {
	var state producerState
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("producer_id = ?", producerID).First(&state).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return err
	}
	lastSeen := allEvents[0].ReceivedAt
	if err == gorm.ErrRecordNotFound {
		seed := allEvents[0]
		if len(newEvents) > 0 {
			seed = newEvents[0]
			newEvents = newEvents[1:]
		}
		state = producerState{ProducerID: producerID, ProducerKind: producerKind, LastSequence: seed.Sequence, LastSeenAt: lastSeen, UpdatedAt: lastSeen}
	} else if state.ProducerKind != producerKind {
		return apperrors.NewBusiness(apperrors.ErrConflict, "producer_kind_conflict", "The producer kind changed for an existing producer ID.", "现有生产者 ID 的类型发生冲突。")
	}
	for _, event := range newEvents {
		switch {
		case producerKind != "freeradius" && event.Sequence > state.LastSequence+1:
			state.GapCount += event.Sequence - state.LastSequence - 1
			state.LastSequence = event.Sequence
		case event.Sequence > state.LastSequence:
			state.LastSequence = event.Sequence
		default:
			state.RegressionCount++
		}
		if event.ReceivedAt.After(lastSeen) {
			lastSeen = event.ReceivedAt
		}
	}
	state.LastSeenAt, state.UpdatedAt = lastSeen, lastSeen
	return tx.Save(&state).Error
}
