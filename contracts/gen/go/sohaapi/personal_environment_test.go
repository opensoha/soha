package sohaapi

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

func TestEnvironmentTemplateKeepsTypedOptionalPersonalPolicy(t *testing.T) {
	input := AIEnvironmentTemplateInput{ID: "personal", Name: "Personal", Backend: AIEnvironmentTemplateInputBackend("container"), IsolationMode: AIEnvironmentTemplateInputIsolationMode("disposable-write"), PersonalDocker: &PersonalDockerEnvironmentPolicy{
		AgentClusterID: uuid.MustParse("00000000-0000-4000-8000-000000000048"), LeaseSeconds: 600,
		Quota: PersonalEnvironmentQuota{DiskBytes: 32 << 20, MemoryBytes: 64 << 20, CPUUnits: 250, MaxProcesses: 16}}}
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var decoded AIEnvironmentTemplateInput
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.ID != input.ID || decoded.Backend != input.Backend || decoded.PersonalDocker == nil || decoded.PersonalDocker.AgentClusterID != input.PersonalDocker.AgentClusterID || decoded.PersonalDocker.Quota.DiskBytes != input.PersonalDocker.Quota.DiskBytes {
		t.Fatal("generated DTO lost typed policy or template fields")
	}
}
