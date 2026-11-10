package docker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	protocol "github.com/opensoha/soha-contracts/docker/runtime"
	"github.com/opensoha/soha-contracts/resource"
	"github.com/opensoha/soha-contracts/resource/terminalstream"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domain "github.com/opensoha/soha/internal/domain/docker"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type nativeLeaseMemory struct {
	mu            sync.Mutex
	items         map[string]domain.NativeTerminalRecord
	finishACKLost bool
}

func (r *nativeLeaseMemory) Create(_ context.Context, v domain.NativeTerminalRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items[v.Lease.ID] = v
	return nil
}
func (r *nativeLeaseMemory) Get(_ context.Context, actor, id string) (domain.NativeTerminalRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.items[id]
	if !ok || v.ActorID != actor {
		return domain.NativeTerminalRecord{}, apperrors.ErrNotFound
	}
	return v, nil
}
func (r *nativeLeaseMemory) Consume(_ context.Context, actor, id, cred, ticket string) (domain.NativeTerminalRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v := r.items[id]
	if v.ActorID != actor || v.CredentialHash != cred || v.TicketHash != ticket || v.Lease.Status != "pending" || !v.Lease.TicketExpiresAt.After(time.Now()) {
		return domain.NativeTerminalRecord{}, apperrors.ErrConflict
	}
	v.Lease.Status = "active"
	v.TicketHash = ""
	r.items[id] = v
	return v, nil
}
func (r *nativeLeaseMemory) RequestClose(_ context.Context, actor, id string) (domain.NativeTerminalRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v := r.items[id]
	if v.ActorID != actor {
		return v, apperrors.ErrNotFound
	}
	if v.Lease.Status == "active" {
		v.Lease.Status = "closing"
	}
	r.items[id] = v
	return v, nil
}
func (r *nativeLeaseMemory) Finish(_ context.Context, v domain.NativeTerminalRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.items[v.Lease.ID].Lease.Status != "active" && r.items[v.Lease.ID].Lease.Status != "closing" {
		return apperrors.ErrConflict
	}
	r.items[v.Lease.ID] = v
	if r.finishACKLost {
		return apperrors.ErrServiceUnavailable
	}
	return nil
}

type nativeLeaseIdentity struct{ revoked atomic.Bool }

func (r *nativeLeaseIdentity) CurrentStreamingPrincipal(_ context.Context, p domainidentity.Principal, a domainidentity.AccessContext) (domainidentity.Principal, error) {
	if r.revoked.Load() || !a.ExpiresAt.After(time.Now()) {
		return p, apperrors.ErrUnauthorized
	}
	return p, nil
}

type nativeNoResize struct{}

func (nativeNoResize) Next(ctx context.Context) *resource.NativeTerminalSize {
	<-ctx.Done()
	return nil
}

func nativeDockerFixture(t *testing.T, endpoint string) (*NativeTerminals, *nativeLeaseMemory, *nativeLeaseIdentity, *readFenceRepo, domainidentity.Principal, domainidentity.AccessContext) {
	t.Helper()
	s, registry, actor := boundReadFixture(endpoint)
	s.permissions = appaccess.NewPermissionResolver(dockerTestRoleReader{matrix: map[string][]string{"reader": {"docker.services.terminal"}}})
	h := registry.hosts["host-1"]
	h.ConfigurationRevision = "host-rev"
	registry.hosts[h.ID] = h
	p := registry.projects["project-1"]
	p.ConfigurationRevision = "project-rev"
	registry.projects[p.ID] = p
	repo := &nativeLeaseMemory{items: map[string]domain.NativeTerminalRecord{}}
	identity := &nativeLeaseIdentity{}
	service, err := NewNativeTerminals(s, repo, identity, t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := service.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	})
	return service, repo, identity, registry, actor, domainidentity.AccessContext{SessionID: "original-session", ExpiresAt: time.Now().Add(time.Minute)}
}
func nativeDockerCaps(w http.ResponseWriter) {
	_ = json.NewEncoder(w).Encode(map[string]any{"data": protocol.TerminalCapabilities{Protocol: protocol.TerminalProtocol, NativePTY: true, MaxTimeoutSeconds: 900, MaxInputBytes: 1 << 20, MaxOutputBytes: 32 << 20}})
}

func TestNativeDockerTerminalOriginalCredentialAndTargetFence(t *testing.T) {
	for _, change := range []string{"session", "key", "container", "host-revision", "project-revision", "endpoint", "revoked", "permission"} {
		t.Run(change, func(t *testing.T) {
			agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { nativeDockerCaps(w) }))
			defer agent.Close()
			s, repo, identity, registry, actor, access := nativeDockerFixture(t, agent.URL)
			input := protocol.TerminalInput{ExpectedContainerID: strings.Repeat("a", 64), ServiceName: "web"}
			created, err := s.Create(t.Context(), actor, access, "project-1", input)
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "session":
				access.SessionID = "another-session"
			case "key":
				access.SessionID = ""
				actor.AccessTokenID = "another-key"
			case "container":
				v := registry.services["service-1"]
				v.ContainerID = strings.Repeat("b", 64)
				registry.services[v.ID] = v
			case "host-revision":
				v := registry.hosts["host-1"]
				v.ConfigurationRevision = "changed"
				registry.hosts[v.ID] = v
			case "project-revision":
				v := registry.projects["project-1"]
				v.ConfigurationRevision = "changed"
				registry.projects[v.ID] = v
			case "endpoint":
				v := registry.hosts["host-1"]
				v.Endpoint = agent.URL + "/other"
				registry.hosts[v.ID] = v
			case "revoked":
				identity.revoked.Store(true)
			case "permission":
				actor.Roles = nil
			}
			if session, err := s.Acquire(t.Context(), actor, access, "project-1", created.Session.ID, created.Ticket); err == nil {
				_ = session.Close()
				t.Fatal("changed binding admitted")
			}
			stored, _ := repo.Get(t.Context(), "reader", created.Session.ID)
			if stored.Lease.Status != "pending" {
				t.Fatal("failed preflight consumed ticket")
			}
		})
	}
}

func TestNativeDockerTerminalOneStreamAndFinishACKLoss(t *testing.T) {
	var starts atomic.Int32
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/docker/runtime/v1/terminal/capabilities" {
			nativeDockerCaps(w)
			return
		}
		starts.Add(1)
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		stream := terminalstream.NewServer(r.Context(), conn, resource.NativeTerminalSize{Width: 80, Height: 24})
		defer func() { _ = stream.Close() }()
		if stream.SendReady() != nil {
			return
		}
		n, _ := io.Copy(stream, stream.Input())
		code := int64(7)
		_ = stream.SendExit(protocol.TerminalResult{Status: "failed", ExitCode: &code, UIDVerifiedBeforeStart: true, InputBytes: n, OutputBytes: n})
	}))
	defer agent.Close()
	s, repo, _, _, actor, access := nativeDockerFixture(t, agent.URL)
	repo.finishACKLost = true
	created, err := s.Create(t.Context(), actor, access, "project-1", protocol.TerminalInput{ExpectedContainerID: strings.Repeat("a", 64), ServiceName: "web"})
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.Acquire(t.Context(), actor, access, "project-1", created.Session.ID, created.Ticket)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.Close() }()
	if duplicate, err := s.Acquire(t.Context(), actor, access, "project-1", created.Session.ID, created.Ticket); !errors.Is(err, apperrors.ErrConflict) {
		if duplicate != nil {
			_ = duplicate.Close()
		}
		t.Fatal("ticket replay admitted", err)
	}
	var output strings.Builder
	result, err := session.Stream(io.NopCloser(strings.NewReader("exact-input")), &output, nativeNoResize{})
	if err != nil || result.Status != "failed" || result.ExitCode == nil || *result.ExitCode != 7 || output.String() != "exact-input" {
		t.Fatal(result, err, output.String())
	}
	if _, err := session.Stream(io.NopCloser(strings.NewReader("replay")), io.Discard, nativeNoResize{}); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("session executed twice")
	}
	stored, err := s.Get(t.Context(), actor, access, "project-1", created.Session.ID)
	if err != nil || stored.Status != "closed" || stored.Result == nil || stored.Result.Status != "failed" || starts.Load() != 1 {
		t.Fatal(stored, err, starts.Load())
	}
}

func TestNativeDockerTerminalUnknownCloseAndOriginalKey(t *testing.T) {
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { nativeDockerCaps(w) }))
	defer agent.Close()
	s, _, _, _, actor, access := nativeDockerFixture(t, agent.URL)
	access.SessionID = ""
	actor.AccessTokenID = "original-key"
	created, err := s.Create(t.Context(), actor, access, "project-1", protocol.TerminalInput{ExpectedContainerID: strings.Repeat("a", 64), ServiceName: "web"})
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.Acquire(t.Context(), actor, access, "project-1", created.Session.ID, created.Ticket)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	item, err := s.Get(t.Context(), actor, access, "project-1", created.Session.ID)
	if err != nil || item.Result == nil || item.Result.Status != "unknown" || item.Result.ExitCode != nil {
		t.Fatal(item, err)
	}
	actor.AccessTokenID = "different-key"
	if _, err := s.Get(t.Context(), actor, access, "project-1", created.Session.ID); err == nil {
		t.Fatal("different Key read original receipt")
	}
}

type failedNativeDockerOutput struct{ peer *terminalstream.Endpoint }

func (w failedNativeDockerOutput) Write([]byte) (int, error) {
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, known := w.peer.Result(); known {
			return 0, io.ErrClosedPipe
		}
		time.Sleep(time.Millisecond)
	}
	return 0, io.ErrClosedPipe
}

func TestNativeDockerTerminalOutputFailureCannotPersistKnownSuccess(t *testing.T) {
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		stream := terminalstream.NewServer(r.Context(), conn, resource.NativeTerminalSize{Width: 80, Height: 24})
		defer func() { _ = stream.Close() }()
		_ = stream.SendReady()
		_, _ = stream.Write([]byte("output"))
		zero := int64(0)
		_ = stream.SendExit(protocol.TerminalResult{Status: "succeeded", ExitCode: &zero, UIDVerifiedBeforeStart: true, OutputBytes: 6})
	}))
	defer agent.Close()
	ctx, stop := context.WithTimeout(t.Context(), 2*time.Second)
	defer stop()
	conn, handshake, err := websocket.DefaultDialer.DialContext(ctx, "ws"+strings.TrimPrefix(agent.URL, "http"), nil)
	if handshake != nil && handshake.Body != nil {
		defer func() { _ = handshake.Body.Close() }()
	}
	if err != nil {
		t.Fatal(err)
	}
	peer := terminalstream.NewClient(ctx, conn)
	defer func() { _ = peer.Close() }()
	if err := peer.WaitReady(ctx); err != nil {
		t.Fatal(err)
	}
	result, err := copyDockerTerminalPeer(ctx, stop, peer, io.NopCloser(strings.NewReader("")), failedNativeDockerOutput{peer: peer}, nativeNoResize{})
	if err == nil || result.Status != "unknown" || result.ExitCode != nil {
		t.Fatal("output failure asserted success", result, err)
	}
}

func TestNativeDockerTerminalRevocationCancelsAnActiveStream(t *testing.T) {
	started := make(chan struct{})
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/docker/runtime/v1/terminal/capabilities" {
			nativeDockerCaps(w)
			return
		}
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		peer := terminalstream.NewServer(r.Context(), conn, resource.NativeTerminalSize{Width: 80, Height: 24})
		defer func() { _ = peer.Close() }()
		if peer.SendReady() != nil {
			return
		}
		close(started)
		<-peer.Done()
	}))
	defer agent.Close()
	s, repo, identity, _, actor, access := nativeDockerFixture(t, agent.URL)
	created, err := s.Create(t.Context(), actor, access, "project-1", protocol.TerminalInput{ExpectedContainerID: strings.Repeat("a", 64), ServiceName: "web"})
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.Acquire(t.Context(), actor, access, "project-1", created.Session.ID, created.Ticket)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.Close() }()
	done := make(chan error, 1)
	go func() {
		_, err := session.Stream(io.NopCloser(strings.NewReader("")), io.Discard, nativeNoResize{})
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("provider not connected")
	}
	identity.revoked.Store(true)
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("revocation accepted as success")
		}
	case <-time.After(4 * time.Second):
		t.Fatal("revoked stream still active")
	}
	stored, err := repo.Get(t.Context(), actor.UserID, created.Session.ID)
	if err != nil || stored.Lease.Status != "closed" || stored.Lease.Result == nil || stored.Lease.Result.Status != "unknown" || stored.Lease.Result.ExitCode != nil {
		t.Fatal("revocation invented process exit", stored.Lease, err)
	}
}
