package networkproxyruntime

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	api "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"github.com/opensoha/soha-contracts/proxyadapter"
	domain "github.com/opensoha/soha/internal/domain/networkproxy"
	"github.com/opensoha/soha/internal/platform/securefile"
)

type adapterPolicy struct {
	proxyadapter.BinaryApproval
	EngineID     string `json:"engineId"`
	Dialect      string `json:"protectionDialect"`
	EngineSHA256 string `json:"engineSHA256"`
}

func (c Config) validateExtensions() error {
	if c.ManagedDeployment && !bindingID.MatchString(c.InstanceID) {
		return errors.New("managed proxy instance identity is required")
	}
	if c.Engine == domain.EngineExternal && !filepath.IsAbs(c.AdapterPolicyFile) {
		return errors.New("external adapter requires an installer policy file")
	}
	if c.IngressListen != "" && (c.IngressEndpointID == "" || c.IngressCertFile == "" || c.IngressKeyFile == "") {
		return errors.New("proxy TLS ingress requires approved endpoint and local TLS identity")
	}
	return nil
}

func readAdapterPolicy(c Config) (adapterPolicy, error) {
	var policy adapterPolicy
	raw, err := securefile.Read(c.AdapterPolicyFile, 16<<10, true)
	if err != nil {
		return policy, errors.New("adapter policy unavailable")
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&policy) != nil || decoder.Decode(new(any)) != io.EOF {
		return policy, errors.New("invalid adapter policy")
	}
	if policy.Dialect != domain.EngineMihomo && policy.Dialect != domain.EngineSingBox && policy.Dialect != domain.EngineV2Ray {
		return policy, errors.New("unverified adapter protection dialect")
	}
	if err := verifyApprovedBinary(policy.Binary, policy.SHA256); err != nil {
		return policy, err
	}
	if err := verifyApprovedBinary(c.EngineBinary, policy.EngineSHA256); err != nil {
		return policy, err
	}
	d := api.NetworkProxyEngineDescriptor{EngineID: policy.EngineID, AdapterID: policy.AdapterID, Version: policy.Version, ProtocolMajor: 1}
	if err := proxyadapter.ValidateDescriptor(d); err != nil {
		return policy, err
	}
	return policy, nil
}

// Symlinks and group/world writable executables cannot satisfy an installation
// pin. Read and hash again immediately before each spawn, never download code.
func verifyApprovedBinary(path, digest string) error {
	if !filepath.IsAbs(path) || !configurationDigestPattern.MatchString(digest) {
		return errors.New("adapter binary approval is invalid")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 || info.Mode().Perm()&0111 == 0 || !approvedBinaryOwner(info) {
		return errors.New("adapter binary permissions are invalid")
	}
	file, err := os.Open(path) // #nosec G304 -- absolute installer path; Lstat rejects symlinks, unsafe mode and unapproved owners; bytes must match pin.
	if err != nil {
		return errors.New("adapter binary unavailable")
	}
	defer func() { _ = file.Close() }()
	hash := sha256.New()
	if _, err := io.Copy(hash, io.LimitReader(file, 128<<20)); err != nil || info.Size() > 128<<20 || fmt.Sprintf("sha256:%x", hash.Sum(nil)) != digest {
		return errors.New("adapter binary digest mismatch")
	}
	return nil
}

type stdioSession struct {
	mu          sync.Mutex
	command     *exec.Cmd
	input       io.WriteCloser
	output      *bufio.Reader
	done        chan struct{}
	fence       *ownedProcessFence
	closed      bool
	stderrBytes int64
}

// stderr is drained and counted, never retained or logged: even a trusted local
// adapter can accidentally print upstream credentials. Bound the accepted budget.
func (s *stdioSession) Write(p []byte) (int, error) {
	s.stderrBytes += int64(len(p))
	if s.stderrBytes > proxyadapter.MaxStderrBytes {
		return 0, errors.New("adapter stderr limit exceeded")
	}
	return len(p), nil
}

func startStdio(policy adapterPolicy) (*stdioSession, error) {
	if err := verifyApprovedBinary(policy.Binary, policy.SHA256); err != nil {
		return nil, err
	}
	command := exec.Command(policy.Binary) // #nosec G204 -- installer pinned absolute binary, no caller arguments or shell.
	// Do not inherit Core credentials, tokens, certificate paths or proxy env.
	command.Env = []string{"PATH=/usr/bin:/bin", "LANG=C"}
	configureOwnedEngineProcess(command)
	s := &stdioSession{command: command, done: make(chan struct{}), fence: &ownedProcessFence{command: command}}
	var err error
	s.input, err = command.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := command.StdoutPipe()
	if err != nil {
		_ = s.input.Close()
		return nil, err
	}
	s.output = bufio.NewReaderSize(out, proxyadapter.MaxFrameBytes+1)
	command.Stderr = s
	command.WaitDelay = 500 * time.Millisecond
	if err := command.Start(); err != nil {
		_ = s.input.Close()
		_ = out.Close()
		return nil, errors.New("adapter start failed")
	}
	go func() { _ = s.fence.wait(); close(s.done) }()
	return s, nil
}

func (s *stdioSession) abort() {
	if s.closed {
		return
	}
	s.closed = true
	_ = s.input.Close()
	_ = s.fence.signal(true)
}

func (s *stdioSession) call(ctx context.Context, epoch string, method proxyadapter.Method, payload proxyadapter.Payload) (*proxyadapter.Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, errors.New("adapter stopped")
	}
	deadline := time.Now().Add(10 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	request := proxyadapter.Frame{Major: 1, RequestID: rand.Text(), ProcessEpoch: epoch, Deadline: deadline, Method: method, Payload: &payload}
	raw, err := proxyadapter.EncodeFrame(request, time.Now())
	if err != nil {
		return nil, err
	}
	type reply struct {
		line []byte
		err  error
	}
	done := make(chan reply, 1)
	go func() {
		if _, err := s.input.Write(raw); err != nil {
			done <- reply{err: err}
			return
		}
		line, err := s.output.ReadSlice('\n')
		done <- reply{line: append([]byte(nil), line...), err: err}
	}()
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		s.abort()
		return nil, ctx.Err()
	case <-timer.C:
		s.abort()
		return nil, errors.New("adapter deadline exceeded")
	case value := <-done:
		if value.err != nil {
			s.abort()
			return nil, errors.New("adapter framing failed")
		}
		response, err := proxyadapter.DecodeFrame(value.line, time.Now())
		if err != nil || proxyadapter.MatchResponse(request, response, payload.Challenge, time.Now()) != nil {
			s.abort()
			return nil, errors.New("adapter response rejected")
		}
		if response.Error != nil {
			return nil, fmt.Errorf("adapter %s", response.Error.Code)
		}
		return response.Result, nil
	}
}

type externalEngineAdapter struct {
	lastSequence  int64
	engineAdapter // Independently verified local protection dialect control.
	policy        adapterPolicy
	session       *stdioSession
	epoch         string
	descriptor    *api.NetworkProxyEngineDescriptor
	prepared      proxyadapter.Payload
}

func (e *engine) dialect() string {
	if a, ok := e.adapter.(*externalEngineAdapter); ok {
		return a.policy.Dialect
	}
	return e.config.Engine
}

func (e *engine) configureExternal() error {
	if e.config.Engine != domain.EngineExternal {
		return nil
	}
	policy, err := readAdapterPolicy(e.config)
	if err != nil {
		return err
	}
	c := e.config
	c.Engine = policy.Dialect
	if c.Engine == domain.EngineV2Ray {
		e.controller = nil
	}
	e.adapter = &externalEngineAdapter{engineAdapter: newBuiltinEngineAdapter(c, "", e.controller), policy: policy}
	// Rendering must use the same operator secret as the host controller.
	if a, ok := e.adapter.(*externalEngineAdapter).engineAdapter.(*clashEngineAdapter); ok {
		a.secret = e.controller.secret
	}
	return nil
}

func (e *engine) prepareExternal(ctx context.Context, desired domain.Configuration, raw []byte) error {
	a, ok := e.adapter.(*externalEngineAdapter)
	if !ok {
		return nil
	}
	if desired.EngineDescriptor == nil || desired.EngineDescriptor.EngineID != a.policy.EngineID || desired.EngineDescriptor.AdapterID != a.policy.AdapterID || desired.EngineDescriptor.Version != a.policy.Version {
		return errors.New("adapter desired identity mismatch")
	}
	if err := verifyApprovedBinary(e.config.EngineBinary, a.policy.EngineSHA256); err != nil {
		return err
	}
	session, err := startStdio(a.policy)
	if err != nil {
		return err
	}
	a.session, a.epoch, a.lastSequence = session, e.processEpoch, 0
	result, err := session.call(ctx, a.epoch, proxyadapter.Handshake, proxyadapter.Payload{Challenge: rand.Text() + rand.Text()})
	if err != nil {
		return err
	}
	d := result.Descriptor
	if d == nil || d.EngineID != a.policy.EngineID || d.AdapterID != a.policy.AdapterID || d.Version != a.policy.Version {
		return errors.New("adapter handshake identity mismatch")
	}
	a.descriptor = d
	hash := fmt.Sprintf("sha256:%x", sha256.Sum256(raw))
	a.prepared = proxyadapter.Payload{Revision: &desired.Revision, ContentHash: hash, Content: string(raw), ValidUntil: &desired.ValidUntil}
	_, err = session.call(ctx, a.epoch, proxyadapter.Prepare, a.prepared)
	return err
}

func (e *engine) applyExternal(ctx context.Context) error {
	a, ok := e.adapter.(*externalEngineAdapter)
	if !ok {
		return nil
	}
	result, err := a.session.call(ctx, a.epoch, proxyadapter.Apply, a.prepared)
	if err != nil {
		return err
	}
	if result.Revision == nil || *result.Revision != *a.prepared.Revision || result.ReadbackHash != e.fileHash {
		return errors.New("adapter readback mismatch")
	}
	return nil
}

func (a *externalEngineAdapter) observe(ctx context.Context) (domain.Observation, []domain.Connection) {
	local, connections := a.engineAdapter.observe(ctx)
	if a.session == nil || local.Health != "healthy" {
		return local, connections
	}
	result, err := a.session.call(ctx, a.epoch, proxyadapter.Observe, proxyadapter.Payload{})
	if err != nil || result.Observation.Freshness != "fresh" || result.Observation.SampleSequence <= a.lastSequence {
		local.Health, local.ReasonCode = "degraded", "adapter_observation_unverified"
		local.Capabilities = []string{}
		return local, nil
	}
	a.lastSequence = result.Observation.SampleSequence
	if !a.supportsClose() {
		local.Capabilities = slices.DeleteFunc(local.Capabilities, func(c string) bool { return c == "close_connection" })
	}
	// Sample facts and security capabilities come from independently verified host control, not adapter claims.
	return local, connections
}

func (a *externalEngineAdapter) close(ctx context.Context, id string) (string, error) {
	if a.session == nil {
		return "failed", errors.New("adapter unavailable")
	}
	now := time.Now().UTC()
	until := now.Add(10 * time.Second)
	if deadline, ok := ctx.Deadline(); ok && deadline.Before(until) {
		until = deadline
	}
	result, err := a.session.call(ctx, a.epoch, proxyadapter.Close, proxyadapter.Payload{ConnectionID: id, IssuedAt: &now, ExpiresAt: &until})
	if err != nil {
		return "failed", err
	}
	local, connections := a.engineAdapter.observe(ctx)
	if local.Health != "healthy" || local.ActiveConnections == nil || *local.ActiveConnections > len(connections) {
		return "failed", errors.New("adapter close readback unavailable")
	}
	for _, c := range connections {
		if c.ID == id {
			return "failed", errors.New("adapter close not observed")
		}
	}
	return result.CloseStatus, nil
}

func (a *externalEngineAdapter) health(ctx context.Context) error {
	if a.session == nil {
		return errors.New("adapter unavailable")
	}
	a.session.mu.Lock()
	closed := a.session.closed
	a.session.mu.Unlock()
	if closed {
		return errors.New("adapter stopped")
	}
	select {
	case <-a.session.done:
		return errors.New("adapter exited")
	default:
	}
	return a.engineAdapter.health(ctx)
}

func (a *externalEngineAdapter) supportsClose() bool {
	return a.descriptor != nil && a.engineAdapter.supportsClose() && slices.Contains(a.descriptor.Capabilities, "close_connection")
}
