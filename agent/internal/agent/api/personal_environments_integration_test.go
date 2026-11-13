package api

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	contractenv "github.com/opensoha/soha-contracts/agent/environment"
	"go.uber.org/zap"
)

func TestPersonalEnvironmentHTTPReal(t *testing.T) {
	if os.Getenv("SOHA_TEST_PERSONAL_DOCKER") != "1" {
		t.Skip("requires owned disposable Docker validation")
	}
	cfg := personalTestConfig()
	cfg.Security.AllowedActions = []string{actionPersonalEnvironmentAcquire, actionPersonalEnvironmentInspect, actionPersonalEnvironmentExecute, actionPersonalEnvironmentRelease, actionPersonalEnvironmentDeleteData, actionPersonalEnvironmentExportData}
	request := personalTestRequest("acquire")
	request.CreateData = true
	request.Lease.ID, request.Lease.RunID, request.Lease.OwnerID = uuid.NewString(), uuid.NewString(), "task48-http-"+uuid.NewString()
	ownerHash := sha256.Sum256([]byte(request.Lease.OwnerID))
	volume := "soha-env-data-" + hex.EncodeToString(ownerHash[:])[:40]
	server := New(cfg, zap.NewNop(), nil, nil)
	firstTaskID := uuid.NewString()
	call := func(input contractenv.Request, status int) contractenv.Result {
		t.Helper()
		data, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(http.MethodPost, "/api/v1/runtime/personal-environments", strings.NewReader(string(data)))
		r.Header.Set("Authorization", "Bearer personal-fixture-token")
		response := httptest.NewRecorder()
		server.httpServer.Handler.ServeHTTP(response, r)
		if response.Code != status {
			t.Fatalf("action %s returned %d: %s", input.Action, response.Code, response.Body.String())
		}
		if status != http.StatusOK {
			return contractenv.Result{}
		}
		var envelope struct {
			Data contractenv.Result `json:"data"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		if err := envelope.Data.ValidateFor(input); err != nil {
			t.Fatal(err)
		}
		return envelope.Data
	}
	t.Cleanup(func() {
		request.Action, request.CreateData, request.Argv = "release", false, nil
		call(request, http.StatusOK)
		// Exact fixture volume and immutable owner labels are checked before cleanup.
		data := personalFixtureDocker(t, "volume", "inspect", "--format", "{{json .Labels}}", volume)
		var labels map[string]string
		if json.Unmarshal(data, &labels) != nil || labels["soha.environment"] != "personal-docker-v1" || labels["soha.environment.owner"] != hex.EncodeToString(ownerHash[:]) {
			t.Fatal("refusing unrelated volume cleanup")
		}
		personalFixtureDocker(t, "run", "--rm", "--network", "none", "--read-only", "--cap-drop", "ALL", "--cap-add", "SYS_ADMIN", "--cap-add", "DAC_OVERRIDE", "--security-opt", "no-new-privileges", "--memory", "64m", "--memory-swap", "64m", "--cpus", "0.25", "--pids-limit", "16", "--mount", "type=volume,src="+volume+",dst=/probe", "--entrypoint", "/bin/sh", personalTestHelper, "-ec", "if [ -e /probe/data ]; then btrfs subvolume delete /probe/data >/dev/null; fi")
		personalFixtureDocker(t, "volume", "rm", volume)
	})
	t.Run("provision_and_execute", func(t *testing.T) {
		result := call(request, http.StatusOK)
		if result.Snapshot == nil || result.Snapshot.ResourceUsage.DiskBytes <= 0 {
			t.Fatal("missing actual quota observation")
		}
		request.Action, request.CreateData, request.Argv = "execute", false, []string{"/bin/sh", "-ec", "printf persisted-http > /data/note"}
		call(request, http.StatusOK)
	})
	t.Run("task_workspace", func(t *testing.T) {
		task := request
		task.TaskID = firstTaskID
		task.Argv = []string{"/bin/sh", "-ec", "printf task-a > scratch; pwd"}
		a := call(task, http.StatusOK)
		task.TaskID = uuid.NewString()
		task.Argv = []string{"/bin/sh", "-ec", "test ! -e scratch; printf task-b > scratch; pwd"}
		b := call(task, http.StatusOK)
		if a.Output == nil || b.Output == nil || *a.Output == *b.Output || !strings.HasPrefix(*a.Output, "/workspace/task-") || !strings.HasPrefix(*b.Output, "/workspace/task-") {
			t.Fatal("tasks shared or escaped a temporary working directory")
		}
		task.TaskID, task.Argv = firstTaskID, []string{"/bin/cat", "scratch"}
		if result := call(task, http.StatusOK); result.Output == nil || *result.Output != "task-a" {
			t.Fatal("another task overwrote the original temporary file")
		}
	})
	t.Run("restart_same_original_lease", func(t *testing.T) {
		server = New(cfg, zap.NewNop(), nil, nil)
		request.Action, request.Argv = "acquire", nil
		call(request, http.StatusOK)
		request.Action, request.Argv = "execute", []string{"/bin/cat", "/data/note"}
		if result := call(request, http.StatusOK); result.Output == nil || *result.Output != "persisted-http" {
			t.Fatal("restart lost data")
		}
	})
	t.Run("sleep_and_new_lease_keep_data", func(t *testing.T) {
		request.Action, request.Argv = "release", nil
		call(request, http.StatusOK)
		if result := call(request, http.StatusOK); result.State != "missing" {
			t.Fatal("missing receipt fabricated a released observation")
		}
		request.Lease.ID, request.Lease.RunID = uuid.NewString(), uuid.NewString()
		request.Action = "acquire"
		call(request, http.StatusOK)
		request.Action, request.Argv = "execute", []string{"/bin/cat", "/data/note"}
		if result := call(request, http.StatusOK); result.Output == nil || *result.Output != "persisted-http" {
			t.Fatal("wake lost data")
		}
		task := request
		task.TaskID, task.Argv = firstTaskID, []string{"/bin/sh", "-ec", "test ! -e scratch; cat /data/note"}
		if result := call(task, http.StatusOK); result.Output == nil || *result.Output != "persisted-http" {
			t.Fatal("sleep retained temporary files or lost persistent data")
		}
	})
	t.Run("sleep_export_verified_stream", func(t *testing.T) {
		request.Action, request.Argv = "release", nil
		call(request, http.StatusOK)
		export := request
		export.Action = "export_data"
		data, err := json.Marshal(export)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(http.MethodPost, "/api/v1/runtime/personal-environments/export", bytes.NewReader(data))
		r.Header.Set("Authorization", "Bearer personal-fixture-token")
		response := httptest.NewRecorder()
		server.httpServer.Handler.ServeHTTP(response, r)
		var archive bytes.Buffer
		if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("export stream failed or became cacheable")
		}
		if _, err := contractenv.ReadExport(response.Body, &archive, export); err != nil {
			t.Fatal(err)
		}
		reader := tar.NewReader(&archive)
		found := false
		for {
			entry, err := reader.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			if entry.Name == "./note" {
				content, err := io.ReadAll(reader)
				found = err == nil && string(content) == "persisted-http"
			}
		}
		if !found {
			t.Fatal("export lost the retained personal file")
		}
		request.Action = "acquire"
		call(request, http.StatusOK)
		request.Action = "execute"
	})
	t.Run("task_workspace_symlink_rejected", func(t *testing.T) {
		task := request
		task.TaskID = uuid.NewString()
		hash := sha256.Sum256([]byte(task.TaskID))
		path := "/workspace/task-" + hex.EncodeToString(hash[:])[:40]
		request.Argv = []string{"/bin/sh", "-ec", `ln -s /data "$1"`, "fixture-symlink", path}
		call(request, http.StatusOK)
		task.Argv = []string{"/bin/true"}
		call(task, http.StatusBadGateway)
		request.Action, request.Argv = "inspect", nil
		if result := call(request, http.StatusOK); result.State != "missing" {
			t.Fatal("rejected task workspace retained its sandbox")
		}
	})
	t.Run("missing_data_is_not_recreated", func(t *testing.T) {
		request.Action, request.Argv = "release", nil
		call(request, http.StatusOK)
		personalFixtureDocker(t, "run", "--rm", "--network", "none", "--read-only", "--cap-drop", "ALL", "--cap-add", "SYS_ADMIN", "--cap-add", "DAC_OVERRIDE", "--security-opt", "no-new-privileges", "--memory", "64m", "--memory-swap", "64m", "--cpus", "0.25", "--pids-limit", "16", "--mount", "type=volume,src="+volume+",dst=/probe", "--entrypoint", "btrfs", personalTestHelper, "subvolume", "delete", "/probe/data")
		request.Lease.ID, request.Lease.RunID = uuid.NewString(), uuid.NewString()
		request.Action = "acquire"
		call(request, http.StatusBadGateway)
		personalFixtureDocker(t, "run", "--rm", "--network", "none", "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--memory", "32m", "--memory-swap", "32m", "--cpus", "0.25", "--pids-limit", "16", "--mount", "type=volume,src="+volume+",dst=/probe", "--entrypoint", "/bin/sh", personalTestImage, "-ec", "test ! -e /probe/data")
	})
	t.Run("explicit_data_deletion_is_not_recreated", func(t *testing.T) {
		request.Action, request.CreateData, request.Argv, request.TaskID = "delete_data", false, nil, ""
		if result := call(request, http.StatusOK); result.State != "deleted" || result.Snapshot != nil {
			t.Fatal("deletion invented a runtime observation")
		}
		server = New(cfg, zap.NewNop(), nil, nil)
		request.Action = "inspect"
		if result := call(request, http.StatusOK); result.State != "deleted" {
			t.Fatal("Agent restart forgot deletion")
		}
		request.Action, request.CreateData = "acquire", true
		call(request, http.StatusBadGateway)
	})
}

func personalFixtureDocker(t *testing.T, args ...string) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput() //nolint:gosec // Test-defined CLI argv targets only the dedicated fixture lab.
	if err != nil {
		t.Fatalf("fixture Docker operation failed: %v", err)
	}
	return output
}
