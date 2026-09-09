package networkprotocol

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestValidateIngestBatchEnforcesTelemetrySemantics(t *testing.T) {
	now := time.Date(2026, 9, 2, 8, 0, 0, 0, time.UTC)
	valid := IngestBatch{
		SchemaVersion: IngestSchemaVersion,
		BatchID:       "batch-1",
		ProducerID:    "gateway-1",
		ProducerKind:  "gateway",
		SentAt:        now,
		Events: []IngestEvent{{
			ID:         "event-1",
			Type:       EventFlowAggregate,
			Sequence:   1,
			OccurredAt: now.Add(-time.Second),
			Payload:    json.RawMessage(`{"windowStartedAt":"2026-09-02T07:59:00Z","windowEndedAt":"2026-09-02T08:00:00Z","sessionId":"session-1","resourceId":"resource-1","direction":"egress","protocol":"tcp","bytes":100,"packets":2,"connections":1,"deniedConnections":0}`),
		}},
	}
	if err := ValidateIngestBatch(valid, now, 5*time.Minute, 7*24*time.Hour, 1000); err != nil {
		t.Fatalf("ValidateIngestBatch(valid) error = %v", err)
	}

	tests := map[string]struct {
		mutate func(*IngestBatch)
		want   string
	}{
		"producer scope": {mutate: func(batch *IngestBatch) { batch.ProducerKind = "endpoint" }, want: "not allowed"},
		"duplicate id":   {mutate: func(batch *IngestBatch) { batch.Events = append(batch.Events, batch.Events[0]) }, want: "duplicate"},
		"future event":   {mutate: func(batch *IngestBatch) { batch.Events[0].OccurredAt = now.Add(6 * time.Minute) }, want: "future"},
		"invalid flow": {mutate: func(batch *IngestBatch) {
			batch.Events[0].Payload = json.RawMessage(`{"windowStartedAt":"2026-09-02T08:01:00Z","windowEndedAt":"2026-09-02T08:00:00Z","sessionId":"session-1","resourceId":"resource-1","direction":"egress","protocol":"tcp","bytes":100,"packets":2,"connections":1,"deniedConnections":2}`)
		}, want: "window"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			batch := valid
			batch.Events = append([]IngestEvent(nil), valid.Events...)
			test.mutate(&batch)
			err := ValidateIngestBatch(batch, now, 5*time.Minute, 7*24*time.Hour, 1000)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestValidateRuntimeMessageWindowFailsClosed(t *testing.T) {
	now := time.Date(2026, 9, 2, 8, 0, 0, 0, time.UTC)
	message := RuntimeMessage{OccurredAt: now, ExpiresAt: now.Add(5 * time.Minute)}
	if err := ValidateRuntimeMessageWindow(message, now, 5*time.Minute); err != nil {
		t.Fatalf("valid window error = %v", err)
	}
	message.ExpiresAt = now
	if err := ValidateRuntimeMessageWindow(message, now, 5*time.Minute); err == nil {
		t.Fatal("expired window error = nil")
	}
}

func TestValidateIngestBatchAllowsEndpointProxyAggregateOnly(t *testing.T) {
	now := time.Date(2026, 9, 2, 8, 1, 0, 0, time.UTC)
	payload := json.RawMessage(`{"windowStartedAt":"2026-09-02T08:00:00Z","windowEndedAt":"2026-09-02T08:01:00Z","engine":"mihomo","profileId":"mihomo-1","profileRevision":1,"mode":"app_subscription","selectedProxy":"edge-a","uploadBytes":1024,"downloadBytes":4096,"activeConnections":2}`)
	batch := IngestBatch{SchemaVersion: IngestSchemaVersion, BatchID: "batch-1", ProducerID: "endpoint-1", ProducerKind: "endpoint", SentAt: now, Events: []IngestEvent{{ID: "event-1", Type: EventProxyFlowAggregate, Sequence: 1, OccurredAt: now, Payload: payload}}}
	if err := ValidateIngestBatch(batch, now, time.Minute, time.Hour, 10); err != nil {
		t.Fatalf("ValidateIngestBatch(endpoint proxy flow) = %v", err)
	}
	batch.ProducerKind = "gateway"
	if err := ValidateIngestBatch(batch, now, time.Minute, time.Hour, 10); err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("ValidateIngestBatch(gateway proxy flow) = %v", err)
	}
}

func TestEventHashIsCanonical(t *testing.T) {
	left := json.RawMessage(`{"id":"e","type":"runtime.heartbeat","sequence":1,"occurredAt":"2026-09-02T08:00:00Z","payload":{"status":"healthy","configurationVersion":1,"policyVersion":1,"uptimeSeconds":2}}`)
	right := json.RawMessage(`{ "payload": { "uptimeSeconds": 2, "policyVersion": 1, "configurationVersion": 1, "status": "healthy" }, "occurredAt": "2026-09-02T08:00:00Z", "sequence": 1, "type": "runtime.heartbeat", "id": "e" }`)
	leftHash, err := EventHash(left)
	if err != nil {
		t.Fatalf("EventHash(left) error = %v", err)
	}
	rightHash, err := EventHash(right)
	if err != nil {
		t.Fatalf("EventHash(right) error = %v", err)
	}
	if leftHash != rightHash {
		t.Fatalf("hashes differ: %s != %s", leftHash, rightHash)
	}
}
