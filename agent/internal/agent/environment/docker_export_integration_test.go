package environment

import (
	"archive/tar"
	"bytes"
	"errors"
	"io"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	contractenv "github.com/opensoha/soha-contracts/agent/environment"
)

func TestPersonalDockerDataExportReal(t *testing.T) {
	if os.Getenv("SOHA_TEST_PERSONAL_DOCKER") != "1" {
		t.Skip("requires owned disposable Docker validation")
	}
	executor, err := NewDockerExecutor(testPersonalImage, testQuotaImage)
	if err != nil {
		t.Fatal(err)
	}
	local := personalDockerRequest("task48-export-"+uuid.NewString(), "export-"+uuid.NewString())
	handle, err := executor.Start(t.Context(), local)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupPersonalDockerFixtures(t, executor, []LocalRequest{local}, []Handle{handle}) })
	personalDockerCommand(t, executor, handle, "/bin/sh", "-ec", "printf retained > /data/note; mkdir /data/nested; printf nested > /data/nested/file; ln -s /etc/passwd /data/outside")
	l := local.Lease
	request := contractenv.Request{SchemaVersion: contractenv.RequestVersion, Action: "export_data", Lease: contractenv.Lease{SchemaVersion: contractenv.LeaseVersion, ID: l.ID, RunID: l.RunID, OwnerID: l.OwnerID, ScopeHash: l.ScopeHash, Mode: l.Mode, IssuedAt: l.IssuedAt, ExpiresAt: l.ExpiresAt, Quota: contractenv.Quota{DiskBytes: l.Quota.DiskBytes, MemoryBytes: l.Quota.MemoryBytes, CPUUnits: l.Quota.CPUUnits, MaxProcesses: l.Quota.MaxProcesses}}}
	t.Run("live_environment_rejected", func(t *testing.T) {
		var archive bytes.Buffer
		if err := executor.ExportPersonalData(t.Context(), request, &archive); err == nil || archive.Len() != 0 {
			t.Fatal("active writer exported its changing data")
		}
	})
	if err := executor.Stop(t.Context(), handle); err != nil {
		t.Fatal(err)
	}
	t.Run("complete_tree_and_links_without_targets", func(t *testing.T) {
		assertPersonalArchive(t, executor, request)
	})
	t.Run("foreign_owner_rejected", func(t *testing.T) {
		foreign := request
		foreign.Lease.OwnerID = "foreign-" + uuid.NewString()
		if err := executor.ExportPersonalData(t.Context(), foreign, &bytes.Buffer{}); err == nil {
			t.Fatal("export borrowed another owner's volume")
		}
	})
	t.Run("interrupted_transfer_retains_data", func(t *testing.T) {
		if err := executor.ExportPersonalData(t.Context(), request, closedExportWriter{}); err == nil {
			t.Fatal("failed transfer became complete")
		}
		assertPersonalArchive(t, executor, request)
	})
	t.Run("cross_Agent_export_lock_blocks_late_start", func(t *testing.T) {
		handle, err = executor.Start(t.Context(), local)
		if err != nil {
			t.Fatal(err)
		}
		personalDockerCommand(t, executor, handle, "/bin/sh", "-ec", "dd if=/dev/zero of=/data/export-large bs=1048576 count=24 >/dev/null 2>&1")
		if err := executor.Stop(t.Context(), handle); err != nil {
			t.Fatal(err)
		}
		peer, err := NewDockerExecutor(testPersonalImage, testQuotaImage)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := peer.ensureVolume(t.Context(), local.Lease, false); err != nil {
			t.Fatal(err)
		}
		target := &pausedExportWriter{started: make(chan struct{}), resume: make(chan struct{})}
		completed := make(chan error, 1)
		waited := false
		defer func() {
			target.release()
			if !waited {
				<-completed
			}
		}()
		go func() { completed <- executor.ExportPersonalData(t.Context(), request, target) }()
		select {
		case <-target.started:
		case <-time.After(10 * time.Second):
			t.Fatal("export did not reach its held stream")
		}
		name, ref := dockerLeaseIdentity(local.Lease)
		// The creation check already happened before the export took its lock.
		if _, err := peer.run(t.Context(), peer.containerArgs(local.Lease, name, ref, dockerPersonalVolume(local.Lease.OwnerID))...); err != nil {
			t.Fatal(err)
		}
		late, found, err := peer.personalHandle(t.Context(), local.Lease)
		if err != nil || !found {
			t.Fatal("late fixture container identity unavailable")
		}
		defer func() {
			if err := peer.Stop(t.Context(), late); err != nil {
				t.Error("late fixture cleanup failed")
			}
		}()
		if err := peer.requireRunning(t.Context(), late.ID); err == nil {
			t.Fatal("late writer became ready while an export held the lock")
		}
		if err := peer.Stop(t.Context(), late); err != nil {
			t.Fatal(err)
		}
		target.release()
		err = <-completed
		waited = true
		if err != nil {
			t.Fatal(err)
		}
		handle, err = executor.Start(t.Context(), local)
		if err != nil {
			t.Fatal(err)
		}
		personalDockerCommand(t, executor, handle, "/bin/rm", "/data/export-large")
		if err := executor.Stop(t.Context(), handle); err != nil {
			t.Fatal(err)
		}
		assertPersonalArchive(t, executor, request)
	})
	t.Run("retired_data_rejected", func(t *testing.T) {
		if err := executor.DeletePersonalData(t.Context(), local.Lease); err != nil {
			t.Fatal(err)
		}
		if err := executor.ExportPersonalData(t.Context(), request, &bytes.Buffer{}); err == nil {
			t.Fatal("deleted data became a completed empty export")
		}
	})
}

type closedExportWriter struct{}

func (closedExportWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

type pausedExportWriter struct {
	started, resume        chan struct{}
	startOnce, releaseOnce sync.Once
}

func (w *pausedExportWriter) release() { w.releaseOnce.Do(func() { close(w.resume) }) }
func (w *pausedExportWriter) Write(data []byte) (int, error) {
	w.startOnce.Do(func() { close(w.started) })
	<-w.resume
	return len(data), nil
}

func assertPersonalArchive(t *testing.T, executor *DockerExecutor, request contractenv.Request) {
	t.Helper()
	var framed bytes.Buffer
	writer := contractenv.ExportWriter{Writer: &framed}
	if err := executor.ExportPersonalData(t.Context(), request, &writer); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Finish(request); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if _, err := contractenv.ReadExport(&framed, &archive, request); err != nil {
		t.Fatal(err)
	}
	reader := tar.NewReader(&archive)
	files := map[string]string{}
	link := false
	for {
		entry, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if entry.Typeflag == tar.TypeReg {
			data, err := io.ReadAll(reader)
			if err != nil {
				t.Fatal(err)
			}
			files[entry.Name] = string(data)
		}
		if entry.Name == "./outside" {
			link = entry.Typeflag == tar.TypeSymlink && entry.Linkname == "/etc/passwd"
		}
	}
	if len(files) != 2 || files["./note"] != "retained" || files["./nested/file"] != "nested" || !link {
		t.Fatalf("archive changed files or followed a link: %v", files)
	}
}
