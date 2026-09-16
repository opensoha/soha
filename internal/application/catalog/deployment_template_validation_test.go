package catalog

import (
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	"testing"
)

func TestBuiltinDeploymentTemplatesAreValid(t *testing.T) {
	for _, spec := range domaincatalog.BuiltinDeploymentTemplates() {
		t.Run(spec.Key, func(t *testing.T) {
			if err := normalizeDeploymentTemplate(&spec); err != nil {
				t.Fatal(err)
			}
			if len(spec.ParameterSchema.Required) > 0 {
				if _, err := spec.ParameterSchema.ResolveParameters(spec.Defaults, nil, nil, nil); err == nil {
					t.Fatal("template should require service inputs")
				}
				return
			}
			if _, err := spec.ParameterSchema.ResolveParameters(spec.Defaults, nil, nil, nil); err != nil {
				t.Fatal(err)
			}
		})
	}
}
