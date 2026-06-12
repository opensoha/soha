package event

import (
	"context"
	"crypto/subtle"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domainevent "github.com/opensoha/soha/internal/domain/event"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type Repository interface {
	Create(context.Context, domainevent.Envelope) error
	List(context.Context, int) ([]domainevent.Envelope, error)
	Get(context.Context, string) (domainevent.Envelope, error)
}

type AuditRecorder interface {
	Record(context.Context, domainaudit.Entry) error
}

type Service struct {
	repo           Repository
	audit          AuditRecorder
	eventSinkToken string
}

func New(repo Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) SetAuditRecorder(audit AuditRecorder) {
	s.audit = audit
}

func (s *Service) SetConnectorEventSinkToken(token string) {
	s.eventSinkToken = strings.TrimSpace(token)
}

func (s *Service) ValidateConnectorEventSinkToken(token string) error {
	token = strings.TrimSpace(token)
	if s.eventSinkToken == "" || token == "" {
		return fmt.Errorf("%w: connector event sink token is invalid", apperrors.ErrUnauthorized)
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte(s.eventSinkToken)) != 1 {
		return fmt.Errorf("%w: connector event sink token is invalid", apperrors.ErrUnauthorized)
	}
	return nil
}

func (s *Service) List(ctx context.Context, limit int) ([]domainevent.Envelope, error) {
	if s.repo == nil {
		return []domainevent.Envelope{}, nil
	}
	return s.repo.List(ctx, limit)
}

func (s *Service) Get(ctx context.Context, eventID string) (domainevent.Envelope, error) {
	if s.repo == nil {
		return domainevent.Envelope{}, nil
	}
	return s.repo.Get(ctx, eventID)
}

func (s *Service) IngestConnectorEvents(ctx context.Context, input domainevent.ConnectorEventIngestInput) (int, error) {
	if s.repo == nil {
		return 0, fmt.Errorf("%w: event repository is not configured", apperrors.ErrInvalidArgument)
	}
	connectorID := strings.TrimSpace(input.ConnectorID)
	if connectorID == "" {
		return 0, fmt.Errorf("%w: connectorId is required", apperrors.ErrInvalidArgument)
	}
	if len(input.Events) == 0 {
		return 0, fmt.Errorf("%w: events are required", apperrors.ErrInvalidArgument)
	}

	accepted := 0
	eventIDs := make([]string, 0, len(input.Events))
	sources := make([]string, 0, len(input.Events))
	for _, event := range input.Events {
		envelope, err := connectorEventEnvelope(connectorID, event)
		if err != nil {
			return accepted, err
		}
		if err := s.repo.Create(ctx, envelope); err != nil {
			return accepted, err
		}
		accepted++
		eventIDs = append(eventIDs, envelope.ID)
		if !containsString(sources, envelope.Source) {
			sources = append(sources, envelope.Source)
		}
	}
	s.recordConnectorEventAudit(ctx, input, connectorID, eventIDs, sources, accepted)
	return accepted, nil
}

func connectorEventEnvelope(connectorID string, event domainevent.ConnectorEvent) (domainevent.Envelope, error) {
	if !validConnectorSource(connectorID) {
		return domainevent.Envelope{}, fmt.Errorf("%w: connectorId must match connector source pattern", apperrors.ErrInvalidArgument)
	}
	eventID := strings.TrimSpace(event.ID)
	if eventID == "" {
		return domainevent.Envelope{}, fmt.Errorf("%w: connector event id is required", apperrors.ErrInvalidArgument)
	}
	eventType := strings.TrimSpace(event.Type)
	if eventType == "" {
		return domainevent.Envelope{}, fmt.Errorf("%w: connector event type is required", apperrors.ErrInvalidArgument)
	}
	source := strings.TrimSpace(event.Source)
	if source == "" {
		return domainevent.Envelope{}, fmt.Errorf("%w: connector event source is required", apperrors.ErrInvalidArgument)
	}
	if !validConnectorSource(source) {
		return domainevent.Envelope{}, fmt.Errorf("%w: connector event source must match connector source pattern", apperrors.ErrInvalidArgument)
	}
	if source != connectorID {
		return domainevent.Envelope{}, fmt.Errorf("%w: connector event source must match connectorId", apperrors.ErrInvalidArgument)
	}
	if event.Payload == nil {
		return domainevent.Envelope{}, fmt.Errorf("%w: connector event payload is required", apperrors.ErrInvalidArgument)
	}
	occurredAt, err := parseConnectorEventTime(event.OccurredAt)
	if err != nil {
		return domainevent.Envelope{}, err
	}
	payload := cloneConnectorPayload(event.Payload)
	payload["connectorId"] = connectorID
	payload["connectorEventId"] = eventID
	payload["connectorEventType"] = eventType
	payload["connectorSource"] = source
	if subject := strings.TrimSpace(event.Subject); subject != "" {
		payload["subject"] = subject
	}
	if !occurredAt.IsZero() {
		payload["occurredAt"] = occurredAt.Format(time.RFC3339Nano)
	}
	return domainevent.Envelope{
		ID:         connectorEventEnvelopeID(connectorID, eventID),
		Source:     "connector." + source,
		Category:   "connector",
		Severity:   "info",
		ClusterID:  connectorPayloadString(payload, "clusterId"),
		Namespace:  connectorPayloadString(payload, "namespace"),
		Summary:    connectorEventSummary(connectorID, eventType, event.Subject),
		Payload:    payload,
		OccurredAt: occurredAt,
	}, nil
}

func validConnectorSource(value string) bool {
	if value == "" {
		return false
	}
	for i := 0; i < len(value); i++ {
		ch := value[i]
		if i == 0 {
			if ch < 'a' || ch > 'z' {
				return false
			}
			continue
		}
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-' {
			continue
		}
		return false
	}
	return true
}

func connectorEventEnvelopeID(connectorID, eventID string) string {
	return "connector:" + connectorID + ":" + eventID
}

func connectorEventSummary(connectorID, eventType, subject string) string {
	if subject = strings.TrimSpace(subject); subject != "" {
		return fmt.Sprintf("connector %s received %s for %s", connectorID, eventType, subject)
	}
	return fmt.Sprintf("connector %s received %s", connectorID, eventType)
}

func parseConnectorEventTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: connector event occurredAt must be RFC3339", apperrors.ErrInvalidArgument)
	}
	return parsed.UTC(), nil
}

func cloneConnectorPayload(payload map[string]any) map[string]any {
	out := make(map[string]any, len(payload)+6)
	for key, value := range payload {
		out[key] = value
	}
	return out
}

func connectorPayloadString(payload map[string]any, key string) string {
	value, ok := payload[key]
	if !ok {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

func containsString(values []string, candidate string) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

func (s *Service) recordConnectorEventAudit(ctx context.Context, input domainevent.ConnectorEventIngestInput, connectorID string, eventIDs, sources []string, accepted int) {
	if s.audit == nil {
		return
	}
	actorID := strings.TrimSpace(input.ActorID)
	if actorID == "" {
		actorID = "connector:" + connectorID
	}
	actorName := strings.TrimSpace(input.ActorName)
	if actorName == "" {
		actorName = "Connector Runtime"
	}
	_ = s.audit.Record(ctx, domainaudit.Entry{
		ID:            uuid.NewString(),
		ActorID:       actorID,
		ActorName:     actorName,
		Roles:         append([]string(nil), input.ActorRoles...),
		Teams:         append([]string(nil), input.ActorTeams...),
		ResourceKind:  "ConnectorEvent",
		ResourceName:  connectorID,
		Action:        "connector.events.ingest",
		Result:        "success",
		Summary:       fmt.Sprintf("ingested %d connector event(s)", accepted),
		RequestPath:   input.RequestPath,
		RequestMethod: input.RequestMethod,
		RequestID:     input.RequestID,
		SourceIP:      input.SourceIP,
		Metadata: map[string]any{
			"connectorId": connectorID,
			"eventIds":    append([]string(nil), eventIDs...),
			"sources":     append([]string(nil), sources...),
			"eventCount":  accepted,
			"authKind":    strings.TrimSpace(input.AuthKind),
		},
		CreatedAt: time.Now().UTC(),
	})
}
