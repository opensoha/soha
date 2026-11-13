package runner

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	cfgpkg "github.com/opensoha/soha-agent/internal/agent/config"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	"golang.org/x/crypto/ssh"
)

func TestSSHCommandCancellationSignalsOnlyItsSessionBeforeTransportClose(t *testing.T) {
	for _, mode := range []string{"acknowledged", "reply-timeout", "completed"} {
		t.Run(mode, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = listener.Close() }()
			key := testSSHSigner(t)
			signals, done := make(chan string, 1), make(chan struct{})
			go func() {
				defer close(done)
				connection, err := listener.Accept()
				if err != nil {
					return
				}
				config := &ssh.ServerConfig{NoClientAuth: true}
				config.AddHostKey(key)
				server, channels, requests, err := ssh.NewServerConn(connection, config)
				if err != nil {
					_ = connection.Close()
					return
				}
				defer func() { _ = server.Close() }()
				go ssh.DiscardRequests(requests)
				channel, ok := <-channels
				if !ok {
					return
				}
				_, sessionRequests, err := channel.Accept()
				if err != nil {
					return
				}
				for request := range sessionRequests {
					if request.Type != "signal" {
						continue
					}
					var input struct{ Signal string }
					if ssh.Unmarshal(request.Payload, &input) == nil {
						signals <- input.Signal
					}
					if mode != "reply-timeout" {
						_ = request.Reply(true, nil)
					}
				}
			}()
			host, port, _ := net.SplitHostPort(listener.Addr().String())
			number, _ := strconv.Atoi(port)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			client, err := dialSSH(ctx, sohaapi.AgentSSHSessionRequest{Address: host, Port: number, Username: "soha", TrustedHostKey: string(ssh.MarshalAuthorizedKey(key.PublicKey()))}, ssh.Password("fixture"))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = client.Close() }()
			session, err := client.NewSession()
			if err != nil {
				t.Fatal(err)
			}
			stop := stopSSHCommandOnCancel(ctx, client, session)
			if mode != "completed" {
				cancel()
			}
			stop()
			stop()
			if mode == "completed" {
				select {
				case <-signals:
					t.Fatal("completed command was signaled")
				default:
				}
				_ = client.Close()
			} else {
				select {
				case signal := <-signals:
					if signal != "KILL" {
						t.Fatal(signal)
					}
				case <-time.After(3 * time.Second):
					t.Fatal("session signal missing")
				}
			}
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("transport did not close")
			}
		})
	}
}

func TestSSHCommandOutputIsBoundedEvenForInvalidUTF8(t *testing.T) {
	var output sshCommandOutput
	data := []byte(strings.Repeat("\xff", 2<<20))
	if n, err := output.Write(data); err != nil || n != len(data) {
		t.Fatal(n, err)
	}
	value, truncated := output.snapshot()
	if len(value) > 1<<20 || !utf8.ValidString(value) || !truncated {
		t.Fatal("unbounded or invalid command output")
	}
}

func TestSSHCommandRejectsUnauthorizedIntentBeforeCredentialRedemption(t *testing.T) {
	var callback callbackRequest
	requests := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		requests = append(requests, request.URL.Path)
		if strings.Contains(request.URL.Path, "callback") {
			if err := json.NewDecoder(request.Body).Decode(&callback); err != nil {
				t.Error(err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"id": "task-one"}})
			return
		}
		t.Errorf("unexpected credential or SSH request %s", request.URL.Path)
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()
	logs, observed := observer.New(zap.WarnLevel)
	r := New(cfgpkg.ControlPlaneConfig{AgentID: "agent-one", BaseURL: server.URL, BearerToken: "agent-only"}, zap.New(logs))
	r.SetSSHExecutor("cluster-one", true)
	r.executeSSHCommand(context.Background(), ExecutionTask{ID: "task-one", ProviderKind: "vm_ssh.other-cluster", TimeoutSeconds: 60, CallbackToken: "callback-one", Payload: map[string]any{"sshCommand": sohaapi.AgentSSHCommandRequest{Command: "must never execute", TimeoutSeconds: 60}}})
	if len(requests) != 1 || callback.Status != "failed" {
		t.Fatal("invalid intent was dispatched")
	}
	raw, _ := json.Marshal(callback.Payload["sshCommand"])
	var result sohaapi.AgentSSHCommandResult
	_ = json.Unmarshal(raw, &result)
	if result.Effect != "not_started" || !result.Stopped || result.ExitCode != nil || strings.Contains(string(raw), "must never execute") {
		t.Fatal("invalid rejection receipt")
	}
	entries := observed.All()
	if len(entries) != 1 || entries[0].ContextMap()["stage"] != "validation" || len(entries[0].ContextMap()) != 2 {
		t.Fatal("unsafe or missing preparation diagnostic", entries)
	}
}

func TestSSHAuthorizationNeverFollowsRedirectWithAgentCredentials(t *testing.T) {
	targetRequests := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { targetRequests++; w.WriteHeader(200) }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	r := New(cfgpkg.ControlPlaneConfig{AgentID: "agent-one", BaseURL: server.URL, BearerToken: "agent-only"}, nil)
	if err := r.checkSSHCommandAuthorization(context.Background(), sohaapi.AgentSSHSessionRequest{SessionID: "session-one"}); err == nil || targetRequests != 0 {
		t.Fatal("redirect received credentials")
	}
}

func TestSSHClaimRemainsAvailableAlongsideBuildpacks(t *testing.T) {
	var claim claimRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&claim)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{}})
	}))
	defer server.Close()
	r := New(cfgpkg.ControlPlaneConfig{AgentID: "agent-one", BaseURL: server.URL, BearerToken: "agent-only", ProviderKinds: []string{"unrelated"}, Buildpacks: cfgpkg.BuildpacksConfig{Enabled: true}}, nil)
	r.SetSSHExecutor("cluster-one", true)
	r.claim(context.Background())
	if strings.Join(claim.ProviderKinds, ",") != "buildpacks_runner.agent-one,vm_ssh.cluster-one" {
		t.Fatal(claim.ProviderKinds)
	}
}
