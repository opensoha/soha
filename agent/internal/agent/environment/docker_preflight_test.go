package environment

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"
)

func TestQuotaPreflightChecksActualContainerLimits(t *testing.T) {
	quota := personalDockerRequest("owner", "lease").Lease.Quota
	for _, wrong := range []string{"", "Memory", "MemorySwap", "NanoCpus", "PidsLimit", "NetworkMode", "ReadonlyRootfs", "CapDrop", "SecurityOpt"} {
		t.Run(wrong, func(t *testing.T) {
			config := map[string]any{"Memory": quota.MemoryBytes, "MemorySwap": quota.MemoryBytes, "NanoCpus": quota.CPUUnits * 1000000, "PidsLimit": quota.MaxProcesses, "NetworkMode": "none", "ReadonlyRootfs": true, "CapDrop": []string{"ALL"}, "SecurityOpt": []string{"no-new-privileges:true"}}
			delete(config, wrong)
			executor, err := NewDockerExecutor(testPersonalImage, testQuotaImage)
			if err != nil {
				t.Fatal(err)
			}
			executor.run = func(context.Context, ...string) ([]byte, error) { return json.Marshal(config) }
			if err := executor.verifyPersonalProbeLimits(t.Context(), Handle{ID: "probe"}, quota); (err == nil) != (wrong == "") {
				t.Fatalf("unverified limit accepted: %v", err)
			}
		})
	}
}

func TestQuotaProbeGCNeverDeletesPersonalDataEvenWithProbeTag(t *testing.T) {
	lease := personalDockerRequest("personal-owner", "lease").Lease
	lease.ExpiresAt = time.Now().Add(-time.Minute)
	data, err := json.Marshal(lease)
	if err != nil {
		t.Fatal(err)
	}
	name := dockerPersonalVolume(lease.OwnerID)
	executor, err := NewDockerExecutor(testPersonalImage, testQuotaImage)
	if err != nil {
		t.Fatal(err)
	}
	executor.run = func(_ context.Context, args ...string) ([]byte, error) {
		if args[0] != "volume" {
			t.Fatal("probe cleanup reached personal container or helper")
		}
		switch args[1] {
		case "ls":
			return []byte(name + " " + lease.ExpiresAt.UTC().Format(time.RFC3339Nano)), nil
		case "inspect":
			return json.Marshal(map[string]string{personalProbeLabel: lease.ID, dockerEnvironmentLabel + ".lease": base64.RawURLEncoding.EncodeToString(data)})
		default:
			t.Fatal("probe cleanup tried to remove personal data")
			return nil, nil
		}
	}
	if err := executor.gcPersonalProbes(t.Context(), time.Now()); err == nil {
		t.Fatal("personal data accepted as disposable probe")
	}
}
