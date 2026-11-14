package dockerruntime

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	protocol "github.com/opensoha/soha/contracts/docker/runtime"
)

func helperFixture() (*VolumeReader, *readerHelper, string) {
	r := NewVolumeReader("sha256:"+strings.Repeat("a", 64), "11111111-1111-4111-8111-111111111111")
	nonce := "22222222-2222-4222-8222-222222222222"
	now := time.Now().UTC()
	h := &readerHelper{ID: strings.Repeat("b", 64), Image: r.image, Name: "/" + r.helperName(nonce), Created: now.Format(time.RFC3339Nano)}
	h.Config.Labels = map[string]string{readerOwnerLabel: r.owner, readerNonceLabel: nonce, readerProtocolLabel: protocol.ReadProtocol, readerIssuedLabel: strconv.FormatInt(now.Unix(), 10), readerDeadlineLabel: strconv.FormatInt(now.Add(time.Minute).Unix(), 10), readerContainerLabel: strings.Repeat("c", 64)}
	h.Config.User, h.Config.WorkingDir = "0:0", "/"
	h.Config.Entrypoint, h.Config.Cmd = []string{"/soha-agent"}, []string{"volume-reader"}
	h.HostConfig.ReadonlyRootfs, h.HostConfig.AutoRemove = true, true
	h.HostConfig.NetworkMode, h.HostConfig.IpcMode = "none", "none"
	h.HostConfig.PidMode = "container:" + h.Config.Labels[readerContainerLabel]
	h.HostConfig.CapDrop = []string{"ALL"}
	h.HostConfig.CapAdd = []string{"CAP_SYS_PTRACE", "CAP_DAC_READ_SEARCH"}
	h.HostConfig.SecurityOpt = []string{"no-new-privileges:true"}
	h.HostConfig.PidsLimit, h.HostConfig.Memory, h.HostConfig.NanoCpus = 16, 64<<20, 250000000
	return r, h, nonce
}

func TestVolumeHelperOwnershipRejectsForeignOrWeakenedIsolation(t *testing.T) {
	r, h, nonce := helperFixture()
	if err := r.ownsHelper(h, nonce); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*readerHelper){
		"image":                func(h *readerHelper) { h.Image = "sha256:" + strings.Repeat("d", 64) },
		"name":                 func(h *readerHelper) { h.Name = "/business" },
		"owner":                func(h *readerHelper) { h.Config.Labels[readerOwnerLabel] = "other" },
		"nonce":                func(h *readerHelper) { h.Config.Labels[readerNonceLabel] = "other" },
		"invalid lease":        func(h *readerHelper) { h.Config.Labels[readerDeadlineLabel] = "0" },
		"host PID":             func(h *readerHelper) { h.HostConfig.PidMode = "host" },
		"replacement CID":      func(h *readerHelper) { h.HostConfig.PidMode = "container:" + strings.Repeat("d", 64) },
		"network":              func(h *readerHelper) { h.HostConfig.NetworkMode = "bridge" },
		"writable root":        func(h *readerHelper) { h.HostConfig.ReadonlyRootfs = false },
		"privileged":           func(h *readerHelper) { h.HostConfig.Privileged = true },
		"host mount":           func(h *readerHelper) { h.Mounts = []Mount{{Type: "bind", Source: "/private", Destination: "/data"}} },
		"bind":                 func(h *readerHelper) { h.HostConfig.Binds = []string{"/private:/data"} },
		"image volume":         func(h *readerHelper) { h.Config.Volumes = map[string]any{"/data": map[string]any{}} },
		"environment":          func(h *readerHelper) { h.Config.Env = []string{"SOHA_AGENT_TOKEN=private-fixture"} },
		"entrypoint":           func(h *readerHelper) { h.Config.Entrypoint = []string{"/bin/sh"} },
		"command":              func(h *readerHelper) { h.Config.Cmd = []string{"serve"} },
		"capability":           func(h *readerHelper) { h.HostConfig.CapAdd = append(h.HostConfig.CapAdd, "SYS_ADMIN") },
		"memory":               func(h *readerHelper) { h.HostConfig.Memory = 0 },
		"PIDs":                 func(h *readerHelper) { h.HostConfig.PidsLimit = 0 },
		"user namespace":       func(h *readerHelper) { h.HostConfig.UsernsMode = "host" },
		"privilege escalation": func(h *readerHelper) { h.HostConfig.SecurityOpt = nil },
	} {
		t.Run(name, func(t *testing.T) {
			r, h, nonce := helperFixture()
			change(h)
			if r.ownsHelper(h, nonce) == nil {
				t.Fatal("unsafe helper ownership accepted")
			}
		})
	}
}

func TestUnknownHelperCreateIsNotRetriedAndOnlyOriginalHelperIsRemoved(t *testing.T) {
	r, h, nonce := helperFixture()
	// The generated nonce is learned from this one attempted create. The fake
	// daemon commits it but loses the reply; cleanup must find that exact object.
	createCount, removeCount := 0, 0
	exists := false
	r.observe = func(_ context.Context, _ int, args ...string) ([]byte, error) {
		switch args[1] {
		case "create":
			createCount++
			for i, a := range args {
				if a == "--name" {
					h.Name = "/" + args[i+1]
				}
				if a == "--label" {
					parts := strings.SplitN(args[i+1], "=", 2)
					h.Config.Labels[parts[0]] = parts[1]
				}
			}
			nonce = h.Config.Labels[readerNonceLabel]
			h.HostConfig.PidMode = "container:" + h.Config.Labels[readerContainerLabel]
			exists = true
			return nil, errors.New("lost create response")
		case "ls":
			if exists {
				return []byte(h.ID + "\n"), nil
			}
			return nil, nil
		case "inspect":
			return json.Marshal(h)
		case "rm":
			removeCount++
			if args[len(args)-1] != h.ID {
				t.Fatal("cleanup selected a name or different CID")
			}
			exists = false
			return nil, nil
		default:
			t.Fatalf("unexpected Docker command: %q", args)
			return nil, nil
		}
	}
	p, _ := protocol.NewProjectBinding(r.owner, "33333333-3333-4333-8333-333333333333")
	req := protocol.ReadRequest{Protocol: protocol.ReadProtocol, Action: "files.read", ProjectID: "project", Target: "/data", Container: protocol.ContainerBinding{ProjectBinding: p, ServiceName: "web", ContainerID: strings.Repeat("c", 64)}}
	if _, err := r.readWithHelper(context.Background(), req); err == nil {
		t.Fatal("unknown create claimed successful read")
	}
	if createCount != 1 || removeCount != 1 || exists || nonce == "22222222-2222-4222-8222-222222222222" {
		t.Fatal("original create/cleanup not resolved exactly once", createCount, removeCount, exists)
	}
}

func TestHelperCleanupConfirmsAbsenceAndRefusesForeignReplacement(t *testing.T) {
	for _, foreign := range []bool{false, true} {
		r, h, nonce := helperFixture()
		exists := true
		removed := false
		if foreign {
			h.Image = "sha256:" + strings.Repeat("f", 64)
		}
		r.observe = func(_ context.Context, _ int, args ...string) ([]byte, error) {
			switch args[1] {
			case "ls":
				if exists {
					return []byte(h.ID), nil
				}
				return nil, nil
			case "inspect":
				return json.Marshal(h)
			case "rm":
				removed = true
				exists = false
				return nil, errors.New("response lost after remove")
			}
			return nil, errors.New("unexpected call")
		}
		err := r.removeHelper(context.Background(), r.helperName(nonce), nonce)
		if foreign {
			if err == nil || removed || !exists {
				t.Fatal("foreign helper was removed")
			}
		} else if err != nil || !removed || exists {
			t.Fatal("confirmed absence was not accepted", err)
		}
	}
}

func TestHelperCollectorPreservesActiveLeaseAndReclaimsExpiredCreatedHelper(t *testing.T) {
	for _, expired := range []bool{false, true} {
		r, h, _ := helperFixture()
		exists := true
		removed := false
		now := time.Now().UTC()
		if expired {
			h.Created = now.Add(-2 * time.Minute).Format(time.RFC3339Nano)
			h.Config.Labels[readerIssuedLabel] = strconv.FormatInt(now.Add(-2*time.Minute).Unix(), 10)
			h.Config.Labels[readerDeadlineLabel] = strconv.FormatInt(now.Add(-time.Minute).Unix(), 10)
		}
		r.observe = func(_ context.Context, _ int, args ...string) ([]byte, error) {
			switch args[1] {
			case "ls":
				if exists {
					return []byte(h.ID), nil
				}
				return nil, nil
			case "inspect":
				return json.Marshal(h)
			case "rm":
				removed = true
				exists = false
				return nil, nil
			}
			return nil, errors.New("unexpected call")
		}
		if err := r.Collect(context.Background(), now); err != nil {
			t.Fatal(err)
		}
		if removed != expired || exists == expired {
			t.Fatal("lease expiry was not respected", expired, removed, exists)
		}
	}
}

func TestHelperPIDTargetMustMatchOriginalRequestBeforeStart(t *testing.T) {
	r, h, _ := helperFixture()
	exists := false
	creates, starts, removes := 0, 0, 0
	r.observe = func(_ context.Context, _ int, args ...string) ([]byte, error) {
		switch args[1] {
		case "create":
			creates++
			for i, a := range args {
				if a == "--name" {
					h.Name = "/" + args[i+1]
				}
				if a == "--label" {
					parts := strings.SplitN(args[i+1], "=", 2)
					h.Config.Labels[parts[0]] = parts[1]
				}
			}
			h.Config.Labels[readerContainerLabel] = strings.Repeat("d", 64)
			h.HostConfig.PidMode = "container:" + h.Config.Labels[readerContainerLabel]
			exists = true
			return []byte(h.ID), nil
		case "ls":
			if exists {
				return []byte(h.ID), nil
			}
			return nil, nil
		case "inspect":
			return json.Marshal(h)
		case "start":
			starts++
			return nil, errors.New("must not start")
		case "rm":
			removes++
			if args[len(args)-1] != h.ID {
				t.Fatal("removed target instead of helper")
			}
			exists = false
			return nil, nil
		}
		return nil, errors.New("unexpected command")
	}
	p, _ := protocol.NewProjectBinding(r.owner, "33333333-3333-4333-8333-333333333333")
	req := protocol.ReadRequest{Protocol: protocol.ReadProtocol, Action: "files.read", ProjectID: "project", Target: "/data", Container: protocol.ContainerBinding{ProjectBinding: p, ServiceName: "web", ContainerID: strings.Repeat("c", 64)}}
	if _, err := r.readWithHelper(context.Background(), req); err == nil || creates != 1 || starts != 0 || removes != 1 || exists {
		t.Fatal("cross-container helper was used", err, creates, starts, removes, exists)
	}
}

func TestHelperImageUpgradeRequiresExplicitCleanupOnlyGrant(t *testing.T) {
	for _, grant := range []bool{false, true} {
		old, h, _ := helperFixture()
		var cleanup []string
		if grant {
			cleanup = []string{old.image}
		}
		r := NewVolumeReader("sha256:"+strings.Repeat("f", 64), old.owner, cleanup...)
		now := time.Now().UTC()
		h.Created = now.Add(-2 * time.Minute).Format(time.RFC3339Nano)
		h.Config.Labels[readerIssuedLabel] = strconv.FormatInt(now.Add(-2*time.Minute).Unix(), 10)
		h.Config.Labels[readerDeadlineLabel] = strconv.FormatInt(now.Add(-time.Minute).Unix(), 10)
		exists, removed := true, false
		r.observe = func(_ context.Context, _ int, args ...string) ([]byte, error) {
			switch args[1] {
			case "ls":
				if exists {
					return []byte(h.ID), nil
				}
				return nil, nil
			case "inspect":
				return json.Marshal(h)
			case "rm":
				removed = true
				exists = false
				return nil, nil
			}
			return nil, errors.New("unexpected command")
		}
		err := r.Collect(context.Background(), now)
		if grant {
			if err != nil || !removed || exists {
				t.Fatal("authorized old image helper was not reclaimed", err)
			}
		} else if err == nil || removed || !exists {
			t.Fatal("unknown old image was removed")
		}
		if r.image == old.image {
			t.Fatal("cleanup-only image became the execution image")
		}
	}
}

func TestDelayedCreateDoesNotInvalidateExpiredHelperLease(t *testing.T) {
	r, h, nonce := helperFixture()
	now := time.Now().UTC()
	h.Config.Labels[readerIssuedLabel] = strconv.FormatInt(now.Add(-3*time.Minute).Unix(), 10)
	h.Config.Labels[readerDeadlineLabel] = strconv.FormatInt(now.Add(-2*time.Minute).Unix(), 10)
	// The daemon commits after both the original caller and its lease expired.
	h.Created = now.Add(-time.Minute).Format(time.RFC3339Nano)
	if err := r.ownsHelper(h, nonce); err != nil {
		t.Fatal("late create became unreclaimable", err)
	}
}

func TestSafeVolumeReaderRequiresPrivateDefaultNamespaces(t *testing.T) {
	item := Container{Platform: "linux", Running: true}
	if !safeReaderTarget(item) {
		t.Fatal("supported target rejected")
	}
	for _, change := range []func(*Container){func(c *Container) { c.Running = false }, func(c *Container) { c.Platform = "windows" }, func(c *Container) { c.PIDMode = "host" }, func(c *Container) { c.PIDMode = "container:other" }, func(c *Container) { c.UserNSMode = "host" }, func(c *Container) { c.Privileged = true }, func(c *Container) { c.CapAdd = []string{"CAP_SYS_ADMIN"} }, func(c *Container) { c.CapAdd = []string{"SYS_PTRACE"} }, func(c *Container) { c.CapAdd = []string{"ALL"} }} {
		c := item
		change(&c)
		if safeReaderTarget(c) {
			t.Fatal("unsafe target accepted", c)
		}
	}
	for _, configuration := range [][2]string{{"", ""}, {"image:latest", "11111111-1111-4111-8111-111111111111"}, {"sha256:" + strings.Repeat("a", 64), ""}} {
		r := NewVolumeReader(configuration[0], configuration[1])
		if r.Enabled() {
			t.Fatal("unconfigured reader enabled")
		}
		if _, err := r.ReadFiles(context.Background(), protocol.ReadRequest{}); !errors.Is(err, ErrVolumeReaderUnsupported) {
			t.Fatal(err)
		}
	}
	r, _, _ := helperFixture()
	calls := 0
	r.observe = func(context.Context, int, ...string) ([]byte, error) {
		calls++
		return nil, errors.New("unexpected Docker call")
	}
	for i := 0; i < cap(r.slots); i++ {
		r.slots <- struct{}{}
	}
	p, _ := protocol.NewProjectBinding(r.owner, "33333333-3333-4333-8333-333333333333")
	req := protocol.ReadRequest{Protocol: protocol.ReadProtocol, Action: "files.read", ProjectID: "project", Target: "/data", Container: protocol.ContainerBinding{ProjectBinding: p, ServiceName: "web", ContainerID: strings.Repeat("c", 64)}}
	if _, err := r.ReadFiles(context.Background(), req); err == nil || calls != 0 {
		t.Fatal("saturated reader dispatched Docker work", err, calls)
	}
}
