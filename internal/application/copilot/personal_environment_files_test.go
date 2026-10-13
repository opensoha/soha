package copilot

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	contractenv "github.com/opensoha/soha-contracts/agent/environment"
	guest "github.com/opensoha/soha-contracts/agent/guest"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type personalFileReservationStore struct {
	*personalCleanupStore
	aborted bool
}

func (s *personalFileReservationStore) AbortPersonalEnvironment(_ context.Context, reserved, previous domain.PersonalEnvironmentRecord) (domain.PersonalEnvironmentRecord, error) {
	if reserved.OperationID == "" || reserved.OperationID != s.item.OperationID || reserved.Version != s.item.Version || previous.Version+1 != reserved.Version {
		return domain.PersonalEnvironmentRecord{}, apperrors.ErrConflict
	}
	s.aborted = true
	previous.Version = reserved.Version + 1
	s.item = previous
	return previous, nil
}

func TestPersonalVMFileReadDoesNotKeepAnUndispatchedReservation(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name      string
		invokeErr error
		wantErr   error
		abort     bool
	}{
		{"unavailable_before_dispatch", errors.Join(domain.ErrPersonalEnvironmentNotDispatched, apperrors.ErrServiceUnavailable), apperrors.ErrServiceUnavailable, true},
		{"revoked_before_dispatch", errors.Join(domain.ErrPersonalEnvironmentNotDispatched, apperrors.ErrAccessDenied), apperrors.ErrAccessDenied, true},
		{"unknown_after_dispatch", apperrors.ErrServiceUnavailable, apperrors.ErrServiceUnavailable, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			item := domain.PersonalEnvironmentRecord{AssistantID: uuid.NewString(), CreatedBy: uuid.NewString(), Backend: "vm", ComputeRef: uuid.NewString(), ComputeGeneration: 1, DataRef: uuid.NewString(), DataInitialized: true, Version: 5, State: "ready", Lease: contractenv.Lease{ID: uuid.NewString()}}
			store := &personalFileReservationStore{personalCleanupStore: &personalCleanupStore{item: item}}
			calls := 0
			service := &Service{personalEnvironment: &PersonalEnvironmentDependencies{Store: store, InvokeVM: func(_ context.Context, principal domainidentity.Principal, reserved domain.PersonalEnvironmentRecord, op guest.RuntimeOperation) (guest.RuntimeResult, error) {
				calls++
				if principal.UserID != item.CreatedBy || reserved.OperationID == "" || op.Kind != "file_read" || op.File.Path != "notes/proof.txt" || op.Request.CreateData {
					t.Fatal("file read changed its owner, operation or data identity")
				}
				return guest.RuntimeResult{}, test.invokeErr
			}}}
			_, err := service.readPersonalVMFile(t.Context(), domainidentity.Principal{UserID: item.CreatedBy}, item, "notes/proof.txt")
			if !errors.Is(err, test.wantErr) || calls != 1 || store.aborted != test.abort {
				t.Fatalf("file read reservation: err=%v calls=%d aborted=%t", err, calls, store.aborted)
			}
			if store.item.DataRef != item.DataRef || store.item.ComputeRef != item.ComputeRef || !store.item.DataInitialized {
				t.Fatal("file read changed the completed data binding")
			}
			if test.abort && (store.item.State != "ready" || store.item.OperationID != "") || !test.abort && (store.item.State != "unknown" || store.item.OperationID == "") {
				t.Fatal("file read discarded an unknown dispatch or retained an undispatched reservation")
			}
		})
	}
}

func TestPersonalFilePathsAndBoundedUTF8Receipts(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"", "/etc/passwd", "../other", "notes/../other", "./notes", "notes//other", "notes/", "bad\x00path", "bad\npath", "bad\\path", string([]byte{255}), strings.Repeat("x", 1025)} {
		if _, err := personalFilePath(path); !errors.Is(err, apperrors.ErrInvalidArgument) {
			t.Fatalf("unsafe data path accepted: %q %v", path, err)
		}
	}
	if absolute, err := personalFilePath("notes/用户.txt"); err != nil || absolute != "/data/notes/用户.txt" {
		t.Fatalf("valid relative path changed: %q %v", absolute, err)
	}
	for _, test := range []struct {
		output string
		err    error
	}{
		{fileReadFixture(""), nil}, {fileReadFixture("fixture"), nil}, {fileReadFixture(strings.Repeat("x", personalFileLimit)), nil},
		{fileReadFixture(strings.Repeat("x", personalFileLimit+1)), apperrors.ErrInvalidArgument}, {fileReadFixture(string([]byte{255})), apperrors.ErrInvalidArgument}, {"ok\ninvalid!", apperrors.ErrInvalidArgument},
		{"missing\n", apperrors.ErrNotFound}, {"denied\n", apperrors.ErrAccessDenied}, {"too_large\n", apperrors.ErrInvalidArgument}, {"untrusted", apperrors.ErrServiceUnavailable},
	} {
		if _, err := personalFileContent(test.output); !errors.Is(err, test.err) {
			t.Fatalf("file receipt accepted=%t want=%t", err == nil, test.err == nil)
		}
	}
}

func fileReadFixture(content string) string {
	return "ok\n" + base64.StdEncoding.EncodeToString([]byte(content))
}

// Tests the production fixed shell against a disposable, networkless sandbox;
// this proves path/read boundaries, not personal provisioning or disk quotas.
func TestPersonalFileReadScriptWithDocker(t *testing.T) {
	if os.Getenv("SOHA_PERSONAL_FILE_DOCKER_INTEGRATION") != "1" {
		t.Skip("explicit disposable Docker fixture required")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	runID := uuid.NewString()
	image := "busybox@sha256:9532d8c39891ca2ecde4d30d7710e01fb739c87a8b9299685c63704296b16028"
	// #nosec G204 -- Fixed Docker executable/options/image; the label is a new UUID fixture identifier.
	id, err := exec.CommandContext(ctx, "docker", "run", "--detach", "--rm", "--label", "soha.test.personal-file="+runID, "--network", "none", "--read-only", "--user", "1000:1000", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--memory", "64m", "--cpus", "0.25", "--pids-limit", "16", "--tmpfs", "/data:rw,nosuid,nodev,noexec,size=1m,uid=1000,gid=1000,mode=0700", image, "/bin/sleep", "120").Output()
	if err != nil {
		t.Fatal("create owned file sandbox:", err)
	}
	container := strings.TrimSpace(string(id))
	if len(container) != 64 || strings.Trim(container, "0123456789abcdef") != "" {
		t.Fatal("Docker returned an invalid fixture container ID")
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		// #nosec G204 -- Inspect only the validated full ID created by this fixture.
		owner, err := exec.CommandContext(cleanup, "docker", "inspect", "--format", `{{index .Config.Labels "soha.test.personal-file"}}`, container).Output()
		if err != nil || strings.TrimSpace(string(owner)) != runID {
			t.Error("cannot confirm file fixture ownership; refusing cleanup")
			return
		}
		// #nosec G204 -- Remove only the validated fixture ID after rechecking its unique ownership label.
		if err := exec.CommandContext(cleanup, "docker", "rm", "--force", container).Run(); err != nil {
			t.Error("file fixture cleanup:", err)
		}
	})
	seed := `printf 'fixture\n用户' > /data/notes.txt; : > /data/empty; mkdir /data/nested; printf 'nested' > /data/nested/file; ln -s /etc/passwd /data/outside; ln -s /data/notes.txt /data/inside; dd if=/dev/zero of=/data/large bs=61441 count=1 2>/dev/null; printf '\377' > /data/binary`
	// #nosec G204 -- Fixed seed script creates synthetic data only in the validated disposable container.
	if err := exec.CommandContext(ctx, "docker", "exec", container, "/bin/sh", "-ec", seed).Run(); err != nil {
		t.Fatal("seed synthetic files:", err)
	}
	for _, test := range []struct {
		path, content string
		err           error
	}{{"notes.txt", "fixture\n用户", nil}, {"nested/file", "nested", nil}, {"empty", "", nil}, {"inside", "fixture\n用户", nil}, {"outside", "", apperrors.ErrAccessDenied}, {"absent", "", apperrors.ErrNotFound}, {"nested", "", apperrors.ErrNotFound}, {"large", "", apperrors.ErrInvalidArgument}, {"binary", "", apperrors.ErrInvalidArgument}} {
		t.Run(test.path, func(t *testing.T) {
			absolute, err := personalFilePath(test.path)
			if err != nil {
				t.Fatal(err)
			}
			// #nosec G204 -- Validated fixture ID/path and the exact fixed production script, passed as argv.
			output, err := exec.CommandContext(ctx, "docker", "exec", container, "/bin/sh", "-ec", personalFileReadScript, "personal-file-read", absolute).Output()
			if err != nil {
				t.Fatal("fixed read command failed:", err)
			}
			content, err := personalFileContent(string(output))
			if !errors.Is(err, test.err) || content != test.content {
				t.Fatalf("file boundary mismatch: err=%v", err)
			}
		})
	}
	t.Run("opened_descriptor_survives_link_replacement", func(t *testing.T) {
		// Inject the precise replacement after source validation and before the
		// production read. The original check-then-reopen script fails this case.
		script := strings.Replace(personalFileReadScript, "printf 'ok\\n'", `rm "$resolved"; ln -s /etc/passwd "$resolved"; printf 'ok\n'`, 1)
		// #nosec G204 -- Fixed synthetic file, validated fixture ID, and deterministic test fault injection.
		output, err := exec.CommandContext(ctx, "docker", "exec", container, "/bin/sh", "-ec", script, "personal-file-read", "/data/empty").Output()
		if err != nil {
			t.Fatal("descriptor replacement fixture:", err)
		}
		if content, err := personalFileContent(string(output)); err != nil || content != "" {
			t.Fatal("read reopened the replaced path")
		}
	})
	t.Run("symlink_replacement_race", func(t *testing.T) { assertPersonalFileDescriptorRace(t, ctx, container) })
}

func assertPersonalFileDescriptorRace(t *testing.T, ctx context.Context, container string) {
	t.Helper()
	// #nosec G204 -- Fixed race generator runs inside the validated synthetic-data container.
	writer := exec.CommandContext(ctx, "docker", "exec", container, "/bin/sh", "-ec", `i=0; while [ "$i" -lt 20000 ]; do ln -s /etc/passwd /data/race.next; mv -Tf /data/race.next /data/race; ln -s /data/notes.txt /data/race.next; mv -Tf /data/race.next /data/race; i=$((i+1)); done`)
	if err := writer.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Process.Kill(); _ = writer.Wait() })
	observed := 0
	for range 100 {
		// #nosec G204 -- Fixed production read script/path and validated fixture container ID.
		output, err := exec.CommandContext(ctx, "docker", "exec", container, "/bin/sh", "-ec", personalFileReadScript, "personal-file-read", "/data/race").Output()
		if err != nil {
			continue // A disappearing/replaced file may fail closed.
		}
		content, err := personalFileContent(string(output))
		if err == nil && content != "fixture\n用户" {
			t.Fatal("symlink replacement exposed data outside the allowed opened file")
		}
		if err == nil {
			observed++
		}
	}
	if observed == 0 {
		t.Fatal("race fixture did not exercise a successful allowed read")
	}
}
