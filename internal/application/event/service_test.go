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
	if err != nil {
		t.Fatalf("IngestConnectorEvents returned error: %v", err)
	}
	if accepted != 1 {
		t.Fatalf("accepted = %d, want 1", accepted)
	}
	if len(repo.created) != 1 {
		t.Fatalf("created events = %d, want 1", len(repo.created))
	}
	event := repo.created[0]
	if event.ID != "connector:feishu:event-1" || event.Source != "connector.feishu" || event.Category != "connector" || event.Severity != "info" {
		t.Fatalf("unexpected envelope identity: %#v", event)
	}
	if event.ClusterID != "cluster-a" || event.Namespace != "default" {
		t.Fatalf("unexpected envelope scope: %#v", event)
	}
	if event.Payload["connectorId"] != "feishu" || event.Payload["connectorEventId"] != "event-1" || event.Payload["connectorEventType"] != "im.message.receive_v1" {
		t.Fatalf("missing connector metadata in payload: %#v", event.Payload)
	}
	if event.OccurredAt.IsZero() || !event.OccurredAt.Equal(time.Date(2026, 6, 11, 8, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected occurredAt: %v", event.OccurredAt)
	}
	if len(audit.entries) != 1 {
		t.Fatalf("audit entries = %d, want 1", len(audit.entries))
	}
	entry := audit.entries[0]
	if entry.ActorID != "connector:feishu" || entry.Action != "connector.events.ingest" || entry.Result != "success" {
		t.Fatalf("unexpected audit entry: %#v", entry)
	}
	if entry.RequestID != "request-1" || entry.RequestPath != "/api/v1/connectors/events" {
		t.Fatalf("audit request metadata missing: %#v", entry)
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
