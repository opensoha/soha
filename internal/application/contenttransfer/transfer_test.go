package contenttransfer

import (
	"context"
	"crypto/hmac"
	"testing"

	d "github.com/opensoha/soha/internal/domain/contenttransfer"
	"github.com/opensoha/soha/internal/domain/identity"
)

func TestRequestIdentityAndApprovalTargetHaveSeparateBindings(t *testing.T) {
	key := []byte("synthetic-32-byte-binding-key-0000")
	p := identity.Principal{UserID: "owner"}
	first := operation{id: "request", action: "upload", destinationID: "destination", name: "original.txt", key: "allocated-object", content: []byte("original")}
	gates := []d.Gate{{DestinationID: "destination", PolicyVersion: 2}}
	backends := []string{"backend-v1"}
	changed := first
	changed.content = []byte("changed")
	if hmac.Equal(requestTag(key, p, first), requestTag(key, p, changed)) {
		t.Fatal("idempotency accepted a different payload")
	}
	retry := first
	retry.id = "new-attempt"
	if !hmac.Equal(operationTag(key, p, first, gates, backends), operationTag(key, p, retry, gates, backends)) {
		t.Fatal("a new attempt cannot target the approved preallocated object")
	}
	retry.key = "another-object"
	if hmac.Equal(operationTag(key, p, first, gates, backends), operationTag(key, p, retry, gates, backends)) {
		t.Fatal("approval accepted another storage object")
	}
	if hmac.Equal(operationTag(key, p, first, gates, backends), operationTag(key, p, first, gates, []string{"backend-v2"})) {
		t.Fatal("approval accepted a changed connection snapshot")
	}
}
func TestCompositePoliciesKeepUnknownAndStrictDecisions(t *testing.T) {
	config := func(mode string) *d.Config {
		return &d.Config{Policy: d.Policy{Mode: mode, Detectors: []string{"credential_assignment"}}}
	}
	cases := []struct {
		name, first, second string
		content             []byte
		allowed             bool
		action              string
	}{{"warn match", "audit", "warn", []byte("password = synthetic_value_123"), false, "acknowledge"}, {"block match", "warn", "block", []byte("password = synthetic_value_123"), false, "appeal"}, {"uninspected cannot be acknowledged", "audit", "warn", []byte{0, 1}, false, ""}, {"audit retains uninspected status", "audit", "audit", []byte{0, 1}, true, ""}}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			op := operation{name: "file.txt", content: c.content, gates: []scopeGate{{destination: d.Destination{Published: config(c.first)}}, {destination: d.Destination{Published: config(c.second)}}}}
			decision, _ := evaluate(context.Background(), op)
			if decision.Allowed != c.allowed || decision.RequiredAction != c.action {
				t.Fatalf("unexpected decision: %+v", decision)
			}
			if len(c.content) == 2 && decision.Status == "clean" {
				t.Fatal("binary content became clean")
			}
		})
	}
}

func TestAuditOnlyMatchDoesNotMasqueradeAsBlockBypass(t *testing.T) {
	audit := &d.Config{Policy: d.Policy{Mode: "audit", Detectors: []string{"credential_assignment"}}}
	block := &d.Config{Policy: d.Policy{Mode: "block", Detectors: []string{"private_key"}}}
	op := operation{name: "example.txt", content: []byte("password = synthetic_value_123"), gates: []scopeGate{{destination: d.Destination{Published: audit}}, {destination: d.Destination{Published: block}}}}
	decision, _ := evaluate(context.Background(), op)
	if !decision.Allowed || decision.Status != "matched" || decision.Mode != "audit" {
		t.Fatalf("audit-only exception hidden by an unrelated clean block gate: %+v", decision)
	}
}
