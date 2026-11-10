package mcp

import (
	"slices"
	"testing"
)

func TestNativeObservabilityAdapters(t *testing.T) {
	registry := NewRegistry(0)
	logs, ok := registry.Get("logs.v1")
	if !ok || !slices.Equal(logs.SupportedBackends, []string{"loki"}) {
		t.Fatalf("logs.v1 backends = %v, registered = %t", logs.SupportedBackends, ok)
	}
	if _, ok := registry.Get("traces.v1"); ok {
		t.Fatal("native Trace adapter should be absent")
	}
}
