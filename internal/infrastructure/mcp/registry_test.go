package mcp

import "testing"

func TestRegistryIncludesSkyWalkingTraceBackend(t *testing.T) {
	registry := NewRegistry(0)
	adapter, ok := registry.Get("traces.v1")
	if !ok {
		t.Fatalf("expected traces.v1 adapter to exist")
	}
	foundJaeger := false
	foundSkyWalking := false
	for _, item := range adapter.SupportedBackends {
		if item == "jaeger" {
			foundJaeger = true
		}
		if item == "skywalking" {
			foundSkyWalking = true
		}
	}
	if !foundJaeger {
		t.Fatalf("expected traces.v1 to keep jaeger support")
	}
	if !foundSkyWalking {
		t.Fatalf("expected traces.v1 to include skywalking support")
	}
}
