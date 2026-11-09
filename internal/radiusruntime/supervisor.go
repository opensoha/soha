package radiusruntime

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Installation is installer policy, never a workbench request. Its digest and
// hook attestation must be reviewed together with the immutable configuration.
type Installation struct {
	ConfigurationCatalog []ConfigurationSnapshot `json:"configurationCatalog,omitempty"`
	Owner                string                  `json:"owner"`
	StateDir             string                  `json:"stateDir"`
	Binary               string                  `json:"binary"`
	BinarySHA256         string                  `json:"binarySHA256"`
	ConfigurationDir     string                  `json:"configurationDir"`
	ConfigurationSHA256  string                  `json:"configurationSHA256"`
	Socket               string                  `json:"socket"`
	FreeradUID           uint32                  `json:"freeradUID"`
	FreeradGID           uint32                  `json:"freeradGID"`
	Hooks                HookAttestation         `json:"hooks"`
	Environment          map[string]string       `json:"environment"`
	Materials            map[string]string       `json:"materials"`
	Certificates         []string                `json:"certificates"`
	StopGraceSeconds     int                     `json:"stopGraceSeconds"`
	MaxStates            int                     `json:"maxStates"`
	MaxPending           int                     `json:"maxPending"`
}
type HookAttestation struct {
	ConfigurationSHA256 string   `json:"configurationSHA256"`
	Socket              string   `json:"socket"`
	Routes              []string `json:"routes"`
	NonceAttribute      string   `json:"nonceAttribute"`
	ControlOnly         bool     `json:"controlOnly"`
	EpochEnvironment    string   `json:"epochEnvironment"`
	SocketEnvironment   string   `json:"socketEnvironment"`
}

type ownerRecord struct {
	Owner      string `json:"owner"`
	PID        int    `json:"pid"`
	StartToken string `json:"startToken"`
	Epoch      string `json:"epoch"`
	Stopped    bool   `json:"stopped"`
}
type hostOps struct {
	lstat        func(string) (os.FileInfo, error)
	supported    func() bool
	ownership    func(os.FileInfo) (uint32, uint32, bool)
	lock         func(*os.File) error
	startToken   func(int) (string, error)
	peer         func(net.Conn) (uint32, int, error)
	configure    func(*exec.Cmd)
	chown        func(string, int, int) error
	pollInterval time.Duration
}

func nativeHost() hostOps {
	return hostOps{lstat: os.Lstat, supported: supportedHost, ownership: ownership, lock: lockOwner, startToken: processStartToken, peer: peerCredentials, configure: configureChild, chown: os.Chown, pollInterval: 5 * time.Second}
}

type Supervisor struct {
	stopOnce         sync.Once
	stopError        error
	selectedMaterial *ConfigurationSnapshot
	config           Installation
	host             hostOps
	source           ControlSource
	observer         Observer
	mu               sync.Mutex
	command          *exec.Cmd
	childRecord      ownerRecord
	tracker          *Tracker
	done             chan struct{}
	lock             *os.File
	server           *http.Server
	listener         *net.UnixListener
	socketInfo       os.FileInfo
	gate             sync.RWMutex
	leaseState       Lease
	observedNASUse   bool
	hookConnected    bool
	running          bool
}

func NewSupervisor(config Installation, source ControlSource, observer Observer) (*Supervisor, error) {
	return newSupervisor(config, source, observer, nativeHost())
}
func newSupervisor(config Installation, source ControlSource, observer Observer, host hostOps) (*Supervisor, error) {
	if source == nil || observer == nil || !host.supported() {
		return nil, ErrDenied
	}
	if host.lstat == nil {
		host.lstat = os.Lstat
	}
	if err := validateInstallationWithPaths(config, host.paths()); err != nil {
		return nil, err
	}
	return &Supervisor{config: config, source: source, observer: observer, host: host}, nil
}

var digestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func validateInstallation(c Installation) error {
	return validateInstallationWithPaths(c, nativePaths())
}
func validateInstallationWithPaths(c Installation, paths pathPolicy) error {
	if err := validateCoordinates(c, paths); err != nil {
		return err
	}
	if len(c.ConfigurationCatalog) == 0 {
		return validateSnapshot(c, paths)
	}
	if err := validateCatalogLayout(c); err != nil {
		return err
	}
	for _, entry := range c.ConfigurationCatalog {
		selected, err := snapshotInstallation(c, entry)
		if err != nil {
			return err
		}
		if err = validateSnapshot(selected, paths); err != nil {
			return err
		}
	}
	return nil
}
func validateCoordinates(c Installation, paths pathPolicy) error {
	if !identifier.MatchString(c.Owner) || !digestPattern.MatchString(c.BinarySHA256) || c.StopGraceSeconds < 1 || c.StopGraceSeconds > 5 || c.MaxStates < 1 || c.MaxStates > 65536 || c.MaxPending < 1 || c.MaxPending > 4096 {
		return ErrInvalid
	}
	for _, p := range []string{c.StateDir, c.Binary} {
		if err := paths.check(p); err != nil {
			return err
		}
	}
	if c.StateDir == "/" || !filepath.IsAbs(c.Socket) || filepath.Clean(c.Socket) != c.Socket || filepath.Dir(c.Socket) != c.StateDir || len(c.Socket) > 100 || c.StateDir == c.ConfigurationDir {
		return ErrInvalid
	}
	// StateDir is allowed to be the missing leaf. The socket is checked again
	// after its protected parent has actually been created.
	if _, err := paths.lstat(c.StateDir); err == nil {
		return paths.check(c.Socket)
	}
	return nil
}
func validateReferences(c Installation, paths pathPolicy) error {
	if len(c.Certificates) == 0 {
		return ErrInvalid
	}
	for path, digest := range c.Materials {
		if paths.check(path) != nil || !digestPattern.MatchString(digest) {
			return ErrInvalid
		}
	}
	for _, path := range c.Certificates {
		if paths.check(path) != nil {
			return ErrInvalid
		}
		if _, ok := c.Materials[path]; !ok && !strings.HasPrefix(path, c.ConfigurationDir+string(os.PathSeparator)) {
			return ErrDenied
		}
	}
	for key, path := range c.Environment {
		if !strings.HasSuffix(key, "_FILE") {
			continue
		}
		if paths.check(path) != nil {
			return ErrDenied
		}
		if _, ok := c.Materials[path]; !ok && !strings.HasPrefix(path, c.ConfigurationDir+string(os.PathSeparator)) {
			return ErrDenied
		}
	}
	return nil
}
func validateHooks(c Installation) error {
	h := c.Hooks
	routes := append([]string(nil), h.Routes...)
	sort.Strings(routes)
	if h.ConfigurationSHA256 != c.ConfigurationSHA256 || h.Socket != c.Socket || strings.Join(routes, ",") != "/admit,/challenge,/finish" || !h.ControlOnly || h.NonceAttribute != "control:Tmp-String-8" || h.EpochEnvironment != "SOHA_RADIUS_PROCESS_EPOCH" || h.SocketEnvironment != "SOHA_RADIUS_HOOK_SOCKET" {
		return ErrInvalid
	}
	return nil
}
func validateEnvironment(environment map[string]string) error {
	for key, value := range environment {
		if !strings.HasPrefix(key, "SOHA_RADIUS_") || strings.ContainsAny(key, "=\x00") || strings.ContainsRune(value, 0) || len(value) > 4096 {
			return ErrInvalid
		}
		if (strings.Contains(key, "SECRET") || strings.Contains(key, "PASSWORD") || strings.Contains(key, "TOKEN")) && !strings.HasSuffix(key, "_FILE") {
			return ErrInvalid
		}
	}
	return nil
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	sum := sha256.New()
	if _, err = io.Copy(sum, io.LimitReader(f, 128<<20)); err != nil {
		return "", err
	}
	info, err := f.Stat()
	if err != nil || info.Size() > 128<<20 {
		return "", ErrInvalid
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

// ConfigurationDigest binds relative names, modes and exact bytes of regular
// files. Symlinks, writable-by-others files and special files are not admissible.
func ConfigurationDigest(root string) (string, error) {
	return configurationDigest(root, nativePaths())
}
func configurationDigest(root string, paths pathPolicy) (string, error) {
	if err := paths.directory(root); err != nil {
		return "", err
	}
	sum := sha256.New()
	total := int64(0)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := paths.lstat(path)
		if err != nil {
			return err
		}
		if !paths.protected(info) {
			return ErrDenied
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			fmt.Fprintf(sum, "dir:%s:%o\n", relative, info.Mode().Perm())
			return nil
		}
		if !info.Mode().IsRegular() || info.Size() > 4<<20 {
			return ErrDenied
		}
		total += info.Size()
		if total > 16<<20 {
			return ErrDenied
		}
		digest, err := hashFile(path)
		if err != nil {
			return err
		}
		fmt.Fprintf(sum, "file:%s:%o:%s\n", relative, info.Mode().Perm(), digest)
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}
func (s *Supervisor) verifyArtifacts(ctx context.Context) error {
	if err := s.verifyFilesystemArtifacts(); err != nil {
		return err
	}
	c := s.config
	versionCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	command := exec.CommandContext(versionCtx, c.Binary, "-v")
	command.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin"}
	output := &boundedOutput{}
	command.Stdout = output
	command.Stderr = output
	if err := command.Run(); err != nil || !regexp.MustCompile(`(?m)FreeRADIUS(?: Version)? 3\.2\.10(?:[\s,]|$)`).Match(output.data) {
		return ErrDenied
	}
	return nil
}

func (s *Supervisor) verifyFilesystemArtifacts() error {
	c := s.config
	if err := s.verifyMaterials(); err != nil {
		return err
	}
	info, err := s.host.paths().regular(c.Binary)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 || info.Mode().Perm()&0111 == 0 {
		return ErrDenied
	}
	uid, _, ok := s.host.ownership(info)
	if !ok || uid != 0 {
		return ErrDenied
	}
	actual, err := hashFile(c.Binary)
	if err != nil || actual != c.BinarySHA256 {
		return ErrDenied
	}
	actual, err = configurationDigest(c.ConfigurationDir, s.host.paths())
	if err != nil || actual != c.ConfigurationSHA256 {
		return ErrDenied
	}
	err = filepath.WalkDir(c.ConfigurationDir, func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := e.Info()
		if err != nil {
			return err
		}
		uid, _, ok := s.host.ownership(info)
		if !ok || uid != 0 {
			return ErrDenied
		}
		return nil
	})
	if err != nil {
		return err
	}
	return nil
}

type boundedOutput struct{ data []byte }

func (b *boundedOutput) Write(data []byte) (int, error) {
	if len(b.data)+len(data) > 4096 {
		return 0, ErrInvalid
	}
	b.data = append(b.data, data...)
	return len(data), nil
}

func (s *Supervisor) acquire() error {
	c := s.config
	if err := s.establishDirectory(); err != nil {
		return err
	}
	lockPath := filepath.Join(c.StateDir, "owner.lock")
	if err := s.safePath(lockPath); err != nil {
		return err
	}
	file, err := os.OpenFile(lockPath, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return err
	}
	s.lock = file
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return ErrDenied
	}
	uid, _, ok := s.host.ownership(info)
	if !ok || uid != 0 {
		return ErrDenied
	}
	if err = s.host.lock(file); err != nil {
		return ErrDenied
	}
	recordPath := filepath.Join(c.StateDir, "child.json")
	if err = s.safePath(recordPath); err != nil {
		return err
	}
	data, err := os.ReadFile(recordPath)
	if err == nil {
		var old ownerRecord
		if json.Unmarshal(data, &old) != nil || old.Owner != c.Owner || !old.Stopped {
			return ErrDenied
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if _, err = os.Lstat(c.Socket); !errors.Is(err, os.ErrNotExist) {
		return ErrDenied
	}
	if err = s.host.chown(c.StateDir, 0, int(c.FreeradGID)); err != nil {
		return err
	}
	return os.Chmod(c.StateDir, 0710)
}

func (s *Supervisor) establishDirectory() error {
	path := s.config.StateDir
	if err := s.safePath(path); err != nil {
		return err
	}
	if err := os.Mkdir(path, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	if err := s.host.paths().directory(path); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0022 != 0 {
		return ErrDenied
	}
	uid, _, ok := s.host.ownership(info)
	if !ok || uid != 0 {
		return ErrDenied
	}
	marker := filepath.Join(path, "installation.json")
	if err = s.safePath(marker); err != nil {
		return err
	}
	data, err := os.ReadFile(marker)
	if err == nil {
		var owner struct {
			Owner string `json:"owner"`
		}
		if json.Unmarshal(data, &owner) != nil || owner.Owner != s.config.Owner {
			return ErrDenied
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	entries, err := os.ReadDir(path)
	if err != nil || len(entries) != 0 {
		return ErrDenied
	}
	data, err = json.Marshal(struct {
		Owner string `json:"owner"`
	}{s.config.Owner})
	if err != nil {
		return err
	}
	file, err := os.OpenFile(marker, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}
func (s *Supervisor) saveRecord() error {
	data, err := json.Marshal(s.childRecord)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(s.config.StateDir, ".child-")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if _, err = file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(name, filepath.Join(s.config.StateDir, "child.json"))
}
func (s *Supervisor) fetchLease(ctx context.Context, epoch string) (Lease, error) {
	call, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	lease, err := s.source.Fetch(call, epoch)
	if err != nil {
		return Lease{}, err
	}
	if lease.Control.Epoch != epoch || !lease.CertificateValidUntil.After(time.Now()) {
		return Lease{}, ErrDenied
	}
	return lease, nil
}
func (s *Supervisor) boundLease(lease Lease) (Lease, error) {
	certificateUntil, err := s.certificatesUntil()
	if err != nil {
		return Lease{}, err
	}
	lease.Control.ConfigValidUntil = earliest(lease.Control.ConfigValidUntil, lease.CertificateValidUntil)
	lease.Control.ConfigValidUntil = earliest(lease.Control.ConfigValidUntil, certificateUntil)
	lease.CertificateValidUntil = earliest(lease.CertificateValidUntil, certificateUntil)
	return lease, nil
}
func (s *Supervisor) lease(ctx context.Context, epoch string) (Lease, error) {
	lease, err := s.fetchLease(ctx, epoch)
	if err != nil {
		return Lease{}, err
	}
	if err = s.matchConfiguration(lease); err != nil {
		return Lease{}, err
	}
	return s.boundLease(lease)
}
func (s *Supervisor) start(ctx context.Context) error {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return err
	}
	epoch := hex.EncodeToString(raw)
	s.childRecord = ownerRecord{Owner: s.config.Owner, Epoch: epoch}
	lease, err := s.fetchLease(ctx, epoch)
	if err != nil {
		return err
	}
	if err = s.selectConfiguration(lease); err != nil {
		return err
	}
	if !lease.RunProcess {
		s.childRecord.Stopped = true
		if err = s.saveRecord(); err != nil {
			return err
		}
		if err = s.publish(ctx, true, "fresh_owner_no_child"); err != nil {
			return err
		}
		return nilStop
	}
	if err = s.verifyArtifacts(ctx); err != nil {
		return err
	}
	lease, err = s.boundLease(lease)
	if err != nil {
		return err
	}
	tracker, err := New(Options{Epoch: epoch, Binding: lease.Control.Binding, CertificateValidUntil: lease.CertificateValidUntil, HardUntil: lease.HardUntil, FreshChild: true, CompleteHooks: true, MaxStates: s.config.MaxStates, MaxPending: s.config.MaxPending, RequestTimeout: time.Second}, lease.Control)
	if err != nil {
		return err
	}
	s.tracker = tracker
	s.leaseState = lease
	s.childRecord = ownerRecord{Owner: s.config.Owner, Epoch: epoch}
	// This intent survives every post-start error, blocking orphan adoption.
	if err = s.saveRecord(); err != nil {
		return err
	}
	hook, err := s.openHook(tracker)
	if err != nil {
		return err
	}
	return s.launchChild(hook)
}
func (s *Supervisor) openHook(tracker *Tracker) (*HTTP, error) {
	hook, err := NewHTTP(tracker, s.config.FreeradUID, s.hookPeer, 32)
	if err != nil {
		return nil, err
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: s.config.Socket, Net: "unix"})
	if err != nil {
		return nil, err
	}
	listener.SetUnlinkOnClose(false)
	s.listener = listener
	if err = s.host.chown(s.config.Socket, 0, int(s.config.FreeradGID)); err != nil {
		return nil, err
	}
	if err = os.Chmod(s.config.Socket, 0660); err != nil {
		return nil, err
	}
	s.socketInfo, err = os.Lstat(s.config.Socket)
	if err != nil {
		return nil, err
	}
	return hook, nil
}
func (s *Supervisor) launchChild(hook *HTTP) error {
	var err error
	if err := s.verifyFilesystemArtifacts(); err != nil {
		return err
	}
	if err := s.safePath(s.config.Socket); err != nil {
		return err
	}
	command := exec.Command(s.config.Binary, "-f", "-d", s.config.ConfigurationDir)
	command.Env = s.environment(s.childRecord.Epoch)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	s.host.configure(command)
	if err := command.Start(); err != nil {
		return err
	}
	s.command = command
	s.done = make(chan struct{})
	s.mu.Lock()
	s.childRecord.PID = command.Process.Pid
	s.childRecord.StartToken, err = s.host.startToken(command.Process.Pid)
	s.mu.Unlock()
	go func() { _ = command.Wait(); close(s.done) }()
	if err != nil {
		return err
	}
	if err = s.saveRecord(); err != nil {
		return err
	}
	if err = s.configureHook(hook); err != nil {
		return err
	}
	s.server = hook.Server()
	go func() {
		if err := s.server.Serve(s.listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.tracker.MarkUnknown()
		}
	}()
	return nil
}
func (s *Supervisor) hookPeer(conn net.Conn) (uint32, error) {
	uid, pid, err := s.host.peer(conn)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil || pid != s.childRecord.PID || uid != s.config.FreeradUID {
		return 0, ErrDenied
	}
	s.hookConnected = true
	return uid, nil
}

func (s *Supervisor) environment(epoch string) []string {
	values := map[string]string{"PATH": "/usr/sbin:/usr/bin:/sbin:/bin"}
	for key, value := range s.config.Environment {
		values[key] = value
	}
	values["SOHA_RADIUS_MANAGED_ENABLED"] = "true"
	values["SOHA_RADIUS_HOOK_POOL_START"] = "1"
	values["SOHA_RADIUS_PROCESS_EPOCH"] = epoch
	values["SOHA_RADIUS_HOOK_SOCKET"] = s.config.Socket
	result := make([]string, 0, len(values))
	for key, value := range values {
		result = append(result, key+"="+value)
	}
	sort.Strings(result)
	return result
}
func (s *Supervisor) observation(stopped bool, reason string) Observation {
	s.mu.Lock()
	record := s.childRecord
	observedNASUse := s.observedNASUse
	hookConnected := s.hookConnected
	s.mu.Unlock()
	result := Observation{Epoch: record.Epoch, PID: record.PID, StartToken: record.StartToken, ConfigurationSHA256: s.config.ConfigurationSHA256, Version: "3.2.10", ProcessStopped: stopped, Reason: reason, ObservedNASUse: observedNASUse}
	if stopped {
		zero := 0
		if s.tracker != nil {
			result.State = s.tracker.Snapshot()
		}
		result.State.Epoch = record.Epoch
		result.State.Known = true
		result.State.Active = &zero
		result.State.Pending = 0
		result.State.Reason = ""
	} else if s.tracker != nil {
		result.State = s.tracker.Snapshot()
		result.HooksReady = result.State.Known && hookConnected
	}
	return result
}
func (s *Supervisor) publish(ctx context.Context, stopped bool, reason string) error {
	call, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return s.observer.Record(call, s.observation(stopped, reason))
}

func (s *Supervisor) Run(ctx context.Context) (result error) {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return ErrDenied
	}
	s.running = true
	s.mu.Unlock()
	defer func() {
		if err := s.close(); result == nil {
			result = err
		}
	}()
	if err := s.acquire(); err != nil {
		return err
	}
	if err := s.verifyMaterialFiles(s.config.Materials); err != nil {
		return err
	}
	if err := s.start(ctx); err != nil {
		if errors.Is(err, nilStop) {
			return nil
		}
		return err
	}
	defer func() {
		if err := s.stop(); err != nil {
			result = err
			return
		}
		publishCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := s.publish(publishCtx, true, "owned_child_wait_confirmed"); result == nil {
			result = err
		}
	}()
	err := s.monitor(ctx)
	if errors.Is(err, nilStop) {
		return nil
	}
	return err
}
func (s *Supervisor) monitor(ctx context.Context) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	poll := time.NewTimer(0)
	defer poll.Stop()
	for {
		select {
		case <-ctx.Done():
			s.tracker.MarkUnknown()
			return ctx.Err()
		case <-s.done:
			s.tracker.MarkUnknown()
			return fmt.Errorf("owned RADIUS child exited")
		case <-poll.C:
			if err := s.refresh(ctx); err != nil {
				s.tracker.MarkUnknown()
				return err
			}
			poll.Reset(s.host.pollInterval)
		case <-ticker.C:
			snapshot := s.tracker.Snapshot()
			if !snapshot.Known {
				return ErrUnknown
			}
			if snapshot.Draining && snapshot.Active != nil && *snapshot.Active == 0 {
				return nil
			}
		}
	}
}
func (s *Supervisor) refresh(ctx context.Context) error {
	lease, err := s.lease(ctx, s.childRecord.Epoch)
	if err != nil {
		return err
	}
	if !lease.RunProcess {
		return nilStop
	}
	if err = s.verifyMaterials(); err != nil {
		return err
	}
	actual, err := configurationDigest(s.config.ConfigurationDir, s.host.paths())
	if err != nil || actual != s.config.ConfigurationSHA256 {
		return ErrDenied
	}
	if err = s.applyLease(lease); err != nil {
		return err
	}
	return s.publish(ctx, false, "")
}
func (s *Supervisor) applyLease(lease Lease) error {
	if err := s.matchConfiguration(lease); err != nil {
		return err
	}
	s.gate.Lock()
	defer s.gate.Unlock()
	snapshot := s.tracker.Snapshot()
	if lease.Control.Binding != s.leaseState.Control.Binding {
		return ErrDenied
	}
	var err error
	if lease.Control.Revision > snapshot.Revision {
		err = s.tracker.UpdateControl(lease.Control)
	} else if lease.Control.Revision == snapshot.Revision {
		err = s.tracker.RenewControl(lease.Control)
	} else {
		return ErrDenied
	}
	if err != nil {
		return err
	}
	s.leaseState = lease
	return nil
}

var nilStop = errors.New("control requested process stop")

func (s *Supervisor) stop() error {
	s.stopOnce.Do(func() { s.stopError = s.stopOwned() })
	return s.stopError
}

func (s *Supervisor) stopOwned() error {
	if s.command == nil {
		return nil
	}
	if s.tracker != nil {
		s.tracker.MarkUnknown()
	}
	select {
	case <-s.done:
	default:
		token, err := s.host.startToken(s.childRecord.PID)
		if err != nil || token != s.childRecord.StartToken {
			select {
			case <-s.done:
				s.mu.Lock()
				s.childRecord.Stopped = true
				s.mu.Unlock()
				return s.saveRecord()
			case <-time.After(time.Second):
				return fmt.Errorf("owned process identity changed; stop unconfirmed")
			}
		}
		if err = s.command.Process.Signal(os.Interrupt); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return fmt.Errorf("owned process interrupt failed")
		}
		select {
		case <-s.done:
		case <-time.After(time.Duration(s.config.StopGraceSeconds) * time.Second):
			token, err = s.host.startToken(s.childRecord.PID)
			if err != nil || token != s.childRecord.StartToken {
				return ErrUnknown
			}
			if err = s.command.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
				return fmt.Errorf("owned process kill failed")
			}
			select {
			case <-s.done:
			case <-time.After(time.Second):
				return fmt.Errorf("owned process wait unconfirmed")
			}
		}
	}
	s.mu.Lock()
	s.childRecord.Stopped = true
	s.mu.Unlock()
	return s.saveRecord()
}
func (s *Supervisor) close() error {
	var result error
	if s.command != nil {
		if err := s.stop(); err != nil {
			result = err
		}
	}
	if s.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		if err := s.server.Shutdown(ctx); err != nil {
			_ = s.server.Close()
		}
		cancel()
	}
	if s.listener != nil {
		_ = s.listener.Close()
	}
	if s.socketInfo != nil {
		info, err := os.Lstat(s.config.Socket)
		if err == nil && os.SameFile(info, s.socketInfo) {
			if err = os.Remove(s.config.Socket); err != nil && result == nil {
				result = err
			}
		}
	}
	if s.lock != nil {
		if err := s.lock.Close(); err != nil && result == nil {
			result = err
		}
	}
	return result
}
