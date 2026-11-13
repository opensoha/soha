package environment

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	contractenv "github.com/opensoha/soha-contracts/agent/environment"
)

func TestPersonalDockerPreflightReal(t *testing.T) {
	if os.Getenv("SOHA_TEST_PERSONAL_DOCKER") != "1" {
		t.Skip("requires owned disposable Docker validation")
	}
	executor, err := NewDockerExecutor(testPersonalImage, testQuotaImage)
	if err != nil {
		t.Fatal(err)
	}
	request := contractenv.PreflightRequest{SchemaVersion: contractenv.PreflightRequestVersion, ID: uuid.NewString(), Quota: contractenv.Quota{DiskBytes: 32 << 20, MemoryBytes: 64 << 20, CPUUnits: 250, MaxProcesses: 16}}
	t.Run("verified_limits_and_cleanup", func(t *testing.T) {
		result, err := executor.Preflight(t.Context(), request)
		if err != nil || result.ValidateFor(request) != nil {
			t.Fatalf("quota preflight: %+v %v", result, err)
		}
		names, err := executor.run(t.Context(), "volume", "ls", "--filter", "name="+dockerPersonalVolume(personalProbeOwnerPrefix+request.ID), "--format", "{{.Name}}")
		if err != nil || strings.TrimSpace(string(names)) != "" {
			t.Fatalf("successful preflight left probe data: %v", err)
		}
	})
	t.Run("restart_gc_only_probe_volume", func(t *testing.T) {
		lease := personalDockerRequest(personalProbeOwnerPrefix+uuid.NewString(), uuid.NewString()).Lease
		lease.ID = strings.TrimPrefix(lease.OwnerID, personalProbeOwnerPrefix)
		lease.RunID, lease.IssuedAt, lease.ExpiresAt = lease.ID, time.Now().Add(-2*time.Minute), time.Now().Add(-time.Minute)
		if err := executor.createPersonalProbeVolume(t.Context(), lease); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			cleanup, cancel := context.WithTimeout(context.Background(), defaultOperationTimeout)
			defer cancel()
			if err := executor.cleanupPersonalProbe(cleanup, lease); err != nil {
				t.Error(err)
			}
		})
		if _, err := executor.ensureVolume(t.Context(), lease, true); err != nil {
			t.Fatal(err)
		}
		restarted, err := NewDockerExecutor(testPersonalImage, testQuotaImage)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := restarted.GC(t.Context(), time.Now()); err != nil {
			t.Fatal(err)
		}
		names, err := executor.run(t.Context(), "volume", "ls", "--filter", "name="+dockerPersonalVolume(lease.OwnerID), "--format", "{{.Name}}")
		if err != nil || strings.TrimSpace(string(names)) != "" {
			t.Fatalf("restart left expired probe: %v", err)
		}
	})
	t.Run("cleanup_failure_never_ready", func(t *testing.T) {
		request.ID = uuid.NewString()
		volume := dockerPersonalVolume(personalProbeOwnerPrefix + request.ID)
		normalRun := executor.run
		executor.run = func(ctx context.Context, args ...string) ([]byte, error) {
			if args[0] == "volume" && args[1] == "rm" {
				return nil, errors.New("fixture lost cleanup receipt")
			}
			return normalRun(ctx, args...)
		}
		t.Cleanup(func() {
			executor.run = normalRun
			cleanup, cancel := context.WithTimeout(context.Background(), defaultOperationTimeout)
			defer cancel()
			data, err := executor.run(cleanup, "volume", "inspect", "--format", "{{json .Labels}}", volume)
			if err != nil {
				t.Error(err)
				return
			}
			var labels map[string]string
			if err := json.Unmarshal(data, &labels); err != nil {
				t.Error(err)
				return
			}
			lease, err := dockerLeaseFromLabels(labels)
			if err != nil {
				t.Error(err)
				return
			}
			if err := executor.cleanupPersonalProbe(cleanup, lease); err != nil {
				t.Error(err)
			}
		})
		result, err := executor.Preflight(t.Context(), request)
		if err == nil || result.Ready || result.ID != "" {
			t.Fatalf("unconfirmed cleanup reported readiness: %+v %v", result, err)
		}
	})
}
