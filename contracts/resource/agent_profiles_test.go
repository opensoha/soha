package resource

import (
	"slices"
	"testing"
)

func TestKubernetesAgentProfilesKeepPermissionsExplicit(t *testing.T) {
	workbench, err := KubernetesAgentProfileActions("workbench")
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"platform.nodes.drain", "platform.resources.apply", "platform.resources.create", "platform.resources.delete", "platform.port_forwards.tunnel"} {
		if !slices.Contains(workbench, action) {
			t.Fatalf("missing workbench action %s", action)
		}
	}
	if slices.Contains(workbench, "*") || slices.Contains(workbench, "platform.crds.delete") {
		t.Fatal("workbench must not opt into unrestricted or CRD deletion actions")
	}
	legacy, _ := KubernetesAgentProfileActions("legacy")
	if !slices.Equal(workbench, legacy) {
		t.Fatal("legacy action set changed")
	}
	implemented := KubernetesAgentActions()
	for _, action := range []string{"platform.helm_releases.install", "platform.helm_releases.values_update", "platform.helm_releases.delete", "platform.helm_releases.rollback"} {
		if slices.Contains(workbench, action) || !slices.Contains(implemented, action) {
			t.Fatalf("Helm mutation %s must require explicit opt-in", action)
		}
	}
	readOnly, _ := KubernetesAgentProfileActions("read-only")
	if !slices.Equal(readOnly, []string{"platform.custom_resources.list"}) {
		t.Fatalf("read-only contains writes: %v", readOnly)
	}
	if _, err := KubernetesAgentProfileActions("unknown"); err == nil {
		t.Fatal("unknown profile accepted")
	}
	workbench[0] = "changed"
	if next, _ := KubernetesAgentProfileActions("workbench"); next[0] == "changed" {
		t.Fatal("profile leaked mutable storage")
	}
}
