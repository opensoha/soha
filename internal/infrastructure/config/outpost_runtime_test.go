package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOutpostRuntimeTokenFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	credential := OutpostRuntimeExecutorConfig{ExecutorID: "edge", TokenFile: path}
	if err := os.WriteFile(path, []byte(strings.Repeat("a", 32)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if token, err := credential.ReadToken(); err != nil || token != strings.Repeat("a", 32) {
		t.Fatalf("private token: %v", err)
	}
	if err := os.Chmod(path, 0664); err != nil {
		t.Fatal(err)
	}
	if _, err := credential.ReadToken(); err == nil {
		t.Fatal("accepted publicly readable token file")
	}
}

func TestOutpostRuntimeInstallationPolicy(t *testing.T) {
	policy := RuntimeConfig{
		OutpostRuntimeExecutors: []OutpostRuntimeExecutorConfig{{ExecutorID: "edge", TokenFile: "/secret/token"}},
		OutpostSlots:            []OutpostRuntimeSlotConfig{{ID: "slot", OutpostID: "outpost", ExecutorID: "edge", Namespace: "access", Name: "outpost", UID: "uid", TemplateDigest: "digest", MaxReplicas: 2}},
	}
	if problems := validateOutpostRuntimeConfig(policy); len(problems) != 0 {
		t.Fatalf("valid installer policy: %v", problems)
	}
	policy.OutpostSlots = append(policy.OutpostSlots, policy.OutpostSlots[0])
	if len(validateOutpostRuntimeConfig(policy)) == 0 {
		t.Fatal("accepted duplicate workload control")
	}
	policy.OutpostSlots = policy.OutpostSlots[:1]
	policy.OutpostSlots[0].ExecutorID = "other"
	if len(validateOutpostRuntimeConfig(policy)) == 0 {
		t.Fatal("accepted unconfigured executor")
	}
}
