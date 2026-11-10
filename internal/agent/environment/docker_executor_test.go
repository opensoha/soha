package environment

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

const testPersonalImage = "busybox@sha256:9532d8c39891ca2ecde4d30d7710e01fb739c87a8b9299685c63704296b16028"
const testQuotaImage = "docker@sha256:a6dd5322747a95cd8e3207bd8d415a8fd20ec34e9c00f06dc019cbd912013489"

func personalDockerRequest(owner, id string) LocalRequest {
	now := time.Now().UTC().Truncate(time.Microsecond)
	return LocalRequest{Lease: Lease{ID: id, RunID: "run-" + id, OwnerID: owner, ScopeHash: "sha256:" + strings.Repeat("a", 64),
		Mode: "restricted-write", IssuedAt: now, ExpiresAt: now.Add(time.Hour),
		Quota: Quota{DiskBytes: 32 << 20, MemoryBytes: 64 << 20, CPUUnits: 250, MaxProcesses: 16}},
		Spec: Spec{Backend: BackendLocalContainer, Name: "personal-task", Image: testPersonalImage, Workspace: "/unused"}}
}

func personalDockerRecord(request LocalRequest) (dockerEnvironmentRecord, Handle) {
	name, ref := dockerLeaseIdentity(request.Lease)
	data, _ := json.Marshal(request.Lease)
	record := dockerEnvironmentRecord{ID: strings.Repeat("b", 64), Labels: map[string]string{
		dockerEnvironmentLabel: dockerEnvironmentBackend, dockerEnvironmentLabel + ".ref": ref,
		dockerEnvironmentLabel + ".name": name, dockerEnvironmentLabel + ".image": testPersonalImage,
		dockerEnvironmentLabel + ".lease": base64.RawURLEncoding.EncodeToString(data)}}
	return record, Handle{ID: record.ID, Ref: ref, Backend: BackendLocalContainer}
}

func TestDockerEnvironmentRejectsUnsupportedBoundariesBeforeDocker(t *testing.T) {
	changes := map[string]func(*LocalRequest){
		"network egress":          func(r *LocalRequest) { r.Lease.Quota.NetworkEgress = true },
		"different image":         func(r *LocalRequest) { r.Spec.Image = "busybox:latest" },
		"startup command":         func(r *LocalRequest) { r.Spec.Command = "/bin/sh" },
		"plaintext environment":   func(r *LocalRequest) { r.Spec.Env = map[string]string{"VALUE": "data"} },
		"unbounded process count": func(r *LocalRequest) { r.Lease.Quota.MaxProcesses = 0 },
		"oversized CPU":           func(r *LocalRequest) { r.Lease.Quota.CPUUnits = 8001 },
		"unsupported disk":        func(r *LocalRequest) { r.Lease.Quota.DiskBytes = 1 },
		"long lease":              func(r *LocalRequest) { r.Lease.ExpiresAt = time.Now().Add(25 * time.Hour) },
		"invalid scope":           func(r *LocalRequest) { r.Lease.ScopeHash = "unscoped" },
		"oversized owner":         func(r *LocalRequest) { r.Lease.OwnerID = strings.Repeat("x", 129) },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			executor, err := NewDockerExecutor(testPersonalImage, testQuotaImage)
			if err != nil {
				t.Fatal(err)
			}
			executor.run = func(context.Context, ...string) ([]byte, error) {
				t.Fatal("invalid input reached Docker")
				return nil, nil
			}
			request := personalDockerRequest("owner", "lease")
			change(&request)
			if _, err := executor.Start(t.Context(), request); err == nil {
				t.Fatal("unsupported request accepted")
			}
		})
	}
	if _, err := NewDockerExecutor("busybox:latest", testQuotaImage); err == nil {
		t.Fatal("mutable image accepted")
	}
}

func TestPersonalVolumeRejectsBindOptionsOrPluginEvenWithMatchingOwnerLabels(t *testing.T) {
	lease := personalDockerRequest("owner", "lease").Lease
	for _, test := range []struct {
		name, driver string
		options      map[string]string
		valid        bool
	}{
		{"owned local", "local", nil, true},
		{"plugin", "other-driver", nil, false},
		{"bind mount", "local", map[string]string{"type": "none", "device": "/host/path", "o": "bind"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			executor, err := NewDockerExecutor(testPersonalImage, testQuotaImage)
			if err != nil {
				t.Fatal(err)
			}
			executor.run = func(_ context.Context, args ...string) ([]byte, error) {
				if len(args) != 5 || args[0] != "volume" || args[1] != "inspect" || args[4] != dockerPersonalVolume(lease.OwnerID) {
					t.Fatalf("unexpected operation: %+v", args)
				}
				return json.Marshal(map[string]any{"driver": test.driver, "options": test.options, "labels": map[string]string{dockerEnvironmentLabel: dockerEnvironmentBackend, dockerEnvironmentLabel + ".owner": dockerHash(lease.OwnerID), dockerEnvironmentLabel + ".disk": "33554432"}})
			}
			if err := executor.verifyVolume(t.Context(), lease); (err == nil) != test.valid {
				t.Fatalf("volume driver bypass: %v", err)
			}
		})
	}
}

func TestDockerEnvironmentRecoversOriginalLeaseAndRejectsAnotherWriter(t *testing.T) {
	request := personalDockerRequest("owner", "lease")
	record, handle := personalDockerRecord(request)
	name := record.Labels[dockerEnvironmentLabel+".name"]
	executor, err := NewDockerExecutor(testPersonalImage, testQuotaImage)
	if err != nil {
		t.Fatal(err)
	}
	creates := 0
	executor.run = func(_ context.Context, args ...string) ([]byte, error) {
		switch args[0] {
		case "info":
			return []byte("2"), nil
		case "container":
			if args[1] == "ls" {
				return []byte(name), nil
			}
			if strings.Contains(args[3], "Running") {
				return []byte("true"), nil
			}
			return json.Marshal(record)
		case "volume":
			if args[1] == "ls" {
				return nil, nil
			}
			if args[1] == "create" {
				return []byte(dockerPersonalVolume(request.Lease.OwnerID)), nil
			}
			return json.Marshal(map[string]any{"driver": "local", "options": nil, "labels": map[string]string{dockerEnvironmentLabel: dockerEnvironmentBackend, dockerEnvironmentLabel + ".owner": dockerHash(request.Lease.OwnerID), dockerEnvironmentLabel + ".disk": "33554432"}})
		case "run":
			if args[1] == "--detach" {
				creates++
				return nil, errors.New("unexpected creation")
			}
			return []byte("qgroupid rfer excl max_rfer\n-------- ---- ---- --------\n0/999 16384 16384 33554432\n"), nil
		case "exec":
			if args[2] == "65534:65534" {
				return []byte("ready"), nil
			}
			return nil, errors.New("unexpected task dispatch")
		default:
			return nil, errors.New("unexpected Docker operation")
		}
	}
	recovered, err := executor.Start(t.Context(), request)
	if err != nil || recovered != handle || creates != 0 {
		t.Fatalf("lease recovery created another container: %+v %v", recovered, err)
	}
	other := request
	other.Lease.ID, other.Lease.RunID = "another-lease", "another-run"
	if _, err := executor.Start(t.Context(), other); err == nil || creates != 0 {
		t.Fatal("another writer adopted the active container")
	}
	manager := NewManager()
	acquired, err := manager.Acquire(request.Lease, time.Now().UTC())
	if err != nil || acquired.IssuedAt != request.Lease.IssuedAt {
		t.Fatalf("recovery rewrote original issuance: %+v %v", acquired, err)
	}
}

func TestDockerCancellationDuringQuotaCheckRemovesOnlyImmutableContainer(t *testing.T) {
	request := personalDockerRequest("owner", "lease")
	record, handle := personalDockerRecord(request)
	executor, err := NewDockerExecutor(testPersonalImage, testQuotaImage)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	removed := ""
	executed := false
	fixture := &dockerCommandFixture{parent: record}
	fixture.before = func(args []string) error {
		if args[0] == "run" {
			cancel()
			return context.Canceled
		}
		if args[0] == "exec" && args[2] != "65534:65534" {
			executed = true
		}
		if args[0] == "container" && args[1] == "rm" && args[len(args)-1] == handle.ID {
			removed = handle.ID
		}
		return nil
	}
	executor.run = fixture.run
	if _, err := executor.Execute(ctx, handle, []string{"/bin/echo", "owned"}); !errors.Is(err, context.Canceled) || removed != handle.ID || executed {
		t.Fatalf("cancelled preflight retained or misidentified the container: %v removed=%q executed=%v", err, removed, executed)
	}
	fixture.parent = record
	fixture.parent.Labels[dockerEnvironmentLabel+".ref"] = "another-lease"
	removed = ""
	if err := executor.Stop(t.Context(), handle); err == nil || removed != "" {
		t.Fatal("cleanup removed another lease")
	}
}

type dockerCommandFixture struct {
	parent dockerEnvironmentRecord
	fence  dockerEnvironmentRecord
	before func([]string) error
}

func (f *dockerCommandFixture) run(_ context.Context, args ...string) ([]byte, error) {
	if f.before != nil {
		if err := f.before(args); err != nil {
			return nil, err
		}
	}
	switch args[0] {
	case "create":
		if f.fence.ID != "" {
			return nil, errors.New("name already reserved")
		}
		labels := map[string]string{}
		for i := 0; i+1 < len(args); i++ {
			if args[i] == "--label" {
				parts := strings.SplitN(args[i+1], "=", 2)
				labels[parts[0]] = parts[1]
			}
		}
		f.fence = dockerEnvironmentRecord{ID: strings.Repeat("c", 64), Labels: labels}
		return []byte(f.fence.ID), nil
	case "container":
		if args[1] == "ls" {
			filter := args[5]
			for _, record := range []dockerEnvironmentRecord{f.parent, f.fence} {
				if record.ID == "" {
					continue
				}
				if filter == "id="+record.ID {
					return []byte(record.ID), nil
				}
				name := record.Labels[dockerEnvironmentLabel+".name"]
				if record.ID == f.fence.ID {
					name = dockerCommandFenceName(Handle{ID: record.Labels[dockerEnvironmentLabel+".parent"]})
				}
				if filter == "name=^/"+name+"$" {
					return []byte(name), nil
				}
			}
			return nil, nil
		}
		id := args[len(args)-1]
		if args[1] == "rm" {
			if id == f.parent.ID {
				f.parent.ID = ""
			}
			if id == f.fence.ID {
				f.fence.ID = ""
			}
			return nil, nil
		}
		if strings.Contains(args[3], "Running") {
			return []byte("true"), nil
		}
		if id == f.parent.ID {
			return json.Marshal(f.parent)
		}
		return json.Marshal(f.fence)
	case "volume":
		if args[1] == "ls" {
			return nil, nil
		}
		return json.Marshal(map[string]any{"driver": "local", "options": nil, "labels": map[string]string{dockerEnvironmentLabel: dockerEnvironmentBackend, dockerEnvironmentLabel + ".owner": dockerHash("owner"), dockerEnvironmentLabel + ".disk": "33554432"}})
	case "exec":
		if args[2] == "65534:65534" {
			return []byte("ready"), nil
		}
	}
	return nil, errors.New("unexpected operation")
}

func TestDockerCommandReservationFencesSeparateAgentsAndLostAttempts(t *testing.T) {
	request := personalDockerRequest("owner", "lease")
	record, handle := personalDockerRecord(request)
	fixture := &dockerCommandFixture{parent: record}
	first, err := NewDockerExecutor(testPersonalImage, testQuotaImage)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewDockerExecutor(testPersonalImage, testQuotaImage)
	if err != nil {
		t.Fatal(err)
	}
	first.run, second.run = fixture.run, fixture.run
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	claimed, err := first.acquireCommandFence(ctx, handle)
	if err != nil || claimed.ID == "" {
		t.Fatalf("command admission failed: %+v %v", claimed, err)
	}
	if _, err := second.acquireCommandFence(ctx, handle); !errors.Is(err, ErrEnvironmentCommandBusy) || fixture.parent.ID != handle.ID || fixture.fence.ID != claimed.ID {
		t.Fatal("another Agent overlapped or cancelled the active command")
	}
	fixture.fence.Labels[dockerEnvironmentLabel+".deadline"] = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano)
	if _, err := second.acquireCommandFence(ctx, handle); err == nil || fixture.parent.ID != "" || fixture.fence.ID != "" {
		t.Fatal("lost attempt silently replayed or retained its sandbox")
	}
}

func TestDockerRecoveryGCDoesNotStarveExpiredOrOrphanReceipts(t *testing.T) {
	now := time.Now().UTC()
	var rows strings.Builder
	for i := 0; i < defaultGCBatchLimit+8; i++ {
		fmt.Fprintf(&rows, "%s %s\n", strings.Repeat("a", 64), now.Add(time.Hour).Format(time.RFC3339Nano))
	}
	oldID := strings.Repeat("b", 64)
	fmt.Fprintf(&rows, "%s %s\n", oldID, now.Add(-time.Minute).Format(time.RFC3339Nano))
	ids, err := expiredDockerReceiptIDs(rows.String(), now)
	if err != nil || len(ids) != 1 || ids[0] != oldID {
		t.Fatalf("active receipts starved expired cleanup: %v %v", ids, err)
	}
	request := personalDockerRequest("owner", "lease")
	record, handle := personalDockerRecord(request)
	fixture := &dockerCommandFixture{parent: record}
	executor, err := NewDockerExecutor(testPersonalImage, testQuotaImage)
	if err != nil {
		t.Fatal(err)
	}
	executor.run = fixture.run
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	fence, err := executor.acquireCommandFence(ctx, handle)
	if err != nil {
		t.Fatal(err)
	}
	fixture.parent.ID = "" // crash between parent removal and marker removal
	fixture.fence.Labels[dockerEnvironmentLabel+".deadline"] = now.Add(-time.Minute).Format(time.RFC3339Nano)
	executor.run = func(ctx context.Context, args ...string) ([]byte, error) {
		if args[0] == "container" && args[1] == "ls" && strings.Contains(args[5], "personal-docker-command-v1") {
			return []byte(fence.ID + " " + fixture.fence.Labels[dockerEnvironmentLabel+".deadline"]), nil
		}
		if args[0] == "container" && args[1] == "ls" && strings.Contains(args[5], "label=") {
			return nil, nil
		}
		return fixture.run(ctx, args...)
	}
	if _, err := executor.GC(ctx, now); err != nil || fixture.fence.ID != "" {
		t.Fatalf("orphan command reservation leaked: %v", err)
	}
}

func TestDockerEnvironmentRejectsUnreliableQuotaAndUsage(t *testing.T) {
	for _, output := range []string{"", "0/1 0 0 none", "0/1 0 0 31", "0/1 -1 0 32", "0/1 0 0 32\n0/2 0 0 32", "0/1 invalid 0 32", "WARNING: qgroup data inconsistent\n0/1 0 0 32"} {
		if _, err := parseDockerVolumeQuota(output, 32); err == nil {
			t.Fatalf("unreliable quota accepted: %q", output)
		}
	}
	if usage, err := parseDockerVolumeQuota("Qgroupid Referenced Exclusive Max referenced Path\n0/1 16 16 32 docker/volumes/fixture/_data/data", 32); err != nil || usage != 16 {
		t.Fatalf("actual btrfs-progs Path column rejected: %d %v", usage, err)
	}
	for _, output := range []string{"", "1 2 usage_usec -1", "1 2 usage_usec 9223372036854775807", "-1 2 usage_usec 3", "1 -2 usage_usec 3"} {
		if _, err := parseDockerCgroupUsage(output); err == nil {
			t.Fatalf("unreliable usage accepted: %q", output)
		}
	}
	usage, err := parseDockerCgroupUsage("4096\n3\nusage_usec 8\nuser_usec 4\nsystem_usec 4\n")
	if err != nil || usage.MemoryBytes != 4096 || usage.Processes != 3 || usage.CPUTimeNanos != 8000 {
		t.Fatalf("actual usage lost: %+v %v", usage, err)
	}
}
