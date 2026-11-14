package environment

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestPersonalDockerEnvironmentReal(t *testing.T) {
	if os.Getenv("SOHA_TEST_PERSONAL_DOCKER") != "1" {
		t.Skip("set SOHA_TEST_PERSONAL_DOCKER=1 for owned disposable Docker validation")
	}
	executor, err := NewDockerExecutor(testPersonalImage, testQuotaImage)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	requests := []LocalRequest{personalDockerRequest("task48-a-"+uuid.NewString(), "lease-"+uuid.NewString()), personalDockerRequest("task48-b-"+uuid.NewString(), "lease-"+uuid.NewString())}
	for i := range requests {
		requests[i].Spec.Workspace = filepath.Join(root, requests[i].Lease.ID)
	}
	handles := []Handle{}
	t.Cleanup(func() { cleanupPersonalDockerFixtures(t, executor, requests, handles) })
	manager := personalDockerManager(t, executor, root)
	acquire := func(request LocalRequest) Handle {
		t.Helper()
		handle, err := manager.Acquire(t.Context(), request.Lease, request.Spec, time.Now().UTC())
		if err != nil {
			t.Fatal(err)
		}
		handles = append(handles, handle)
		return handle
	}
	a, b := acquire(requests[0]), acquire(requests[1])
	t.Run("owner isolation", func(t *testing.T) {
		personalDockerCommand(t, executor, a, "/bin/sh", "-ec", "printf a-persisted > /data/owner-a")
		personalDockerCommand(t, executor, b, "/bin/sh", "-ec", "printf b-persisted > /data/owner-b; test ! -e /data/owner-a")
		if got := personalDockerCommand(t, executor, a, "/bin/cat", "/data/owner-a"); got != "a-persisted" {
			t.Fatal("wrong owner's data")
		}
	})
	t.Run("sandbox boundaries", func(t *testing.T) {
		if got := personalDockerCommand(t, executor, a, "/bin/id", "-u"); strings.TrimSpace(got) != "1000" {
			t.Fatalf("task command is not non-root: %q", got)
		}
		personalDockerCommand(t, executor, a, "/bin/sh", "-ec", "test ! -S /var/run/docker.sock; test ! -e /var/lib/docker/volumes; if kill -STOP 1 2>/dev/null; then exit 1; fi")
		data, err := executor.run(t.Context(), "container", "inspect", "--format", "{{.HostConfig.NetworkMode}} {{.HostConfig.ReadonlyRootfs}} {{.HostConfig.Memory}} {{.HostConfig.MemorySwap}} {{.HostConfig.NanoCpus}} {{.HostConfig.PidsLimit}}", a.ID)
		if err != nil || strings.TrimSpace(string(data)) != "none true 67108864 67108864 250000000 16" {
			t.Fatalf("hard sandbox limits differ: %q %v", data, err)
		}
	})
	t.Run("actual usage", func(t *testing.T) {
		usage, refs, err := executor.Inspect(t.Context(), a)
		if err != nil || usage.DiskBytes <= 0 || usage.MemoryBytes <= 0 || usage.Processes <= 0 || usage.CPUTimeNanos <= 0 || len(refs) != 0 {
			t.Fatalf("usage was fabricated or unavailable: %+v %v", usage, err)
		}
	})
	t.Run("bounded stdout and stderr", func(t *testing.T) {
		got := personalDockerCommand(t, executor, a, "/bin/sh", "-ec", "printf out; printf diagnostic >&2")
		if got != "out\n[stderr]\ndiagnostic" {
			t.Fatal("successful command lost its bounded diagnostic output")
		}
	})
	t.Run("Agent restart recovery", func(t *testing.T) {
		restarted, err := NewDockerExecutor(testPersonalImage, testQuotaImage)
		if err != nil {
			t.Fatal(err)
		}
		recoveredManager := personalDockerManager(t, restarted, root)
		recovered, err := recoveredManager.Acquire(t.Context(), requests[0].Lease, requests[0].Spec, time.Now().UTC())
		if err != nil || recovered != a {
			t.Fatalf("restart replaced the original lease: %+v %v", recovered, err)
		}
		if got := personalDockerCommand(t, restarted, recovered, "/bin/cat", "/data/owner-a"); got != "a-persisted" {
			t.Fatal("restart lost data")
		}
	})
	t.Run("read-only persistent mount", func(t *testing.T) {
		read := requests[0]
		read.Lease.ID, read.Lease.RunID, read.Lease.Mode = "read-"+uuid.NewString(), "read-run", "observe-only"
		read.Spec.Workspace = filepath.Join(root, read.Lease.ID)
		handle := acquire(read)
		personalDockerCommand(t, executor, handle, "/bin/sh", "-ec", "test \"$(cat /data/owner-a)\" = a-persisted; if printf forbidden 2>/dev/null > /data/forbidden; then exit 1; fi")
		if err := executor.Stop(t.Context(), handle); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("hard disk quota and recovery", func(t *testing.T) {
		if _, err := executor.Execute(t.Context(), a, []string{"/bin/dd", "if=/dev/urandom", "of=/data/overflow", "bs=1M", "count=48"}); err == nil {
			t.Fatal("persistent quota was not hard enforced")
		}
		if _, found, err := executor.containerRecord(t.Context(), a.ID); err != nil || found {
			t.Fatalf("failed command retained its task process: found=%v err=%v", found, err)
		}
		fresh := requests[0]
		fresh.Lease.ID, fresh.Lease.RunID = "quota-recovery-"+uuid.NewString(), "quota-recovery"
		fresh.Spec.Workspace = filepath.Join(root, fresh.Lease.ID)
		a = acquire(fresh)
		size, err := strconv.ParseInt(strings.TrimSpace(personalDockerCommand(t, executor, a, "/bin/stat", "-c", "%s", "/data/overflow")), 10, 64)
		if err != nil || size < 30<<20 || size > requests[0].Lease.Quota.DiskBytes {
			t.Fatalf("failure did not occur at the storage ceiling: %d %v", size, err)
		}
		personalDockerCommand(t, executor, a, "/bin/rm", "/data/overflow")
		if got := personalDockerCommand(t, executor, a, "/bin/cat", "/data/owner-a"); got != "a-persisted" {
			t.Fatal("quota recovery lost persistent data")
		}
	})
	t.Run("cross-Agent command fencing and cancellation", func(t *testing.T) { personalDockerCancelAndFence(t, executor, a) })
	t.Run("persistent data after cancellation", func(t *testing.T) {
		fresh := requests[0]
		fresh.Lease.ID, fresh.Lease.RunID = "after-cancel-"+uuid.NewString(), "after-cancel"
		fresh.Spec.Workspace = filepath.Join(root, fresh.Lease.ID)
		a = acquire(fresh)
		if got := personalDockerCommand(t, executor, a, "/bin/cat", "/data/owner-a"); got != "a-persisted" {
			t.Fatal("cancellation deleted persistent data")
		}
	})
	t.Run("different tasks cannot share a writer", func(t *testing.T) {
		other := requests[0]
		other.Lease.ID, other.Lease.RunID = "different-task-"+uuid.NewString(), "different-task"
		if _, err := executor.Start(t.Context(), other); err == nil {
			t.Fatal("different tasks acquired the same persistent writer")
		}
		if got := personalDockerCommand(t, executor, a, "/bin/cat", "/data/owner-a"); got != "a-persisted" {
			t.Fatal("conflicting task changed the original data")
		}
	})
	t.Run("binary output rejected", func(t *testing.T) {
		if _, err := executor.Execute(t.Context(), a, []string{"/bin/sh", "-ec", `printf '\377'`}); err == nil {
			t.Fatal("binary output would be silently repaired by JSON")
		}
		if _, found, err := executor.containerRecord(t.Context(), a.ID); err != nil || found {
			t.Fatal("unsupported command output retained its task sandbox")
		}
		fresh := requests[0]
		fresh.Lease.ID, fresh.Lease.RunID = "after-binary-"+uuid.NewString(), "after-binary"
		fresh.Spec.Workspace = filepath.Join(root, fresh.Lease.ID)
		a = acquire(fresh)
		if got := personalDockerCommand(t, executor, a, "/bin/cat", "/data/owner-a"); got != "a-persisted" {
			t.Fatal("unsupported output cleanup deleted personal data")
		}
	})
	t.Run("background processes rejected", func(t *testing.T) {
		if _, err := executor.Execute(t.Context(), a, []string{"/bin/sh", "-ec", "printf kept > /data/before-background; sleep 60 >/dev/null 2>&1 &"}); err == nil {
			t.Fatal("background task was reported complete")
		}
		if _, found, err := executor.containerRecord(t.Context(), a.ID); err != nil || found {
			t.Fatal("background processes retained the task sandbox")
		}
		fresh := requests[0]
		fresh.Lease.ID, fresh.Lease.RunID = "after-background-"+uuid.NewString(), "after-background"
		fresh.Spec.Workspace = filepath.Join(root, fresh.Lease.ID)
		a = acquire(fresh)
		if got := personalDockerCommand(t, executor, a, "/bin/cat", "/data/before-background"); got != "kept" {
			t.Fatal("stopping background processes deleted persistent data")
		}
	})
	t.Run("lease timer and recovery GC", func(t *testing.T) {
		if err := executor.Stop(t.Context(), b); err != nil {
			t.Fatal(err)
		}
		expiring := requests[1]
		expiring.Lease.ID, expiring.Lease.RunID = "expiring-"+uuid.NewString(), "expiring"
		expiring.Lease.ExpiresAt = time.Now().UTC().Add(7 * time.Second)
		expiring.Spec.Workspace = filepath.Join(root, expiring.Lease.ID)
		b = acquire(expiring)
		// Inject a pre-existing orphan directly; current controlled Execute refuses
		// background processes. The original lease timer must still stop it.
		if _, err := executor.run(t.Context(), "exec", "--user", "1000:1000", b.ID, "/bin/sh", "-ec", "sleep 20 >/dev/null 2>&1 &"); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Until(expiring.Lease.ExpiresAt) + time.Second)
		if err := executor.requireRunning(t.Context(), b.ID); !errors.Is(err, ErrLeaseExpired) {
			t.Fatalf("lease timer did not stop its sandbox: %v", err)
		}
		assertOnlyPersonalDockerFixtures(t, executor, requests)
		restarted, err := NewDockerExecutor(testPersonalImage, testQuotaImage)
		if err != nil {
			t.Fatal(err)
		}
		removed, err := restarted.GC(t.Context(), time.Now().UTC())
		if err != nil || len(removed) != 1 || removed[0].ID != expiring.Lease.ID {
			t.Fatalf("restart GC lost the expired receipt: %+v %v", removed, err)
		}
		if _, found, err := restarted.containerRecord(t.Context(), b.ID); err != nil || found {
			t.Fatalf("expired sandbox was not cleaned: %v %v", found, err)
		}
		fresh := requests[1]
		fresh.Lease.ID, fresh.Lease.RunID = "after-expiry-"+uuid.NewString(), "after-expiry"
		fresh.Spec.Workspace = filepath.Join(root, fresh.Lease.ID)
		b = acquire(fresh)
		if got := personalDockerCommand(t, executor, b, "/bin/cat", "/data/owner-b"); got != "b-persisted" {
			t.Fatal("lease expiry or recovery GC deleted persistent data")
		}
	})
}

func assertOnlyPersonalDockerFixtures(t *testing.T, executor *DockerExecutor, requests []LocalRequest) {
	t.Helper()
	ids, err := executor.run(t.Context(), "container", "ls", "--all", "--no-trunc", "--filter", "label="+dockerEnvironmentLabel+"="+dockerEnvironmentBackend, "--format", "{{.ID}}")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range strings.Fields(string(ids)) {
		record, found, err := executor.containerRecord(t.Context(), id)
		if err != nil || !found {
			t.Fatal("fixture ownership inventory failed")
		}
		lease, err := dockerLeaseFromLabels(record.Labels)
		if err != nil || (lease.OwnerID != requests[0].Lease.OwnerID && lease.OwnerID != requests[1].Lease.OwnerID) {
			t.Fatal("refusing GC: a non-fixture personal environment exists")
		}
	}
}

func personalDockerManager(t *testing.T, executor *DockerExecutor, root string) *RuntimeManager {
	t.Helper()
	backend, err := NewLocalBackend(BackendLocalContainer, executor)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewRuntimeManager(RuntimeOptions{AllowedWorkspaceRoot: root}, backend)
	if err != nil {
		t.Fatal(err)
	}
	return manager
}

func personalDockerCommand(t *testing.T, executor *DockerExecutor, handle Handle, argv ...string) string {
	t.Helper()
	output, err := executor.Execute(t.Context(), handle, argv)
	if err != nil {
		t.Fatalf("owned sandbox command failed: %v", err)
	}
	return string(output)
}

func personalDockerCancelAndFence(t *testing.T, executor *DockerExecutor, handle Handle) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := executor.Execute(ctx, handle, []string{"/bin/sh", "-ec", "printf started > /data/cancel-started; sleep 20"})
		result <- err
	}()
	deadline := time.Now().Add(10 * time.Second)
	started := false
	for time.Now().Before(deadline) {
		output, err := runEnvironmentDocker(t.Context(), "exec", "--user", "1000:1000", handle.ID, "/bin/cat", "/data/cancel-started")
		if err == nil && string(output) == "started" {
			started = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !started {
		cancel()
		t.Fatalf("sandbox command never actually started: %v", <-result)
	}
	other, err := NewDockerExecutor(testPersonalImage, testQuotaImage)
	if err != nil {
		cancel()
		<-result
		t.Fatal(err)
	}
	if _, err := other.Execute(t.Context(), handle, []string{"/bin/sh", "-ec", "printf duplicate > /data/duplicate"}); err == nil {
		cancel()
		<-result
		t.Fatal("another Agent overlapped the original command")
	}
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation did not stop the actual command: %v", err)
	}
	if _, found, err := other.containerRecord(t.Context(), handle.ID); err != nil || found {
		t.Fatalf("cancelled sandbox retained processes: %v %v", found, err)
	}
}

func cleanupPersonalDockerFixtures(t *testing.T, executor *DockerExecutor, requests []LocalRequest, handles []Handle) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	for _, handle := range handles {
		if err := executor.Stop(ctx, handle); err != nil {
			t.Errorf("owned container cleanup failed: %v", err)
		}
	}
	for _, request := range requests {
		volume := dockerPersonalVolume(request.Lease.OwnerID)
		ids, err := executor.run(ctx, "volume", "ls", "--filter", "label="+dockerEnvironmentLabel+".owner="+dockerHash(request.Lease.OwnerID), "--format", "{{.Name}}")
		if err != nil {
			t.Errorf("owned volume cleanup inventory failed: %v", err)
			continue
		}
		if !strings.Contains(string(ids), volume) {
			continue
		}
		if err := executor.verifyVolume(ctx, request.Lease); err != nil {
			t.Errorf("owned volume cleanup denied: %v", err)
			continue
		}
		if _, err := executor.quotaHelper(ctx, volume, "/bin/sh", "-ec", "if [ -e /probe/data ]; then btrfs subvolume delete /probe/data >/dev/null; fi"); err != nil {
			t.Errorf("owned subvolume cleanup failed: %v", err)
			continue
		}
		if _, err := executor.run(ctx, "volume", "rm", volume); err != nil {
			t.Errorf("owned volume cleanup failed: %v", err)
		}
	}
}
