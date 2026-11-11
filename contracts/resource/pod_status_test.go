package resource

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPodContainerStatusKeepsSuccessfulExitAndOldPayloads(t *testing.T) {
	exitCode := int32(0)
	encoded, err := json.Marshal(PodView{ContainerStatuses: []PodContainerStatusView{{Name: "migrate", Kind: "init", State: "terminated", ExitCode: &exitCode}}})
	if err != nil || !strings.Contains(string(encoded), `"exitCode":0`) {
		t.Fatalf("successful termination was lost: %s, %v", encoded, err)
	}
	var old PodView
	if err := json.Unmarshal([]byte(`{"name":"api","namespace":"apps","phase":"Running","readyContainers":"1/1","restarts":0,"ageSeconds":1}`), &old); err != nil {
		t.Fatal(err)
	}
	if old.QOSClass != "" || len(old.ContainerStatuses) != 0 {
		t.Fatal("old payload must not invent QoS or container states")
	}
}
