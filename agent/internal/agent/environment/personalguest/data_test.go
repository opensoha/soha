package personalguest

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	guest "github.com/opensoha/soha-contracts/agent/guest"
)

func testDataPlan() DataPlan {
	now := time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC)
	return DataPlan{SchemaVersion: dataPlanVersion, OperationID: "11111111-1111-4111-8111-111111111111", DiskQuotaBytes: 6 << 30,
		Binding: guest.Binding{SchemaVersion: guest.BindingVersion, ID: "22222222-2222-4222-8222-222222222222", Version: 1,
			AssistantID: "33333333-3333-4333-8333-333333333333", OwnerID: "44444444-4444-4444-8444-444444444444", Provider: "pve",
			ComputeRef: "55555555-5555-4555-8555-555555555555", ComputeGeneration: 1, ProviderIdentityHash: "sha256:" + strings.Repeat("a", 64),
			DataRef: "66666666-6666-4666-8666-666666666666", DataDiskSerial: "soha49data01", DataDiskBytes: 8 << 30,
			PolicyRevision: "sha256:" + strings.Repeat("b", 64), IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour)}}
}

type fakeDataHost struct {
	plan            DataPlan
	block           dataBlock
	mounts          []dataMount
	signatures      string
	receipt         dataReceipt
	found           bool
	calls           [][]string
	failTool        string
	failSave        string
	quotaOutput     string
	staleMetadata   bool
	userQuotaOutput string
	changeOnRead    int
	reads           int
	root            string
	deniedAt        int
	authorizations  int
}

func newFakeDataHost() *fakeDataHost {
	plan := testDataPlan()
	return &fakeDataHost{plan: plan, block: dataBlock{Name: "/dev/sdb", Type: "disk", Size: 8 << 30, Serial: "soha49data01", DeviceID: "8:16"},
		mounts: []dataMount{{Source: "/dev/sda1", Target: "/", FSType: "ext4", DeviceID: "8:1", Options: "rw"}}, signatures: `{"signatures":[]}`, root: "/var/lib/soha/personal-guest-data"}
}

func (f *fakeDataHost) initializer() dataInitializer {
	return dataInitializer{run: f.run, base: f.root, now: func() time.Time { return time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC) },
		authorize: func(_ context.Context, current DataPlan) error {
			if current != f.plan {
				return errors.New("current data lease differs")
			}
			f.authorizations++
			if f.deniedAt == f.authorizations {
				return errors.New("current authority denied")
			}
			return nil
		},
		load: func() (dataReceipt, bool, error) { return f.receipt, f.found, nil },
		save: func(value dataReceipt) error {
			if f.failSave == value.Stage {
				return errors.New("persist failed")
			}
			f.receipt, f.found = value, true
			return nil
		}, mkdir: func(string) error { return nil }}
}

func (f *fakeDataHost) run(_ context.Context, tool string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string{tool}, args...))
	if f.failTool == tool {
		return nil, errors.New("tool failed")
	}
	switch tool {
	case "/usr/bin/lsblk":
		f.reads++
		if f.changeOnRead == f.reads {
			f.block.UUID = "77777777-7777-4777-8777-777777777777"
		}
		observed := f.block
		if strings.Contains(strings.Join(args, " "), "--properties-by blkid") {
			observed.Serial = ""
		} else if f.staleMetadata {
			observed.FSType, observed.UUID = "", ""
		}
		return json.Marshal(map[string]any{"blockdevices": []dataBlock{observed}})
	case "/usr/bin/findmnt":
		return json.Marshal(map[string]any{"filesystems": f.mounts})
	case "/usr/sbin/wipefs":
		return []byte(f.signatures), nil
	case "/usr/sbin/mkfs.xfs":
		if !f.found || f.receipt.Stage != "format_dispatched" {
			return nil, errors.New("mkfs dispatched before durable marker")
		}
		f.block.FSType, f.block.UUID = "xfs", f.plan.Binding.DataRef
	case "/usr/bin/mount":
		path := filepath.Join(f.root, f.plan.Binding.DataRef, "mount")
		f.mounts = append(f.mounts, dataMount{Source: "/dev/sdb", Target: path, FSType: "xfs", Options: "rw,nodev,nosuid,noexec,usrquota,prjquota", DeviceID: f.block.DeviceID, FSRoot: "/"})
	case "/usr/sbin/xfs_quota":
		if args[2] == "state -p" || args[2] == "state -u" {
			return []byte("Accounting: ON\nEnforcement: ON\n"), nil
		}
		if args[2] == "report -p -b -n" {
			if f.quotaOutput != "" {
				return []byte(f.quotaOutput), nil
			}
			return []byte("Project ID Used Soft Hard Warn Grace\n#49 4 0 " + strconv.FormatInt(f.plan.DiskQuotaBytes/1024, 10) + " 00 [------]\n"), nil
		}
		if args[2] == "report -u -b -n" {
			if f.userQuotaOutput != "" {
				return []byte(f.userQuotaOutput), nil
			}
			return []byte("#1000 4 0 " + strconv.FormatInt(f.plan.DiskQuotaBytes/1024, 10) + " 00 [------]\n"), nil
		}

	default:
		return nil, errors.New("unplanned tool")
	}
	return []byte{}, nil
}

func (f *fakeDataHost) count(tool string) int {
	n := 0
	for _, call := range f.calls {
		if call[0] == tool {
			n++
		}
	}
	return n
}

func TestPersonalGuestDataInitializationOnceAndRemount(t *testing.T) {
	f := newFakeDataHost()
	originalOperation := f.plan.OperationID
	first, err := f.initializer().initialize(context.Background(), f.plan)
	if err != nil || first.Reconciled || first.FilesystemID != f.plan.Binding.DataRef || first.DiskQuota != 6<<30 || first.DiskUsed != 4096 || f.receipt.Stage != "complete" {
		t.Fatalf("initialization receipt: %#v, %v", first, err)
	}
	if f.count("/usr/sbin/mkfs.xfs") != 1 {
		t.Fatal("expected exactly one mkfs")
	}
	// Reboot changes the kernel device number; stable serial + FS UUID follow it.
	f.plan.OperationID = "77777777-7777-4777-8777-777777777777"
	f.plan.Binding.Version++
	f.block.DeviceID = "8:32"
	f.mounts = f.mounts[:1]
	second, err := f.initializer().initialize(context.Background(), f.plan)
	if err != nil || !second.Reconciled || f.count("/usr/sbin/mkfs.xfs") != 1 || f.count("/usr/bin/mount") != 2 {
		t.Fatalf("remount must preserve the original filesystem: %#v, %v", second, err)
	}
	if f.receipt.Plan.OperationID != originalOperation {
		t.Fatal("remount rewrote the historical format operation")
	}
	for _, call := range f.calls {
		for _, arg := range call {
			if arg == "-f" || arg == "--force" || arg == "delete" || arg == "remove" || arg == "discard" {
				t.Fatalf("unexpected destructive fallback: %v", call)
			}
		}
	}
}

func TestPersonalGuestDataUnknownFormatNeverRetries(t *testing.T) {
	f := newFakeDataHost()
	f.failTool = "/usr/sbin/mkfs.xfs"
	if _, err := f.initializer().initialize(context.Background(), f.plan); err == nil || f.receipt.Stage != "format_dispatched" {
		t.Fatal("lost format result must retain the dispatch marker")
	}
	f.failTool = ""
	if _, err := f.initializer().initialize(context.Background(), f.plan); err == nil || f.count("/usr/sbin/mkfs.xfs") != 1 {
		t.Fatal("unknown format must never issue another mkfs")
	}
	// The original mkfs might have completed despite losing its reply.
	f.block.FSType, f.block.UUID = "xfs", f.plan.Binding.DataRef
	if _, err := f.initializer().initialize(context.Background(), f.plan); err != nil || f.count("/usr/sbin/mkfs.xfs") != 1 {
		t.Fatal("matching filesystem must recover without repeating mkfs", err)
	}
}

func TestPersonalGuestDataRejectsBeforeFormatting(t *testing.T) {
	tests := []struct {
		name string
		edit func(*fakeDataHost)
	}{
		{"expired", func(f *fakeDataHost) { f.plan.Binding.ExpiresAt = f.plan.Binding.IssuedAt.Add(time.Second) }},
		{"foreign operation", func(f *fakeDataHost) { f.plan.OperationID = "unsafe" }},
		{"unsupported provider", func(f *fakeDataHost) { f.plan.Binding.Provider = "kubevirt" }},
		{"wrong geometry", func(f *fakeDataHost) { f.block.Size-- }},
		{"wrong serial", func(f *fakeDataHost) { f.block.Serial = "another" }},
		{"read only", func(f *fakeDataHost) { f.block.ReadOnly = true }},
		{"device path escape", func(f *fakeDataHost) { f.block.Name = "/dev/../etc/shadow" }},
		{"partition table", func(f *fakeDataHost) { f.block.Children = []dataBlock{{Type: "part", DeviceID: "8:17"}} }},
		{"root disk", func(f *fakeDataHost) { f.mounts[0].DeviceID = f.block.DeviceID }},
		{"unknown root", func(f *fakeDataHost) { f.mounts = nil }},
		{"existing filesystem", func(f *fakeDataHost) { f.block.FSType, f.block.UUID = "xfs", f.plan.Binding.DataRef }},
		{"mounted", func(f *fakeDataHost) { f.block.Mountpoints = []string{"/mnt/other"} }},
		{"signature", func(f *fakeDataHost) { f.signatures = `{"signatures":[{"type":"gpt"}]}` }},
		{"unknown signature", func(f *fakeDataHost) { f.signatures = `{}` }},
		{"wipefs failure", func(f *fakeDataHost) { f.failTool = "/usr/sbin/wipefs" }},
		{"dispatch marker failure", func(f *fakeDataHost) { f.failSave = "format_dispatched" }},
		{"reserved marker failure", func(f *fakeDataHost) { f.failSave = "reserved" }},
		{"revoked before reservation", func(f *fakeDataHost) { f.deniedAt = 1 }},
		{"revoked before format", func(f *fakeDataHost) { f.deniedAt = 2 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeDataHost()
			tt.edit(f)
			if _, err := f.initializer().initialize(context.Background(), f.plan); err == nil {
				t.Fatal("unsafe initialization accepted")
			}
			if f.count("/usr/sbin/mkfs.xfs") != 0 || f.count("/usr/bin/mount") != 0 {
				t.Fatal("unsafe operation dispatched")
			}
		})
	}
}

func TestPersonalGuestDataCurrentBindingRequiresMutualTLS(t *testing.T) {
	f := newGuestTLSFixture(t)
	f.registered = true
	httpClient, err := guestHTTPClient(f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer httpClient.CloseIdleConnections()
	client := &Client{cfg: f.cfg, http: httpClient}
	plan := testDataPlan()
	plan.Binding = f.binding
	if err := client.authorizeData(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	plan.Binding.DataRef = "77777777-7777-4777-8777-777777777777"
	if err := client.authorizeData(context.Background(), plan); err == nil {
		t.Fatal("different current Core data binding accepted")
	}
	plan.Binding = f.binding
	f.mu.Lock()
	f.revoked = true
	f.mu.Unlock()
	if err := client.authorizeData(context.Background(), plan); err == nil {
		t.Fatal("revoked current authority accepted")
	}
	f.mu.Lock()
	f.revoked, f.redirect = false, "https://127.0.0.1/other"
	f.mu.Unlock()
	if err := client.authorizeData(context.Background(), plan); err == nil {
		t.Fatal("authority redirect accepted")
	}
}

func TestPersonalGuestDataRevocationStopsLaterMutations(t *testing.T) {
	for _, deniedAt := range []int{3, 4} {
		t.Run(strconv.Itoa(deniedAt), func(t *testing.T) {
			f := newFakeDataHost()
			f.deniedAt = deniedAt
			if _, err := f.initializer().initialize(context.Background(), f.plan); err == nil || f.receipt.Stage == "complete" {
				t.Fatal("revocation cannot produce a successful data receipt")
			}
			if deniedAt == 3 && f.count("/usr/bin/mount") != 0 || f.count("/usr/sbin/xfs_quota") != 0 {
				t.Fatal("revocation failed to stop the next mutation")
			}
			if f.count("/usr/sbin/mkfs.xfs") != 1 || f.receipt.Stage != "format_dispatched" {
				t.Fatal("revocation must preserve the original once-only intent")
			}
		})
	}
}

func TestPersonalGuestDataRejectsReceiptOrMountedIdentityChange(t *testing.T) {
	tests := []struct {
		name string
		edit func(*fakeDataHost)
	}{
		{"different owner", func(f *fakeDataHost) { f.plan.Binding.OwnerID = "77777777-7777-4777-8777-777777777777" }},
		{"different quota", func(f *fakeDataHost) { f.plan.DiskQuotaBytes -= 1024 }},
		{"lost receipt", func(f *fakeDataHost) { f.found = false }},
		{"wrong filesystem UUID", func(f *fakeDataHost) { f.block.UUID = "77777777-7777-4777-8777-777777777777" }},
		{"wrong target", func(f *fakeDataHost) { f.mounts[1].Target = "/mnt/other" }},
		{"foreign target device", func(f *fakeDataHost) { f.mounts[1].DeviceID = "8:48" }},
		{"unsafe mount", func(f *fakeDataHost) { f.mounts[1].Options = "rw" }},
		{"wrong subvolume", func(f *fakeDataHost) { f.mounts[1].FSRoot = "/other" }},
		{"duplicate mount", func(f *fakeDataHost) { f.mounts = append(f.mounts, f.mounts[1]) }},
		{"invalid stage", func(f *fakeDataHost) { f.receipt.Stage = "ready" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeDataHost()
			if _, err := f.initializer().initialize(context.Background(), f.plan); err != nil {
				t.Fatal(err)
			}
			f.calls = nil
			tt.edit(f)
			if _, err := f.initializer().initialize(context.Background(), f.plan); err == nil {
				t.Fatal("changed identity accepted")
			}
			if f.count("/usr/sbin/mkfs.xfs") != 0 || f.count("/usr/bin/mount") != 0 {
				t.Fatal("changed identity caused a destructive fallback")
			}
		})
	}
}

func TestPersonalGuestDataQuotaReadbackAndMountRace(t *testing.T) {
	for _, output := range []string{"#49 1 0 0", "#49 1 0 6291456\nWARNING: inconsistent", "#49 1 0 6291456\n#49 1 0 6291456", "#50 1 0 6291456", "#49 -1 0 6291456"} {
		f := newFakeDataHost()
		f.quotaOutput = output
		if _, err := f.initializer().initialize(context.Background(), f.plan); err == nil || f.receipt.Stage == "complete" {
			t.Fatalf("false quota success: %q", output)
		}
	}
	f := newFakeDataHost()
	f.changeOnRead = 8
	if _, err := f.initializer().initialize(context.Background(), f.plan); err == nil || f.count("/usr/sbin/xfs_quota") != 0 {
		t.Fatal("device replacement during mount reached quota mutation")
	}
}

func TestPersonalGuestDataDuplicateSerial(t *testing.T) {
	f := newFakeDataHost()
	if _, err := selectDataBlock([]dataBlock{f.block, f.block}, f.block.Serial, f.block.Size); err == nil {
		t.Fatal("duplicate disk serial accepted")
	}
}

func TestPersonalGuestDataRequiresUIDQuotaEvenWithValidProjectQuota(t *testing.T) {
	for _, output := range []string{"#1000 1 0 0", "#1000 1 0 6291456\nWARNING: inconsistent", "#1001 1 0 6291456", "#1000 -1 0 6291456"} {
		f := newFakeDataHost()
		f.userQuotaOutput = output
		if _, err := f.initializer().initialize(t.Context(), f.plan); err == nil || f.receipt.Stage == "complete" {
			t.Fatal("project quota alone granted data readiness", output)
		}
	}
}

func TestPersonalGuestDataFilesystemProbeKeepsSerialAndIgnoresUdevCache(t *testing.T) {
	f := newFakeDataHost()
	f.staleMetadata = true
	result, err := f.initializer().initialize(t.Context(), f.plan)
	if err != nil || result.FilesystemID != f.plan.Binding.DataRef || f.count("/usr/sbin/mkfs.xfs") != 1 {
		t.Fatal("stale udev metadata broke once-only initialization", err)
	}
	direct := 0
	for _, call := range f.calls {
		if call[0] == "/usr/bin/lsblk" && strings.Contains(strings.Join(call, " "), "--properties-by blkid") {
			direct++
			if call[len(call)-1] != "/dev/sdb" {
				t.Fatal("direct probe escaped the selected disk")
			}
		}
	}
	if direct == 0 {
		t.Fatal("filesystem did not receive an authoritative probe")
	}
}
