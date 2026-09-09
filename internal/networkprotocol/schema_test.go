package networkprotocol

import "testing"

func TestSchemasValidateRuntimeAndIngestContracts(t *testing.T) {
	schemas, err := CompileSchemas()
	if err != nil {
		t.Fatalf("CompileSchemas() error = %v", err)
	}

	validRuntime := []byte(`{
  "schemaVersion":"network-runtime/v1alpha1",
  "messageId":"message-1",
  "messageType":"runtime.enroll.request",
  "producerId":"endpoint-1",
  "runtimeId":"endpoint-1",
  "runtimeKind":"endpoint",
  "occurredAt":"2026-09-02T08:00:00Z",
  "expiresAt":"2026-09-02T08:05:00Z",
  "payload":{
    "enrollmentId":"enrollment-1",
    "challengeId":"challenge-1",
    "deviceId":"device-1",
    "devicePublicKey":"01234567890123456789012345678901",
    "wireguardPublicKey":"BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=",
    "platform":"windows",
    "clientVersion":"1.0.0",
    "capabilities":["wireguard"]
  }
}`)
	if err := schemas.ValidateRuntime(validRuntime); err != nil {
		t.Fatalf("ValidateRuntime(valid) error = %v", err)
	}
	validConfiguration := []byte(`{
  "schemaVersion":"network-runtime/v1alpha1",
  "messageId":"message-2",
  "messageType":"configuration.desired",
  "producerId":"network-control",
  "runtimeId":"endpoint-1",
  "runtimeKind":"endpoint",
  "occurredAt":"2026-09-02T08:00:00Z",
  "expiresAt":"2026-09-02T08:05:00Z",
  "payload":{
    "configurationVersion":1,
    "policyVersion":1,
    "validUntil":"2026-09-02T08:05:00Z",
    "accessProfile":"full",
    "protectedResourceIds":[],
    "networkLeases":[],
    "resourceLeases":[],
    "runtimeIntervals":{"heartbeatIntervalSeconds":60,"configurationPollIntervalSeconds":60}
  }
}`)
	if err := schemas.ValidateRuntime(validConfiguration); err != nil {
		t.Fatalf("ValidateRuntime(configuration) error = %v", err)
	}

	validIngest := []byte(`{
  "schemaVersion":"network-ingest/v1alpha1",
  "batchId":"batch-1",
  "producerId":"gateway-1",
  "producerKind":"gateway",
  "sentAt":"2026-09-02T08:00:00Z",
  "events":[{
    "id":"event-1",
    "type":"runtime.heartbeat",
    "sequence":1,
    "occurredAt":"2026-09-02T08:00:00Z",
    "payload":{"status":"healthy","configurationVersion":1,"policyVersion":1,"uptimeSeconds":10}
  }]
}`)
	if err := schemas.ValidateIngest(validIngest); err != nil {
		t.Fatalf("ValidateIngest(valid) error = %v", err)
	}
	validRadius := []byte(`{"schemaVersion":"network-radius-accounting/v1alpha1","eventTimestamp":1788336000,"statusType":"start","accountingSessionId":"session-1","nasId":"nas-hq","sessionTimeSeconds":0}`)
	if err := schemas.ValidateRadiusAccounting(validRadius); err != nil {
		t.Fatalf("ValidateRadiusAccounting(valid) error = %v", err)
	}

	for name, test := range map[string]struct {
		raw      []byte
		validate func([]byte) error
	}{
		"unknown runtime field":  {raw: append(validRuntime[:len(validRuntime)-1], []byte(`,"unexpected":true}`)...), validate: schemas.ValidateRuntime},
		"wrong runtime payload":  {raw: []byte(`{"schemaVersion":"network-runtime/v1alpha1","messageId":"m","messageType":"lease.revoke","producerId":"p","runtimeId":"r","runtimeKind":"gateway","occurredAt":"2026-09-02T08:00:00Z","expiresAt":"2026-09-02T08:05:00Z","payload":{"accepted":true,"reasonCode":"ok"}}`), validate: schemas.ValidateRuntime},
		"short runtime interval": {raw: []byte(`{"schemaVersion":"network-runtime/v1alpha1","messageId":"m","messageType":"configuration.desired","producerId":"network-control","runtimeId":"endpoint-1","runtimeKind":"endpoint","occurredAt":"2026-09-02T08:00:00Z","expiresAt":"2026-09-02T08:05:00Z","payload":{"configurationVersion":1,"policyVersion":1,"validUntil":"2026-09-02T08:05:00Z","accessProfile":"full","protectedResourceIds":[],"networkLeases":[],"resourceLeases":[],"runtimeIntervals":{"heartbeatIntervalSeconds":29,"configurationPollIntervalSeconds":60}}}`), validate: schemas.ValidateRuntime},
		"invalid ingest time":    {raw: []byte(`{"schemaVersion":"network-ingest/v1alpha1","batchId":"b","producerId":"p","producerKind":"gateway","sentAt":"not-a-time","events":[{"id":"e","type":"runtime.heartbeat","sequence":0,"occurredAt":"2026-09-02T08:00:00Z","payload":{"status":"healthy","configurationVersion":0,"policyVersion":0,"uptimeSeconds":0}}]}`), validate: schemas.ValidateIngest},
		"RADIUS secret field":    {raw: append(validRadius[:len(validRadius)-1], []byte(`,"userPassword":"secret"}`)...), validate: schemas.ValidateRadiusAccounting},
	} {
		t.Run(name, func(t *testing.T) {
			if err := test.validate(test.raw); err == nil {
				t.Fatal("validation error = nil, want failure")
			}
		})
	}
}
