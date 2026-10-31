package aigateway

import (
	"strings"
	"testing"
)

func TestDefaultSecretDetectionKeepsOrdinaryResourceIdentifiers(t *testing.T) {
	rule := gatewayRedactionRule{SecretTypes: []string{"default"}, Replacement: "[REDACTED]"}
	for _, name := range []string{"task115-0123456789abcdef", "scheduled-task-service", "disk-volume-0123456789abcdef"} {
		if !gatewayRedactionAuditSummaryForValue(map[string]any{"namespace": name}, rule, "input").empty() {
			t.Fatalf("ordinary resource identifier classified as credential: %s", name)
		}
		if gatewayRedactSensitiveTextWithRule(name, rule) != name {
			t.Fatal("ordinary resource identifier was rewritten")
		}
	}
	for _, prefix := range []string{"sk-", "ghp_", "github_pat_", "glpat-", "xoxb-"} {
		secret := prefix + strings.Repeat("a", 32)
		for _, value := range []string{secret, "Bearer " + secret, "value=" + secret, "(" + secret + ")"} {
			if gatewayRedactionAuditSummaryForValue(value, rule, "input").empty() || strings.Contains(gatewayRedactSensitiveTextWithRule(value, rule), secret) {
				t.Fatalf("real credential form lost detection: %s", prefix)
			}
		}
	}
}
