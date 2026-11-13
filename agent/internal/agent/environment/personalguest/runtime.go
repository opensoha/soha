package personalguest

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/opensoha/soha-agent/internal/agent/config"
	guest "github.com/opensoha/soha-contracts/agent/guest"
)

type runtimeLauncher struct {
	OperationID string `json:"operationId"`
	Digest      string `json:"digest"`
}
type runtimeActive struct {
	lease  string
	cancel context.CancelFunc
	done   chan struct{}
}

type runtimeBroker struct {
	cfg      config.PersonalGuestConfig
	plan     guest.ToolsPlan
	instance string
	ctx      context.Context
	page     *browserPipe
	mu       sync.Mutex
	active   map[string]runtimeActive
}

// The abstract socket has no filesystem cleanup. It carries only a frozen Core
// operation ID and digest, never caller-selected commands or paths.
func LaunchRuntime(ctx context.Context, dataRef, operationID, digest string, output io.Writer) error {
	if !dataUUIDRuntime(dataRef) || !dataUUIDRuntime(operationID) || len(digest) != 71 || !strings.HasPrefix(digest, "sha256:") {
		return errors.New("invalid runtime launcher identity")
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", "@soha-personal-guest-"+dataRef)
	if err != nil {
		return errors.New("personal guest runtime unavailable")
	}
	defer func() { _ = conn.Close() }()
	if err := conn.SetDeadline(time.Now().Add(65 * time.Second)); err != nil {
		return err
	}
	if err := json.NewEncoder(conn).Encode(runtimeLauncher{operationID, digest}); err != nil {
		return err
	}
	raw, err := io.ReadAll(io.LimitReader(conn, 320<<10+1))
	if err != nil || len(raw) > 320<<10 || !json.Valid(raw) {
		return errors.New("runtime outcome is unconfirmed; retain original operation")
	}
	var envelope struct {
		Result *guest.RuntimeResult `json:"result"`
		Error  string               `json:"error"`
	}
	if json.Unmarshal(raw, &envelope) != nil || envelope.Result == nil || envelope.Error != "" {
		return errors.New("personal runtime authority or isolation refused")
	}
	return json.NewEncoder(output).Encode(envelope.Result)
}

func dataUUIDRuntime(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && id != uuid.Nil && id.String() == value
}

func RunRuntime(ctx context.Context, cfg config.PersonalGuestConfig, toolsPath string) error {
	if toolsPath == "" {
		<-ctx.Done()
		return nil
	}
	plan, err := loadRuntimePlan(toolsPath)
	if err != nil || cfg.BindingID != plan.Binding.ID {
		return errors.New("untrusted personal runtime configuration")
	}
	b := &runtimeBroker{cfg: cfg, plan: plan, instance: uuid.NewString(), ctx: ctx, active: map[string]runtimeActive{}}
	defer func() {
		if b.page != nil {
			b.page.close()
		}
	}()
	listener, err := net.Listen("unix", "@soha-personal-guest-"+plan.Binding.DataRef)
	if err != nil {
		return errors.New("personal runtime already has an owner")
	}
	defer func() { _ = listener.Close() }()
	go func() { <-ctx.Done(); _ = listener.Close() }()
	var workers sync.WaitGroup
	defer workers.Wait()
	capacity := make(chan struct{}, 2)
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		select {
		case capacity <- struct{}{}:
		default:
			_ = conn.Close()
			continue
		}
		workers.Add(1)
		go func() { defer workers.Done(); defer func() { <-capacity; _ = conn.Close() }(); b.handle(ctx, conn) }()
	}
}

func (b *runtimeBroker) grant(ctx context.Context, id, digest string) (guest.RuntimeOperation, error) {
	var op guest.RuntimeOperation
	raw, err := config.ReadPrivateFile(b.cfg.StateFile, 16384)
	var current state
	if err != nil || decodeDataJSON(raw, &current) != nil || current.Binding == nil {
		return op, errors.New("current guest pin is unavailable")
	}
	cert := current.ActiveCertificatePEM
	if current.RenewalResult != nil {
		cert = current.RenewalResult.CertificatePEM
	}
	client, err := guestHTTPClientWithCertificate(b.cfg, cert)
	if err != nil {
		return op, err
	}
	defer client.CloseIdleConnections()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, b.cfg.Endpoint+guest.BindingsPath+"/"+b.cfg.BindingID+"/runtime-grants/"+id, nil)
	if err != nil {
		return op, err
	}
	response, err := client.Do(request)
	if err != nil {
		return op, errors.New("current runtime authority unavailable")
	}
	defer func() { _ = response.Body.Close() }()
	raw, err = io.ReadAll(io.LimitReader(response.Body, 320<<10+1))
	if err != nil || len(raw) > 320<<10 || response.StatusCode != http.StatusOK || response.Header.Get("Cache-Control") != "no-store" || decodeDataJSON(raw, &op) != nil || op.Validate(time.Now()) != nil || !op.Binding.SameWorkspace(b.plan.Binding) || op.ArtifactSHA256 != b.plan.ArtifactSHA256 {
		return op, errors.New("current runtime operation denied")
	}
	actual, err := op.Digest()
	if err != nil || actual != digest {
		return op, errors.New("frozen runtime request differs from launcher")
	}
	return op, nil
}

func (b *runtimeBroker) handle(ctx context.Context, conn net.Conn) {
	_ = conn.SetDeadline(time.Now().Add(65 * time.Second))
	raw, err := io.ReadAll(io.LimitReader(&newlineReader{reader: conn}, 1025))
	var launch runtimeLauncher
	if err != nil || len(raw) > 1024 || decodeDataJSON(raw, &launch) != nil || !dataUUIDRuntime(launch.OperationID) {
		return
	}
	op, err := b.grant(ctx, launch.OperationID, launch.Digest)
	if err != nil {
		_ = json.NewEncoder(conn).Encode(map[string]string{"error": "authority_denied"})
		return
	}
	workCtx, cancel := context.WithDeadline(ctx, op.Deadline)
	defer cancel()
	if op.Request.Action == "release" && b.cancelLease(workCtx, op.Request.Lease.ID) != nil {
		_ = json.NewEncoder(conn).Encode(map[string]string{"error": "cancellation_unconfirmed"})
		return
	}
	b.mu.Lock()
	if len(b.active) > 0 && op.Request.Action != "release" {
		b.mu.Unlock()
		_ = json.NewEncoder(conn).Encode(map[string]string{"error": "replay_or_busy"})
		return
	}
	if err := b.markDispatched(op, launch.Digest); err != nil {
		b.mu.Unlock()
		_ = json.NewEncoder(conn).Encode(map[string]string{"error": "replay_refused"})
		return
	}
	done := make(chan struct{})
	b.active[op.OperationID] = runtimeActive{lease: op.Request.Lease.ID, cancel: cancel, done: done}
	b.mu.Unlock()
	defer func() { b.mu.Lock(); delete(b.active, op.OperationID); close(done); b.mu.Unlock() }()
	stop := make(chan struct{})
	defer close(stop)
	go b.monitor(workCtx, cancel, launch, stop)
	result, err := b.execute(workCtx, op)
	if err != nil || result.ValidateFor(op) != nil {
		_ = json.NewEncoder(conn).Encode(map[string]string{"error": "runtime_unconfirmed"})
		return
	}
	_ = json.NewEncoder(conn).Encode(map[string]any{"result": result})
}

func (b *runtimeBroker) monitor(ctx context.Context, cancel context.CancelFunc, launch runtimeLauncher, stop <-chan struct{}) {
	ticker := time.NewTicker(8 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			checkCtx, finish := context.WithTimeout(ctx, 5*time.Second)
			_, err := b.grant(checkCtx, launch.OperationID, launch.Digest)
			finish()
			if err != nil {
				cancel()
				return
			}
		}
	}
}

func (b *runtimeBroker) cancelLease(ctx context.Context, lease string) error {
	b.mu.Lock()
	var waiting []chan struct{}
	for _, active := range b.active {
		if active.lease == lease {
			active.cancel()
			waiting = append(waiting, active.done)
		}
	}
	b.mu.Unlock()
	for _, done := range waiting {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-done:
		}
	}
	return nil
}

type newlineReader struct {
	reader io.Reader
	done   bool
}

func (r *newlineReader) Read(p []byte) (int, error) {
	if r.done {
		return 0, io.EOF
	}
	// Launcher frames are tiny; one-byte framing avoids consuming a second JSON object.
	var buf [1]byte
	n, err := r.reader.Read(buf[:])
	if n == 1 {
		p[0] = buf[0]
		if buf[0] == '\n' {
			r.done = true
		}
		return 1, nil
	}
	return n, err
}

func runtimeWorkspace(plan guest.ToolsPlan) string {
	return filepath.Join("/srv/soha-personal-data", plan.Binding.DataRef, "mount", "workspace")
}
