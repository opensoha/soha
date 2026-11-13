//go:build linux || darwin

package radiusruntime

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	api "github.com/opensoha/soha/contracts/gen/go/sohaapi"
)

type catalogSource struct {
	mu      sync.Mutex
	base    *fixtureControl
	entries []ConfigurationSnapshot
	index   int
}

func (c *catalogSource) Fetch(ctx context.Context, epoch string) (Lease, error) {
	lease, err := c.base.Fetch(ctx, epoch)
	if err != nil {
		return Lease{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry := c.entries[c.index]
	lease.Configuration, lease.ConfigurationSHA256 = entry.Material, entry.TreeSHA256
	return lease, nil
}

func catalogFixture(t *testing.T, index int) (*Supervisor, *catalogSource, *fixtureObserver) {
	t.Helper()
	old, control, observer := supervisorFixture(t)
	base := filepath.Dir(old.config.StateDir)
	common := filepath.Join(base, "control.pem")
	certificateFixture(t, common, time.Now().Add(time.Hour))
	digest, err := hashFile(common)
	if err != nil {
		t.Fatal(err)
	}
	entries := make([]ConfigurationSnapshot, 2)
	for i, name := range []string{"A", "B"} {
		dir := filepath.Join(base, "snapshot-"+name)
		if err = os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(dir, "radiusd.conf"), []byte("fixture-"+name), 0600); err != nil {
			t.Fatal(err)
		}
		tree, err := configurationDigest(dir, old.host.paths())
		if err != nil {
			t.Fatal(err)
		}
		hooks := old.config.Hooks
		hooks.ConfigurationSHA256 = tree
		material := api.NetworkRADIUSConfiguration{Reference: "snapshot-" + name, TreeHash: "sha256:" + tree, HooksAttested: true, EapCARef: "ca-" + name, EapServerCertificateRef: "cert-" + name, EapServerKeyRef: "key-" + name, NasSharedSecretRef: "secret-" + name}
		env := map[string]string{"SOHA_RADIUS_H1_FIXTURE": "true", "SOHA_RADIUS_NAS_ID": "nas-1", "SOHA_RADIUS_RUNTIME_ID": "runtime-1", "SOHA_RADIUS_CONTROL_URL": "https://control.invalid", "SOHA_RADIUS_CONTROL_CERT_FILE": common, "SOHA_RADIUS_CONTROL_KEY_FILE": common, "SOHA_RADIUS_CONTROL_CA_FILE": common}
		entries[i] = ConfigurationSnapshot{Material: material, Directory: dir, TreeSHA256: tree, Hooks: hooks, Environment: env, Certificates: []string{common}}
	}
	c := old.config
	c.ConfigurationDir, c.ConfigurationSHA256 = "", ""
	c.Hooks = HookAttestation{}
	c.Environment = nil
	c.Certificates = nil
	c.Materials = map[string]string{common: digest}
	c.ConfigurationCatalog = entries
	source := &catalogSource{base: control, entries: entries, index: index}
	s, err := newSupervisor(c, source, observer, old.host)
	if err != nil {
		t.Fatal(err)
	}
	s.host.peer = func(net.Conn) (uint32, int, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		return 101, s.childRecord.PID, nil
	}
	return s, source, observer
}

func startCatalogFixture(t *testing.T, index int) (*Supervisor, *catalogSource, *fixtureObserver, chan error) {
	t.Helper()
	s, source, observer := catalogFixture(t, index)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(4 * time.Second):
			if s.command != nil {
				_ = s.command.Process.Kill()
			}
			t.Error("catalog fixture cleanup timeout")
		}
	})
	deadline := time.After(8 * time.Second)
	for {
		select {
		case v := <-observer.observations:
			if v.HooksReady {
				return s, source, observer, done
			}
		case err := <-done:
			done <- err
			t.Fatal("catalog start failed", err)
		case <-deadline:
			t.Fatal("catalog startup timeout")
		}
	}
}

func TestCatalogSelectsAOrBBeforeExecuting(t *testing.T) {
	lastEpoch := ""
	for i, name := range []string{"A", "B"} {
		t.Run(name, func(t *testing.T) {
			s, source, _, _ := startCatalogFixture(t, i)
			entry := source.entries[i]
			if s.config.ConfigurationDir != entry.Directory || s.config.ConfigurationSHA256 != entry.TreeSHA256 || s.command.Args[3] != entry.Directory {
				t.Fatal("wrong approved snapshot executed")
			}
			if s.childRecord.Epoch == "" || s.childRecord.Epoch == lastEpoch {
				t.Fatal("incarnation epoch reused")
			}
			lastEpoch = s.childRecord.Epoch
		})
	}
}

func TestCatalogRuntimeChangeStopsAndWaitsOwnedChild(t *testing.T) {
	s, source, observer, done := startCatalogFixture(t, 0)
	source.mu.Lock()
	source.index = 1
	source.mu.Unlock()
	select {
	case err := <-done:
		if !errors.Is(err, ErrDenied) {
			t.Fatal(err)
		}
		done <- err
	case <-time.After(4 * time.Second):
		t.Fatal("changed configuration did not stop")
	}
	select {
	case <-s.done:
	default:
		t.Fatal("owned child wait missing")
	}
	for {
		select {
		case v := <-observer.observations:
			if v.ProcessStopped {
				if !v.State.Known || v.State.Active == nil || *v.State.Active != 0 {
					t.Fatal("invalid stopped observation")
				}
				return
			}
		default:
			t.Fatal("stop not observed")
		}
	}
}

func TestCatalogClientRequiresFullApprovedMaterial(t *testing.T) {
	for _, kind := range []string{"A", "B", "unknown-reference", "changed-key", "changed-hash"} {
		t.Run(kind, func(t *testing.T) {
			client, control := clientFixture(t)
			a := control.Configuration
			b := a
			b.Reference = "approved-B"
			b.TreeHash = "sha256:" + strings.Repeat("b", 64)
			client.config.AllowedConfigurations = []api.NetworkRADIUSConfiguration{a, b}
			switch kind {
			case "B":
				control.Configuration = b
			case "unknown-reference":
				control.Configuration.Reference = "unapproved"
			case "changed-key":
				control.Configuration.EapServerKeyRef = "other-key"
			case "changed-hash":
				control.Configuration.TreeHash = "sha256:" + strings.Repeat("c", 64)
			}
			lease, err := client.Fetch(context.Background(), "epoch-1")
			if (err == nil) != (kind == "A" || kind == "B") {
				t.Fatalf("wrong material admission: %v", err)
			}
			if err == nil && lease.Configuration != control.Configuration {
				t.Fatal("selected generated material omitted from lease")
			}
		})
	}
}

func TestCatalogRejectsAmbiguousBindingsAndUnsafeEitherSnapshot(t *testing.T) {
	for _, kind := range []string{"duplicate-ref", "tree-hash", "hook-hash", "mixed-legacy", "common-override", "unapproved-file", "unsafe-A", "unsafe-B"} {
		t.Run(kind, func(t *testing.T) {
			s, _, _ := catalogFixture(t, 0)
			data, _ := json.Marshal(s.config)
			var c Installation
			if err := json.Unmarshal(data, &c); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "duplicate-ref":
				c.ConfigurationCatalog[1].Material.Reference = c.ConfigurationCatalog[0].Material.Reference
			case "tree-hash":
				c.ConfigurationCatalog[1].Material.TreeHash = "sha256:" + strings.Repeat("f", 64)
			case "hook-hash":
				c.ConfigurationCatalog[1].Hooks.ConfigurationSHA256 = strings.Repeat("f", 64)
			case "mixed-legacy":
				c.ConfigurationDir = c.ConfigurationCatalog[0].Directory
			case "common-override":
				for path := range c.Materials {
					c.ConfigurationCatalog[1].Materials = map[string]string{path: strings.Repeat("f", 64)}
				}
			case "unapproved-file":
				c.ConfigurationCatalog[1].Environment["SOHA_RADIUS_SHARED_SECRET_CONFIG_FILE"] = filepath.Join(filepath.Dir(c.StateDir), "not-approved")
			case "unsafe-A", "unsafe-B":
				index := 0
				if kind == "unsafe-B" {
					index = 1
				}
				if err := os.Chmod(c.ConfigurationCatalog[index].Directory, 0770); err != nil { // #nosec G302 -- Deliberately writable fixture verifies that unsafe configuration snapshots are rejected.
					t.Fatal(err)
				}
			}
			if _, err := newSupervisor(c, s.source, s.observer, s.host); err == nil {
				t.Fatal("unsafe catalog accepted")
			}
		})
	}
}

func TestLegacyEnvironmentFileMustBeApproved(t *testing.T) {
	s, _, _ := supervisorFixture(t)
	s.config.Environment["SOHA_RADIUS_EXTRA_FILE"] = filepath.Join(filepath.Dir(s.config.StateDir), "outside")
	if validateInstallationWithPaths(s.config, s.host.paths()) == nil {
		t.Fatal("unapproved environment file accepted")
	}
}
