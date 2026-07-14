package appconfig

import "testing"

func TestModuleToggleFeatureFlagsFlattensViperNestedKeys(t *testing.T) {
	toggle := ModuleToggle{Features: map[string]any{
		"memory":     map[string]any{"long_term": false},
		"evaluation": map[string]any{"release_gate": true},
	}}
	flags := toggle.FeatureFlags()
	if flags["memory.long_term"] || !flags["evaluation.release_gate"] || len(flags) != 2 {
		t.Fatalf("flags = %#v", flags)
	}
}
