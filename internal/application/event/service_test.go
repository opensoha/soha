package event

import (
	"context"
	"strings"
	"testing"
	"time"

	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domainevent "github.com/opensoha/soha/internal/domain/event"
)

type memoryEventRepository struct {
	created []domainevent.Envelope
}

func (r *memoryEventRepository) Create(_ context.Context, event domainevent.Envelope) error {
	r.created = append(r.created, event)
	return nil
}

func (r *memoryEventRepository) List(context.Context, int) ([]domainevent.Envelope, error) {
	return append([]domainevent.Envelope(nil), r.created...), nil
}

func (r *memoryEventRepository) Get(_ context.Context, eventID string) (domainevent.Envelope, error) {
	for _, item := range r.created {
		if item.ID == eventID {
			return item, nil
		}
	}
	return domainevent.Envelope{}, nil
}

type captureEventAuditRecorder struct {
	entries []domainaudit.Entry
}

func (r *captureEventAuditRecorder) Record(_ context.Context, entry domainaudit.Entry) error {
	r.entries = append(r.entries, entry)
	return nil
}

func TestIngestConnectorEventsPersistsEventStreamEnvelopeAndAudit(t *testing.T) {
	repo := &memoryEventRepository{}
	audit := &captureEventAuditRecorder{}
	service := New(repo)
	service.SetAuditRecorder(audit)

	accepted, err := service.IngestConnectorEvents(context.Background(), domainevent.ConnectorEventIngestInput{
		ConnectorID:   "feishu",
		RequestPath:   "/api/v1/connectors/events",
		RequestMethod: "POST",
		RequestID:     "request-1",
		SourceIP:      "127.0.0.1",
		Events: []domainevent.ConnectorEvent{
			{
				ID:         "event-1",
				Type:       "im.message.receive_v1",
				Source:     "feishu",
				OccurredAt: "2026-06-11T08:00:00Z",
				Subject:    "oc_chat",
				Payload: map[string]any{
					"tenantKey": "tenant-1",
					"clusterId": "cluster-a",
					"namespace": "default",
				},
			},
		},
	})
	expectEvent(t, err == nil, "IngestConnectorEvents returned error: %v", err)
	expectEvent(t, accepted == 1, "accepted = %d, want 1", accepted)
	expectEvent(t, len(repo.created) == 1, "created events = %d, want 1", len(repo.created))
	event := repo.created[0]
	expectEvent(t, event.ID == "connector:feishu:event-1", "event id = %q", event.ID)
	expectEvent(t, event.Source == "connector.feishu", "event source = %q", event.Source)
	expectEvent(t, event.Category == "connector", "event category = %q", event.Category)
	expectEvent(t, event.Severity == "info", "event severity = %q", event.Severity)
	expectEvent(t, event.ClusterID == "cluster-a", "event cluster = %q", event.ClusterID)
	expectEvent(t, event.Namespace == "default", "event namespace = %q", event.Namespace)
	expectEvent(t, event.Payload["connectorId"] == "feishu", "connector id payload = %#v", event.Payload)
	expectEvent(t, event.Payload["connectorEventId"] == "event-1", "event id payload = %#v", event.Payload)
	expectEvent(t, event.Payload["connectorEventType"] == "im.message.receive_v1", "event type payload = %#v", event.Payload)
	expectEvent(t, !event.OccurredAt.IsZero(), "occurredAt is zero")
	expectEvent(t, event.OccurredAt.Equal(time.Date(2026, 6, 11, 8, 0, 0, 0, time.UTC)), "unexpected occurredAt: %v", event.OccurredAt)
	expectEvent(t, len(audit.entries) == 1, "audit entries = %d, want 1", len(audit.entries))
	entry := audit.entries[0]
	expectEvent(t, entry.ActorID == "connector:feishu", "audit actor = %q", entry.ActorID)
	expectEvent(t, entry.Action == "connector.events.ingest", "audit action = %q", entry.Action)
	expectEvent(t, entry.Result == "success", "audit result = %q", entry.Result)
	expectEvent(t, entry.RequestID == "request-1", "audit request id = %q", entry.RequestID)
	expectEvent(t, entry.RequestPath == "/api/v1/connectors/events", "audit request path = %q", entry.RequestPath)
}

func expectEvent(t *testing.T, condition bool, format string, args ...any) {
	t.Helper()
	if !condition {
		t.Fatalf(format, args...)
	}
}

func TestIngestConnectorEventsRecordsProvidedActorAttribution(t *testing.T) {
	repo := &memoryEventRepository{}
	audit := &captureEventAuditRecorder{}
	service := New(repo)
	service.SetAuditRecorder(audit)

	_, err := service.IngestConnectorEvents(context.Background(), domainevent.ConnectorEventIngestInput{
		ConnectorID: "feishu",
		ActorID:     "service_account:runtime-1",
		ActorName:   "feishu-runtime",
		ActorRoles:  []string{"connector-runtime"},
		ActorTeams:  []string{"integrations"},
		AuthKind:    "service_account_token",
		Events: []domainevent.ConnectorEvent{
			{
				ID:         "event-1",
				Type:       "im.message.receive_v1",
				Source:     "feishu",
				OccurredAt: "2026-06-11T08:00:00Z",
				Payload:    map[string]any{},
			},
		},
	})
	if err != nil {
		t.Fatalf("IngestConnectorEvents returned error: %v", err)
	}
	if len(audit.entries) != 1 {
		t.Fatalf("audit entries = %d, want 1", len(audit.entries))
	}
	entry := audit.entries[0]
	if entry.ActorID != "service_account:runtime-1" || entry.ActorName != "feishu-runtime" {
		t.Fatalf("unexpected actor attribution: %#v", entry)
	}
	if len(entry.Roles) != 1 || entry.Roles[0] != "connector-runtime" || len(entry.Teams) != 1 || entry.Teams[0] != "integrations" {
		t.Fatalf("unexpected actor scopes: roles=%v teams=%v", entry.Roles, entry.Teams)
	}
	if entry.Metadata["authKind"] != "service_account_token" {
		t.Fatalf("authKind metadata = %#v, want service_account_token", entry.Metadata["authKind"])
	}
}

func TestValidateConnectorEventSinkTokenFailsClosed(t *testing.T) {
	service := New(&memoryEventRepository{})
	for _, token := range []string{"", "sink-token"} {
		if err := service.ValidateConnectorEventSinkToken(token); err == nil {
			t.Fatalf("ValidateConnectorEventSinkToken(%q) error = nil, want fail-closed error", token)
		}
	}

	service.SetConnectorEventSinkToken("sink-token")
	if err := service.ValidateConnectorEventSinkToken("wrong"); err == nil {
		t.Fatal("ValidateConnectorEventSinkToken wrong token error = nil")
	}
	if err := service.ValidateConnectorEventSinkToken("sink-token"); err != nil {
		t.Fatalf("ValidateConnectorEventSinkToken returned error: %v", err)
	}
}

func TestIngestConnectorEventsRejectsInvalidPayload(t *testing.T) {
	service := New(&memoryEventRepository{})
	_, err := service.IngestConnectorEvents(context.Background(), domainevent.ConnectorEventIngestInput{
		ConnectorID: "feishu",
		Events: []domainevent.ConnectorEvent{
			{ID: "event-1"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "connector event type is required") {
		t.Fatalf("expected missing type error, got %v", err)
	}
}

func TestIngestConnectorEventsRejectsSchemaMismatches(t *testing.T) {
	service := New(&memoryEventRepository{})

	for _, tc := range []struct {
		name    string
		event   domainevent.ConnectorEvent
		message string
	}{
		{
			name: "missing source",
			event: domainevent.ConnectorEvent{
				ID:         "event-1",
				Type:       "im.message.receive_v1",
				OccurredAt: "2026-06-11T08:00:00Z",
				Payload:    map[string]any{},
			},
			message: "connector event source is required",
		},
		{
			name: "source mismatch",
			event: domainevent.ConnectorEvent{
				ID:         "event-1",
				Type:       "im.message.receive_v1",
				Source:     "wechat",
				OccurredAt: "2026-06-11T08:00:00Z",
				Payload:    map[string]any{},
			},
			message: "connector event source must match connectorId",
		},
		{
			name: "invalid source pattern",
			event: domainevent.ConnectorEvent{
				ID:         "event-1",
				Type:       "im.message.receive_v1",
				Source:     "Feishu",
				OccurredAt: "2026-06-11T08:00:00Z",
				Payload:    map[string]any{},
			},
			message: "connector event source must match connector source pattern",
		},
		{
			name: "invalid occurredAt",
			event: domainevent.ConnectorEvent{
				ID:         "event-1",
				Type:       "im.message.receive_v1",
				Source:     "feishu",
				OccurredAt: "not-a-date",
				Payload:    map[string]any{},
			},
			message: "connector event occurredAt must be RFC3339",
		},
		{
			name: "missing payload",
			event: domainevent.ConnectorEvent{
				ID:         "event-1",
				Type:       "im.message.receive_v1",
				Source:     "feishu",
				OccurredAt: "2026-06-11T08:00:00Z",
			},
			message: "connector event payload is required",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := service.IngestConnectorEvents(context.Background(), domainevent.ConnectorEventIngestInput{
				ConnectorID: "feishu",
				Events:      []domainevent.ConnectorEvent{tc.event},
			})
			if err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("expected %q error, got %v", tc.message, err)
			}
		})
	}
}
