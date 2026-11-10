package docker

import (
	"strings"
	"testing"

	dockerruntime "github.com/opensoha/soha-contracts/docker/runtime"
)

func TestRuntimeSnapshotRetainsOnlyConfirmedIDsAndRejectsOtherProjects(t *testing.T) {
	binding, _ := dockerruntime.NewProjectBinding("11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222")
	services := []Service{{HostID: "host", ProjectID: "project", Name: "web", ContainerID: strings.Repeat("a", 64)}, {HostID: "host", ProjectID: "project", Name: "definition", Status: "defined"}, {HostID: "host", ProjectID: "project", Name: "removed", Status: "removed"}}
	items, err := RuntimeContainerSnapshot(binding, "host", "project", services)
	if err != nil || len(items) != 1 || items[0].ContainerID != services[0].ContainerID {
		t.Fatal(items, err)
	}
	services[0].ContainerID = "short"
	if _, err := RuntimeContainerSnapshot(binding, "host", "project", services); err == nil {
		t.Fatal("short runtime ID accepted")
	}
	services[0].ContainerID = strings.Repeat("a", 64)
	services[0].HostID = "other"
	if _, err := RuntimeContainerSnapshot(binding, "host", "project", services); err == nil {
		t.Fatal("foreign host snapshot accepted")
	}
}
