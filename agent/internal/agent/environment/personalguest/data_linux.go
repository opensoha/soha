//go:build linux

package personalguest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/gofrs/flock"
	"github.com/google/uuid"
	"github.com/opensoha/soha-agent/internal/agent/config"
)

const personalDataRoot = "/var/lib/soha/personal-guest-data"
const personalDataMountRoot = "/srv/soha-personal-data"

var dataUUID = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)

// InitializeData accepts only the operator's root-owned plan below a fixed root.
// It does not expose root execution to the guest, SSH user or model.
func InitializeData(ctx context.Context, planPath string) (DataResult, error) {
	if os.Geteuid() != 0 || filepath.Clean(planPath) != planPath || filepath.Dir(planPath) != personalDataRoot {
		return DataResult{}, errors.New("personal data initialization requires an operator-owned root plan")
	}
	if err := rootDataPath(planPath, false); err != nil {
		return DataResult{}, err
	}
	raw, err := config.ReadPrivateFile(planPath, 8192)
	if err != nil {
		return DataResult{}, err
	}
	var plan DataPlan
	if err := decodeDataJSON(raw, &plan); err != nil {
		return DataResult{}, err
	}
	if err := plan.Validate(time.Now().UTC()); err != nil {
		return DataResult{}, err
	}
	if filepath.Base(planPath) != plan.OperationID+".plan.json" {
		return DataResult{}, errors.New("personal data plan filename differs from its operation")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	for _, tool := range []string{"/usr/bin/lsblk", "/usr/bin/findmnt", "/usr/sbin/wipefs", "/usr/sbin/mkfs.xfs", "/usr/sbin/xfs_quota", "/usr/bin/mount"} {
		if err := rootDataPath(tool, false); err != nil {
			return DataResult{}, errors.New("personal data initialization tools are unavailable or untrusted")
		}
	}
	client, err := rootDataClient(plan.Binding.DataRef)
	if err != nil {
		return DataResult{}, err
	}
	defer client.http.CloseIdleConnections()
	root, err := os.OpenRoot(personalDataRoot)
	if err != nil {
		return DataResult{}, err
	}
	defer func() { _ = root.Close() }()
	lockName := plan.Binding.DataRef + ".lock"
	file, err := root.OpenFile(lockName, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err == nil {
		_ = file.Close()
	} else if !errors.Is(err, os.ErrExist) {
		return DataResult{}, err
	}
	if err := rootDataPath(filepath.Join(personalDataRoot, lockName), false); err != nil {
		return DataResult{}, err
	}
	lock := flock.New(filepath.Join(personalDataRoot, lockName))
	locked, err := lock.TryLock()
	if err != nil || !locked {
		return DataResult{}, errors.New("another initialization owns this personal data disk")
	}
	defer func() { _ = lock.Unlock() }()
	store := rootDataStore{root: root, name: plan.Binding.DataRef + ".json"}
	initializer := dataInitializer{run: runDataCommand, authorize: client.authorizeData, load: store.load, save: store.save, mkdir: makeRootDataDirectory, base: personalDataMountRoot, now: time.Now}
	result, err := initializer.initialize(ctx, plan)
	if err != nil {
		return DataResult{}, err
	}
	if err := exposeDataWorkspace(plan.Binding.DataRef); err != nil {
		return DataResult{}, err
	}
	return result, nil
}

func exposeDataWorkspace(dataRef string) error {
	mount := filepath.Join(personalDataMountRoot, dataRef, "mount")
	for _, path := range []string{personalDataMountRoot, filepath.Dir(mount), mount} {
		if err := rootDataPath(path, true); err != nil {
			return err
		}
		if err := os.Chmod(path, 0711); err != nil { //nolint:gosec // Root-owned directory permits traversal only; workspace is 0700 and metadata stays separate.
			return err
		}
	}
	path := filepath.Join(mount, "workspace")
	err := os.Mkdir(path, 0700)
	if err == nil {
		return os.Chown(path, 1000, 1000)
	}
	if !errors.Is(err, os.ErrExist) {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 1000 || stat.Gid != 1000 || !info.IsDir() || info.Mode().Perm() != 0700 {
		return errors.New("personal data workspace ownership changed")
	}
	return nil
}

func rootDataClient(dataRef string) (*Client, error) {
	path := filepath.Join(personalDataRoot, dataRef+".guest.json")
	if err := rootDataPath(path, false); err != nil {
		return nil, err
	}
	cfg, err := config.LoadPersonalGuest(path)
	if err != nil {
		return nil, err
	}
	for _, path := range []string{cfg.CertificateFile, cfg.KeyFile, cfg.CAFile} {
		if err := rootDataPath(path, false); err != nil {
			return nil, err
		}
	}
	certificate := ""
	statePath := filepath.Join("/var/lib/soha/personal-guests", dataRef, "state-"+cfg.BindingID+".json")
	if raw, readErr := config.ReadPrivateFile(statePath, 16384); readErr == nil {
		var current state
		if decodeDataJSON(raw, &current) != nil {
			return nil, errors.New("invalid managed guest renewal state")
		}
		certificate = current.ActiveCertificatePEM
		if current.RenewalResult != nil {
			certificate = current.RenewalResult.CertificatePEM
		}
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return nil, readErr
	}
	client, err := guestHTTPClientWithCertificate(cfg, certificate)
	if err != nil {
		return nil, err
	}
	// This is a read-only authority check; it neither enrolls nor owns heartbeat state.
	return &Client{cfg: cfg, http: client}, nil
}

// ReconcileData cannot format. A protected completed receipt and a current
// Core data lease are both required; expired identities remain fail closed.
func ReconcileData(ctx context.Context, dataRef string) (DataResult, error) {
	if os.Geteuid() != 0 || !dataUUID.MatchString(dataRef) {
		return DataResult{}, errors.New("managed data reconciliation requires root and a data UUID")
	}
	root, err := os.OpenRoot(personalDataRoot)
	if err != nil {
		return DataResult{}, err
	}
	defer func() { _ = root.Close() }()
	store := rootDataStore{root: root, name: dataRef + ".json"}
	receipt, found, err := store.load()
	if err != nil || !found || receipt.Stage != "complete" || receipt.FilesystemID != dataRef {
		return DataResult{}, errors.New("data reconciliation requires a completed protected receipt")
	}
	lock := flock.New(filepath.Join(personalDataRoot, dataRef+".lock"))
	if err := rootDataPath(filepath.Join(personalDataRoot, dataRef+".lock"), false); err != nil {
		return DataResult{}, err
	}
	locked, err := lock.TryLock()
	if err != nil || !locked {
		return DataResult{}, errors.New("data initialization or reconciliation is already running")
	}
	defer func() { _ = lock.Unlock() }()
	var client *Client
	var plan DataPlan
	for attempt := 0; attempt < 30; attempt++ {
		client, err = rootDataClient(dataRef)
		if err == nil {
			status, raw, callErr := client.call(ctx, http.MethodGet, "/data-lease", nil, "")
			if callErr == nil && status == http.StatusOK && json.Unmarshal(raw, &plan) == nil && plan.Validate(time.Now()) == nil && plan.Binding.DataRef == dataRef {
				break
			}
			client.http.CloseIdleConnections()
			client = nil
		}
		select {
		case <-ctx.Done():
			return DataResult{}, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	if client == nil {
		return DataResult{}, errors.New("current initialized data lease is unavailable")
	}
	defer client.http.CloseIdleConnections()
	authorize := func(ctx context.Context, expected DataPlan) error {
		status, raw, err := client.call(ctx, http.MethodGet, "/data-lease", nil, "")
		var current DataPlan
		if err != nil || status != http.StatusOK || json.Unmarshal(raw, &current) != nil || current != expected {
			return errors.New("current initialized data lease changed")
		}
		return nil
	}
	initializer := dataInitializer{run: runDataCommand, authorize: authorize, load: store.load, save: store.save, mkdir: makeRootDataDirectory, base: personalDataMountRoot, now: time.Now}
	result, err := initializer.initialize(ctx, plan)
	if err != nil {
		return DataResult{}, err
	}
	if err := exposeDataWorkspace(dataRef); err != nil {
		return DataResult{}, err
	}
	return result, nil
}

func rootDataPath(path string, directory bool) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("personal data path must be absolute and canonical")
	}
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != 0 || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0022 != 0 {
			return errors.New("personal data path must be root-owned and protected from replacement")
		}
		if current == path && !directory && !info.Mode().IsRegular() || (current != path || directory) && !info.IsDir() {
			return errors.New("unexpected personal data path type")
		}
		if current == "/" {
			return nil
		}
	}
}

func makeRootDataDirectory(path string) error {
	if !strings.HasPrefix(path, personalDataMountRoot+"/") {
		return errors.New("personal data directory escaped its fixed root")
	}
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	if err := rootDataPath(path, true); err != nil {
		return err
	}
	entries, err := os.ReadDir(path)
	if err != nil || len(entries) != 0 {
		return errors.New("personal data mount target is not an empty protected directory")
	}
	return nil
}

type rootDataStore struct {
	root *os.Root
	name string
}

func (s rootDataStore) load() (dataReceipt, bool, error) {
	path := filepath.Join(personalDataRoot, s.name)
	if err := rootDataPath(path, false); errors.Is(err, os.ErrNotExist) {
		return dataReceipt{}, false, nil
	} else if err != nil {
		return dataReceipt{}, false, err
	}
	raw, err := config.ReadPrivateFile(path, 8192)
	if err != nil {
		return dataReceipt{}, false, err
	}
	var receipt dataReceipt
	if err := decodeDataJSON(raw, &receipt); err != nil {
		return dataReceipt{}, false, err
	}
	return receipt, true, nil
}

func (s rootDataStore) save(receipt dataReceipt) error {
	raw, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	name := s.name + ".next-" + uuid.NewString()
	file, err := s.root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	if _, err := file.Write(raw); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := s.root.Rename(name, s.name); err != nil {
		return err
	}
	directory, err := s.root.Open(".")
	if err != nil {
		return err
	}
	defer func() { _ = directory.Close() }()
	return directory.Sync()
}

type dataOutput struct {
	bytes.Buffer
	remaining int
}

func (w *dataOutput) Write(raw []byte) (int, error) {
	if len(raw) > w.remaining {
		return 0, errors.New("personal data tool output exceeded its limit")
	}
	w.remaining -= len(raw)
	return w.Buffer.Write(raw)
}

func runDataCommand(ctx context.Context, tool string, args ...string) ([]byte, error) {
	switch tool {
	case "/usr/bin/lsblk", "/usr/bin/findmnt", "/usr/sbin/wipefs", "/usr/sbin/mkfs.xfs", "/usr/sbin/xfs_quota", "/usr/bin/mount":
	default:
		return nil, errors.New("personal data tool is not permitted")
	}
	if err := rootDataPath(tool, false); err != nil {
		return nil, errors.New("personal data tool is not root-owned and protected")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, tool, args...) //nolint:gosec // Only fixed, root-verified tools; plans carry no executable or shell input.
	command.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LC_ALL=C"}
	output := &dataOutput{remaining: 1 << 20}
	command.Stdout = output
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		return nil, errors.New("personal data tool failed or timed out; retain the original operation")
	}
	return output.Bytes(), nil
}
