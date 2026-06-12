package middleware

import "testing"

func TestAllowsExternalBearerTokenIncludesConnectorEvents(t *testing.T) {
	t.Parallel()

	if !allowsExternalBearerToken("/api/v1/connectors/events") {
		t.Fatal("connectors event sink should allow handler-level bearer fallback")
	}
}
