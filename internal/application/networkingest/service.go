package networkingest

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	domainnetworkingest "github.com/opensoha/soha/internal/domain/networkingest"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type Store interface {
	Append(context.Context, []domainnetworkingest.Event) (domainnetworkingest.Result, error)
	Summary(context.Context, domainnetworkingest.SummaryFilter) (domainnetworkingest.Summary, error)
}

type Options struct {
	MaxEventsPerBatch int
	MaxClockSkew      time.Duration
	Retention         time.Duration
}

type Service struct {
	store   Store
	schemas *networkprotocol.Schemas
	options Options
	now     func() time.Time
}

func New(store Store, schemas *networkprotocol.Schemas, options Options) (*Service, error) {
	if store == nil || schemas == nil {
		return nil, fmt.Errorf("network ingest store and schemas are required")
	}
	if options.MaxEventsPerBatch < 1 || options.MaxEventsPerBatch > 1000 || options.MaxClockSkew <= 0 || options.Retention <= 0 {
		return nil, fmt.Errorf("network ingest options are invalid")
	}
	return &Service{store: store, schemas: schemas, options: options, now: time.Now}, nil
}

func (s *Service) Ingest(ctx context.Context, identity networkidentity.Identity, raw []byte) (domainnetworkingest.Acknowledgement, error) {
	if err := s.schemas.ValidateIngest(raw); err != nil {
		return domainnetworkingest.Acknowledgement{}, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_ingest_batch", "The ingest batch does not match the network-ingest contract.", "遥测批次不符合 network-ingest 契约。")
	}
	batch, err := networkprotocol.DecodeIngestBatch(raw)
	if err != nil {
		return domainnetworkingest.Acknowledgement{}, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_ingest_batch", "The ingest batch is invalid.", "遥测批次无效。")
	}
	if identity.Scope != networkidentity.ScopeIngest || identity.ID != batch.ProducerID || identity.Kind != batch.ProducerKind {
		return domainnetworkingest.Acknowledgement{}, apperrors.NewBusiness(apperrors.ErrUnauthorized, "producer_identity_mismatch", "The authenticated producer does not match the batch.", "已认证的生产者与遥测批次不匹配。")
	}
	now := s.now().UTC()
	if err := networkprotocol.ValidateIngestBatch(batch, now, s.options.MaxClockSkew, s.options.Retention, s.options.MaxEventsPerBatch); err != nil {
		return domainnetworkingest.Acknowledgement{}, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_ingest_semantics", "The ingest batch violates telemetry constraints.", "遥测批次违反语义约束。")
	}
	events := make([]domainnetworkingest.Event, 0, len(batch.Events))
	for _, event := range batch.Events {
		encoded, err := json.Marshal(event)
		if err != nil {
			return domainnetworkingest.Acknowledgement{}, fmt.Errorf("marshal ingest event: %w", err)
		}
		hash, err := networkprotocol.EventHash(encoded)
		if err != nil {
			return domainnetworkingest.Acknowledgement{}, fmt.Errorf("hash ingest event: %w", err)
		}
		events = append(events, domainnetworkingest.Event{
			ProducerID: batch.ProducerID, EventID: event.ID, EventHash: hash, BatchID: batch.BatchID,
			ProducerKind: batch.ProducerKind, EventType: event.Type, Sequence: event.Sequence,
			OccurredAt: event.OccurredAt.UTC(), ReceivedAt: now, Payload: event.Payload,
		})
	}
	result, err := s.store.Append(ctx, events)
	if err != nil {
		return domainnetworkingest.Acknowledgement{}, err
	}
	return domainnetworkingest.Acknowledgement{BatchID: batch.BatchID, Accepted: result.Accepted, Duplicate: result.Duplicate}, nil
}

func (s *Service) IngestRADIUS(ctx context.Context, identity networkidentity.Identity, raw []byte) (domainnetworkingest.Acknowledgement, error) {
	if err := s.schemas.ValidateRadiusAccounting(raw); err != nil {
		return domainnetworkingest.Acknowledgement{}, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_radius_accounting", "The request does not match the RADIUS accounting contract.", "请求不符合 RADIUS Accounting 契约。")
	}
	if identity.Scope != networkidentity.ScopeIngest || identity.Kind != "freeradius" {
		return domainnetworkingest.Acknowledgement{}, apperrors.NewBusiness(apperrors.ErrUnauthorized, "producer_identity_mismatch", "The authenticated producer is not a FreeRADIUS ingest producer.", "已认证的生产者不是 FreeRADIUS 遥测生产者。")
	}
	var request networkprotocol.RadiusAccountingRequest
	if err := json.Unmarshal(raw, &request); err != nil {
		return domainnetworkingest.Acknowledgement{}, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_radius_accounting", "The RADIUS accounting request is invalid.", "RADIUS Accounting 请求无效。")
	}
	canonicalRequest, err := json.Marshal(request)
	if err != nil {
		return domainnetworkingest.Acknowledgement{}, fmt.Errorf("marshal RADIUS accounting request: %w", err)
	}
	digest := sha256.Sum256(append(append([]byte(identity.ID), '\n'), canonicalRequest...))
	eventID := fmt.Sprintf("radius-%x", digest)
	payload, err := json.Marshal(request.RadiusAccounting)
	if err != nil {
		return domainnetworkingest.Acknowledgement{}, fmt.Errorf("marshal RADIUS accounting payload: %w", err)
	}
	now := s.now().UTC()
	batch, err := json.Marshal(networkprotocol.IngestBatch{
		SchemaVersion: networkprotocol.IngestSchemaVersion,
		BatchID:       eventID,
		ProducerID:    identity.ID,
		ProducerKind:  identity.Kind,
		SentAt:        now,
		Events: []networkprotocol.IngestEvent{{
			ID: eventID, Type: networkprotocol.EventRadiusAccounting, Sequence: request.EventTimestamp,
			OccurredAt: time.Unix(request.EventTimestamp, 0).UTC(), Payload: payload,
		}},
	})
	if err != nil {
		return domainnetworkingest.Acknowledgement{}, fmt.Errorf("marshal canonical RADIUS accounting batch: %w", err)
	}
	return s.Ingest(ctx, identity, batch)
}

func (s *Service) Summary(ctx context.Context, identity networkidentity.Identity, filter domainnetworkingest.SummaryFilter) (domainnetworkingest.Summary, error) {
	if identity.Scope != networkidentity.ScopeIngest || identity.Kind != "core" {
		return domainnetworkingest.Summary{}, apperrors.NewBusiness(apperrors.ErrUnauthorized, "ingest_query_identity_required", "A dedicated core ingest-query identity is required.", "需要专用的 core ingest 查询身份。")
	}
	now := s.now().UTC()
	if filter.To.IsZero() {
		filter.To = now
	} else {
		filter.To = filter.To.UTC()
	}
	if filter.From.IsZero() {
		filter.From = filter.To.Add(-time.Hour)
	} else {
		filter.From = filter.From.UTC()
	}
	if filter.Limit == 0 {
		filter.Limit = 100
	}
	if !filter.To.After(filter.From) || filter.To.Sub(filter.From) > 7*24*time.Hour || filter.To.After(now.Add(s.options.MaxClockSkew)) || filter.Limit < 1 || filter.Limit > 200 || len(filter.ProducerID) > 128 || strings.TrimSpace(filter.ProducerID) != filter.ProducerID {
		return domainnetworkingest.Summary{}, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_ingest_query", "The telemetry query window or filter is invalid.", "遥测查询时间窗或过滤条件无效。")
	}
	summary, err := s.store.Summary(ctx, filter)
	if err != nil {
		return domainnetworkingest.Summary{}, err
	}
	summary.From, summary.To = filter.From, filter.To
	if summary.Producers == nil {
		summary.Producers = []domainnetworkingest.ProducerSummary{}
	}
	if summary.ProxyFlows == nil {
		summary.ProxyFlows = []domainnetworkingest.ProxyFlowSummary{}
	}
	return summary, nil
}
