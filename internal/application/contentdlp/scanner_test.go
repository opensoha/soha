package contentdlp

import (
	"context"
	"strings"
	"testing"

	domain "github.com/opensoha/soha/internal/domain/contentdlp"
)

// Behavioral counterexamples prepared for the deferred P4 test gate.
func TestTextDLPDecisionBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, body, status string
		size               int64
		truncated          bool
	}{
		{"plain", "ordinary text", "clean", 13, false},
		{"private-key", "-----BEGIN PRIVATE KEY-----", "matched", 27, false},
		{"assignment", `password="example-secret-value"`, "matched", 31, false},
		{"encrypted", "-----BEGIN PGP MESSAGE-----", "encrypted", 27, false},
		{"binary", "hello\x00secret", "unsupported", 12, false},
		{"lossy-json", "hello\ufffd", "unsupported", 8, false},
		{"ascii-pdf", "%PDF-1.7", "unsupported", 8, false},
		{"truncated-prefix", "harmless prefix", "too_large", domain.MaxTextBytes + 1, true},
		{"size-mismatch", "small", "error", 8, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Text literals are sized independently of the decision implementation.
			size := tc.size
			if tc.name != "size-mismatch" && !tc.truncated {
				size = int64(len(tc.body))
			}
			policy := domain.Content{Enabled: true, Mode: "block", Detectors: []string{"private_key", "credential_assignment"}, Rules: []domain.Rule{}}
			d := scan(context.Background(), policy, tc.body, size, tc.truncated)
			if d.Status != tc.status || d.Allowed != (tc.status == "clean") {
				t.Fatalf("decision=%+v", d)
			}
			policy.Mode = "audit"
			audit := scan(context.Background(), policy, tc.body, size, tc.truncated)
			if !audit.Allowed {
				t.Fatal("audit must preserve explicit non-blocking mode")
			}
		})
	}
}

func TestDLPValidationAndCustomRule(t *testing.T) {
	policy := domain.Content{Enabled: true, Mode: "block", Detectors: []string{}, Rules: []domain.Rule{{ID: "classification", Expression: `CLASSIFIED-[0-9]+`}}}
	if err := validate(policy); err != nil {
		t.Fatal(err)
	}
	d := scan(context.Background(), policy, "CLASSIFIED-123", 14, false)
	if d.Allowed || d.Status != "matched" || len(d.MatchedRuleIDs) != 1 {
		t.Fatalf("decision=%+v", d)
	}
	for _, expression := range []string{"[", strings.Repeat("x", 257)} {
		policy.Rules[0].Expression = expression
		if validate(policy) == nil {
			t.Fatal("invalid expression accepted")
		}
	}
}
