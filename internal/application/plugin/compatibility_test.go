package plugin

import (
	domainplugin "github.com/opensoha/soha/internal/domain/plugin"
	"testing"
)

func TestCompatibilityUsesBuiltVersionAndCompleteConstraint(t *testing.T) {
	for _, tc := range []struct {
		version, constraint string
		allowed             bool
	}{
		{"0.1.8", ">=0.1.8 <1.0.0", true},
		{"0.1.8+local", ">=0.1.8 <1.0.0", true},
		{"0.1.0", ">=0.1.8", false},
		{"1.0.0", ">=0.1.8 <1.0.0", false},
		{"0.1.8", "invalid", false},
		{"dev", ">=0.1.8", false},
	} {
		t.Run(tc.version+tc.constraint, func(t *testing.T) {
			err := validateCompatibility(&domainplugin.PluginCompatibility{Soha: tc.constraint}, tc.version)
			if (err == nil) != tc.allowed {
				t.Fatalf("compatibility = %v, allowed = %v", err, tc.allowed)
			}
		})
	}
}
