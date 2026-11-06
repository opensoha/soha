package sohaapi

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestReasoningEffortNamesRemainCompatibleWithPodStatusEnums(t *testing.T) {
	values := []WorkbenchModelOptionReasoningEfforts{WorkbenchModelOptionReasoningEffortsHigh, WorkbenchModelOptionReasoningEffortsMedium, WorkbenchModelOptionReasoningEffortsLow}
	for index, want := range []string{"high", "medium", "low"} {
		if string(values[index]) != want || !values[index].Valid() {
			t.Fatalf("reasoning effort %d = %q, want %q", index, values[index], want)
		}
	}
	if High != MarketplaceAdvisorySeverityHigh {
		t.Fatal("legacy advisory High changed")
	}
}

func TestPodStatusSDKKeepsZeroExitCode(t *testing.T) {
	code := int32(0)
	encoded, err := json.Marshal(KubernetesPodContainerStatus{ExitCode: &code})
	if err != nil || !strings.Contains(string(encoded), `"exitCode":0`) {
		t.Fatalf("successful termination was lost: %s, %v", encoded, err)
	}
}

func TestPersonalEnvironmentBackendNamesRemainCompatibleWithPodStatusEnums(t *testing.T) {
	values := []PersonalEnvironmentTemplateBackend{Container, VM}
	for index, want := range []string{"container", "vm"} {
		if string(values[index]) != want || !values[index].Valid() {
			t.Fatalf("backend %d = %q, want %q", index, values[index], want)
		}
	}
}
