package docker

import (
	"strings"
	"testing"

	dockerruntime "github.com/opensoha/soha/contracts/docker/runtime"
)

func TestProjectRuntimePayloadIsOptInAndFreezesUIDs(t *testing.T) {
	host := Host{ID: "host", ResourceUID: "11111111-1111-4111-8111-111111111111"}
	project := Project{ID: "project", HostID: host.ID, ResourceUID: "22222222-2222-4222-8222-222222222222"}
	content := "name: user-name\nservices:\n  web:\n    image: alpine:3.21\n"
	payload := map[string]any{"composeContent": content, "envContent": "COMPOSE_PROJECT_NAME=other", "projectSlug": "same-slug"}
	legacy, err := BindProjectRuntimePayload(project, host, payload)
	if err != nil || legacy["runtimeBinding"] != nil {
		t.Fatal("legacy namespace changed", err)
	}
	project.RuntimeIdentityVersion = 1
	bound, err := BindProjectRuntimePayload(project, host, payload)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := dockerruntime.ProjectBindingFromValue(bound["runtimeBinding"])
	if err != nil || binding.ProjectUID != project.ResourceUID || binding.HostUID != host.ResourceUID || bound["composeContent"] != content || payload["runtimeBinding"] != nil {
		t.Fatal("binding mutated original input or lost identity", err)
	}
	project.ResourceUID = "33333333-3333-4333-8333-333333333333"
	if _, err := BindProjectRuntimePayload(project, host, bound); err == nil {
		t.Fatal("original request silently rebound after recreation")
	}
	project.RuntimeIdentityVersion = 2
	if _, err := BindProjectRuntimePayload(project, host, payload); err == nil {
		t.Fatal("future protocol downgraded")
	}
}

func TestRuntimeServicePayloadFreezesFullContainerID(t *testing.T) {
	host := Host{ID: "host", ResourceUID: "11111111-1111-4111-8111-111111111111"}
	project := Project{ID: "project", HostID: host.ID, ResourceUID: "22222222-2222-4222-8222-222222222222", RuntimeIdentityVersion: 1}
	payload := map[string]any{"composeContent": "services:\n  web:\n    image: alpine\n", "serviceName": "web", "runtimeContainerId": strings.Repeat("a", 64)}
	bound, err := BindProjectRuntimePayload(project, host, payload)
	if err != nil {
		t.Fatal(err)
	}
	target, err := dockerruntime.ContainerBindingFromValue(bound["runtimeContainer"])
	if err != nil || target.ContainerID != payload["runtimeContainerId"] || bound["runtimeContainerId"] != nil {
		t.Fatal(target, err)
	}
	for _, id := range []string{"", strings.Repeat("a", 12), "container-name", "--help"} {
		payload["runtimeContainerId"] = id
		if _, err := BindProjectRuntimePayload(project, host, payload); err == nil {
			t.Fatal("unconfirmed container accepted", id)
		}
	}
}
