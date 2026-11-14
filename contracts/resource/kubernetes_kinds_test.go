package resource

import (
	"strings"
	"testing"
)

func TestKubernetesResourceKindsAreUniqueAndKeepDedicatedLifecycles(t *testing.T) {
	seen := map[string]bool{}
	for _, definition := range KubernetesResourceKinds() {
		key := strings.ToLower(definition.Kind)
		if seen[key] || definition.Version == "" || definition.Resource == "" {
			t.Fatalf("invalid definition: %+v", definition)
		}
		seen[key] = true
		if got, ok := LookupKubernetesResourceKind(" " + key + " "); !ok || got != definition {
			t.Fatalf("lookup: %+v %v", got, ok)
		}
	}
	for _, kind := range []string{"Namespace", "CustomResourceDefinition", "WorkloadCronJob"} {
		if definition, ok := LookupKubernetesResourceKind(kind); !ok || definition.GenericYAML {
			t.Fatalf("%s must retain its dedicated lifecycle", kind)
		}
	}
	for _, kind := range []string{"HorizontalPodAutoscaler", "PodDisruptionBudget"} {
		if definition, ok := LookupKubernetesResourceKind(kind); !ok || !definition.GenericYAML || !definition.Namespaced {
			t.Fatalf("missing namespaced YAML surface: %s", kind)
		}
	}
	if _, ok := LookupKubernetesResourceKind("Unknown"); ok {
		t.Fatal("unknown kind accepted")
	}
	definitions := KubernetesResourceKinds()
	definitions[0].Kind = "changed"
	if KubernetesResourceKinds()[0].Kind == "changed" {
		t.Fatal("caller changed the shared registry")
	}
}
