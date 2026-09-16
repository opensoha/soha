package aigateway

import (
	"encoding/json"
	"errors"
	"testing"

	domain "github.com/opensoha/soha/internal/domain/aigateway"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestCapabilityCheckDiscoveryUsesVisibleVersionedReadCapabilities(t *testing.T) {
	read := domain.ToolCapability{Name: "new-domain.inspect", Version: "v3", RiskLevel: domain.RiskLevelRead, Execution: &domain.ToolExecutionContract{Mode: "sync", Idempotent: true}, ProducesAssessment: true}
	check := domain.CapabilityCheckReference{Purpose: "verification", ToolName: read.Name, CapabilityVersion: read.Version}
	write := domain.ToolCapability{Name: "new-domain.create", Version: "v3", Execution: &domain.ToolExecutionContract{Mode: "sync", Idempotent: true, RecoveryMode: "original_call", Checks: []domain.CapabilityCheckReference{check}}}
	for _, scenario := range []string{"valid", "hidden", "stale-version", "mutation", "no-assessment", "async", "not-idempotent"} {
		target := read
		execution := *read.Execution
		target.Execution = &execution
		switch scenario {
		case "stale-version":
			target.Version = "v4"
		case "mutation":
			target.RiskLevel = domain.RiskLevelExecute
		case "no-assessment":
			target.ProducesAssessment = false
		case "async":
			target.Execution.Mode = "async"
		case "not-idempotent":
			target.Execution.Idempotent = false
		}
		visible := []domain.ToolCapability{write, target}
		if scenario == "hidden" {
			visible = visible[:1]
		}
		// References outside the search page stay discoverable if otherwise visible.
		found, _, _, err := discoverCapabilities(visible, domain.ManifestRequest{Query: "new-domain.create", Limit: 1})
		if err != nil || len(found) != 1 {
			t.Fatalf("%s: %v %v", scenario, found, err)
		}
		if (len(found[0].Execution.Checks) == 1) != (scenario == "valid") {
			t.Fatalf("%s: %+v", scenario, found[0].Execution)
		}
		if len(write.Execution.Checks) != 1 {
			t.Fatal("caller visibility mutated provider catalog")
		}
	}
	tools := []domain.ToolCapability{write, read}
	_, _, cursor, err := discoverCapabilities(tools, domain.ManifestRequest{Limit: 1})
	if err != nil || cursor == "" {
		t.Fatal("expected discovery cursor")
	}
	read.Version = "v4"
	if _, _, _, err := discoverCapabilities([]domain.ToolCapability{write, read}, domain.ManifestRequest{Limit: 1, Cursor: cursor}); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("check version change did not invalidate cursor: %v", err)
	}
}

func TestOwningProvidersPublishResolvableCheckContracts(t *testing.T) {
	vm, err := NewVirtualizationCapabilityProvider(nil)
	if err != nil {
		t.Fatal(err)
	}
	delivery, err := NewDeliveryCapabilityProvider(nil)
	if err != nil {
		t.Fatal(err)
	}
	tools := newCapabilityRegistry(vm, delivery, BuiltinCapabilityProvider{}).Tools()
	visible := map[string]domain.ToolCapability{}
	for _, tool := range tools {
		visible[tool.Name] = tool
	}
	checked := 0
	schema, err := capabilityOpenAPISchema("ToolCapability")
	if err != nil {
		t.Fatal(err)
	}
	validator, err := compileCapabilityInputSchema(schema)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools {
		if tool.Execution == nil || len(tool.Execution.Checks) == 0 {
			continue
		}
		checked++
		filtered := withVisibleCapabilityChecks(tool, visible)
		if len(filtered.Execution.Checks) != len(tool.Execution.Checks) {
			t.Fatalf("broken check reference on %s: %+v", tool.Name, tool.Execution.Checks)
		}
		raw, err := json.Marshal(tool)
		if err != nil {
			t.Fatal(err)
		}
		var value any
		if err := json.Unmarshal(raw, &value); err != nil {
			t.Fatal(err)
		}
		if err := validator.Validate(value); err != nil {
			t.Fatalf("%s violates public contract: %v", tool.Name, err)
		}
	}
	if checked < 4 {
		t.Fatalf("expected VM, worker, Docker and delivery checks; got %d", checked)
	}
}
