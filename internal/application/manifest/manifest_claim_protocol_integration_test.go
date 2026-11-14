package manifest

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	resourceruntime "github.com/opensoha/soha/contracts/resource/runtime"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	dd "github.com/opensoha/soha/internal/domain/delivery"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	agentinfra "github.com/opensoha/soha/internal/infrastructure/agent"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestManifestSummaryProtocolOverHTTP(t *testing.T) {
	binary := summaryProtocolBinary(t)
	for _, tc := range []struct {
		name                     string
		manifest, helm, disabled bool
		wantQueue                bool
	}{
		{name: "ManifestOnly", manifest: true, wantQueue: true},
		{name: "HelmOnly", helm: true},
		{name: "Combined", manifest: true, helm: true, wantQueue: true},
		{name: "Disabled"},
		{name: "ControlPlaneDisabled", manifest: true, helm: true, disabled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			endpoint := startSummaryProtocolPeer(t, binary, tc.manifest, tc.helm, !tc.disabled)
			registry := agentinfra.NewRegistry(3 * time.Second)
			defer func() {
				if err := registry.Close(); err != nil {
					t.Error(err)
				}
			}()
			client, err := registry.ClientFor(domaincluster.Connection{
				Summary:  domaincluster.Summary{ID: "one", ConnectionMode: domaincluster.ConnectionModeAgent},
				Metadata: map[string]any{"endpoint": endpoint, "token": "cluster-token"},
			})
			if err != nil {
				t.Fatal(err)
			}
			observed, err := client.GetSummary(t.Context())
			if err != nil || observed.ID != "one" || observed.Health.Status != "healthy" {
				t.Fatalf("real Agent summary = %+v, error = %v", observed, err)
			}
			store := &protocolOperationRepository{}
			service := &DeclarativeService{
				base:       &Service{clusters: manifestCapabilityCluster{observed}},
				repository: store, tasks: &localTaskFixture{},
			}
			task, err := service.queueOperation(t.Context(), domainmanifest.Package{ID: "package"},
				domainmanifest.EnvironmentBinding{ID: "binding", ClusterID: "one"}, domainmanifest.Deployment{},
				domainmanifest.TaskPayload{Action: domainmanifest.TaskActionPreflight, PackageID: "package", Generation: 1, IdempotencyKey: "operation"})
			if !tc.wantQueue {
				if !errors.Is(err, apperrors.ErrUnsupportedOperation) || len(store.tasks) != 0 || task.ID != "" {
					t.Fatalf("unavailable runner queued task: %+v, error = %v, writes = %d", task, err, len(store.tasks))
				}
				return
			}
			wantProvider := resourceruntime.ManifestAgentProviderPrefix + observed.ID
			if err != nil || task.ProviderKind != wantProvider || len(store.tasks) != 1 {
				t.Fatalf("queued provider = %q, error = %v, writes = %d", task.ProviderKind, err, len(store.tasks))
			}
		})
	}
}

type protocolOperationRepository struct {
	DeclarativeRepository
	tasks []dd.ExecutionTask
}

func (r *protocolOperationRepository) CreateOperationTask(_ context.Context, _ domainmanifest.OperationRun, task dd.ExecutionTask) (string, bool, error) {
	r.tasks = append(r.tasks, task)
	return task.ID, true, nil
}

func summaryProtocolBinary(t *testing.T) string {
	t.Helper()
	binary := os.Getenv("SOHA_TEST_AGENT_BINARY")
	if binary == "" {
		t.Skip("set SOHA_TEST_AGENT_BINARY to the precompiled Agent runner test binary")
	}
	// #nosec G703 -- the local test driver supplies this binary path, validated as absolute, regular and executable below.
	info, err := os.Stat(binary)
	if err != nil || !filepath.IsAbs(binary) {
		t.Fatalf("Agent binary must be an existing absolute path: %q (%v)", binary, err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("Agent binary is not an executable regular file: %q", binary)
	}
	return binary
}

func summaryProtocolFlag(key string, enabled bool) string {
	if enabled {
		return key + "=1"
	}
	return key + "=0"
}

func startSummaryProtocolPeer(t *testing.T, binary string, manifest, helm, enabled bool) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	ready := filepath.Join(t.TempDir(), "summary-endpoint")
	// #nosec G204 -- only the explicitly supplied, validated absolute test binary is executed.
	cmd := exec.CommandContext(ctx, binary, "-test.run=^TestManifestClaimProtocolPeer$", "-test.timeout=15s")
	cmd.Env = append(os.Environ(), "SOHA_TEST_PROTOCOL_MODE=summary", "SOHA_TEST_CORE_URL=http://unused",
		"SOHA_TEST_CLUSTER_ID=one", "SOHA_TEST_REPORT="+ready,
		summaryProtocolFlag("SOHA_TEST_MANIFEST", manifest), summaryProtocolFlag("SOHA_TEST_HELM", helm),
		summaryProtocolFlag("SOHA_TEST_CONTROL_PLANE", enabled))
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		t.Fatal(err)
	}
	done := make(chan struct{})
	var waitErr error
	go func() {
		waitErr = cmd.Wait()
		close(done)
	}()
	t.Cleanup(func() {
		if err := stdin.Close(); err != nil {
			t.Error(err)
		}
		<-done
		if waitErr != nil {
			t.Errorf("Agent summary peer failed: %v\n%s", waitErr, output.String())
		}
	})
	poll := time.NewTicker(10 * time.Millisecond)
	defer poll.Stop()
	for {
		// #nosec G304 -- this endpoint file uses the current test's t.TempDir and a fixed filename.
		if raw, err := os.ReadFile(ready); err == nil {
			return string(raw)
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		select {
		case <-done:
			t.Fatalf("Agent summary peer exited before readiness: %v\n%s", waitErr, output.String())
		case <-ctx.Done():
			t.Fatal("Agent summary peer readiness timed out")
		case <-poll.C:
		}
	}
}
