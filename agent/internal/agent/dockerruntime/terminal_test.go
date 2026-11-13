package dockerruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	protocol "github.com/opensoha/soha/contracts/docker/runtime"
	"github.com/opensoha/soha/contracts/resource"
	"github.com/opensoha/soha/contracts/resource/terminalstream"
)

type ownedTerminalSizes struct{ sent atomic.Bool }

func (q *ownedTerminalSizes) Next(ctx context.Context) *resource.NativeTerminalSize {
	if q.sent.CompareAndSwap(false, true) {
		return &resource.NativeTerminalSize{Width: 100, Height: 30}
	}
	<-ctx.Done()
	return nil
}

func TestNativeDockerTerminalKeepsTransportLimitCause(t *testing.T) {
	for cause, want := range map[error]string{terminalstream.ErrInputLimit: "input_limit", terminalstream.ErrOutputLimit: "output_limit"} {
		result := uncertainTerminalResult(protocol.TerminalResult{Status: "unknown", ReasonCode: "transport_lost"}, cause)
		if result.Status != want || result.ReasonCode != want || result.ExitCode != nil || result.Validate() != nil {
			t.Fatalf("lost bounded transport cause=%+v", result)
		}
	}
}

func TestNativeDockerTerminalUsesOneOriginalEngineExecAndActualExit(t *testing.T) {
	for _, fault := range []string{"none", "exit-seven", "create-ack-lost", "attach-ack-lost", "missing-exit", "foreign-before", "foreign-after"} {
		t.Run(fault, func(t *testing.T) {
			project, err := protocol.NewProjectBinding("6f773cfe-a522-468c-8c85-4b326d41b781", "65b279e6-ea7c-412f-a822-228349f35585")
			if err != nil {
				t.Fatal(err)
			}
			binding := protocol.ContainerBinding{ProjectBinding: project, ServiceName: "owned", ContainerID: strings.Repeat("b", 64)}
			id := strings.Repeat("a", 64)
			var creates, attaches, containerReads, resizes atomic.Int32
			var finished atomic.Bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/_ping":
					w.Header().Set("API-Version", "1.55")
					w.WriteHeader(200)
				case "/v1.55/info":
					_, _ = io.WriteString(w, `{"OSType":"linux"}`)
				case "/v1.55/containers/" + binding.ContainerID + "/json":
					count := containerReads.Add(1)
					labels := binding.Labels()
					labels["com.docker.compose.project"], labels["com.docker.compose.service"] = binding.Namespace, binding.ServiceName
					if fault == "foreign-before" || fault == "foreign-after" && count > 1 {
						labels[protocol.ProjectUIDLabel] = "foreign"
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"Id": binding.ContainerID, "Platform": "linux", "State": map[string]bool{"Running": true}, "Config": map[string]any{"Labels": labels}})
				case "/v1.55/containers/" + binding.ContainerID + "/exec":
					creates.Add(1)
					var input struct {
						Tty, Privileged, AttachStdin, AttachStdout, AttachStderr bool
						Cmd                                                      []string
						ConsoleSize                                              [2]uint
					}
					if r.Method != "POST" || json.NewDecoder(r.Body).Decode(&input) != nil || !input.Tty || input.Privileged || !input.AttachStdin || !input.AttachStdout || !input.AttachStderr || len(input.Cmd) != 1 || input.Cmd[0] != "/bin/sh" || input.ConsoleSize != [2]uint{24, 80} {
						t.Error("exec was not the exact native unprivileged PTY")
					}
					if fault == "create-ack-lost" {
						hijacker, ok := w.(http.Hijacker)
						if !ok {
							t.Error("HTTP fixture does not support hijacking")
							return
						}
						conn, _, err := hijacker.Hijack()
						if err != nil {
							t.Error(err)
							return
						}
						_ = conn.Close()
						return
					}
					_ = json.NewEncoder(w).Encode(map[string]string{"Id": id})
				case "/v1.55/exec/" + id + "/start":
					attaches.Add(1)
					var input struct{ Tty, Detach bool }
					if r.Method != "POST" || json.NewDecoder(r.Body).Decode(&input) != nil || !input.Tty || input.Detach {
						t.Error("attach did not use the original TTY exec")
					}
					hijacker, ok := w.(http.Hijacker)
					if !ok {
						t.Error("HTTP fixture does not support hijacking")
						return
					}
					conn, wire, err := hijacker.Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					defer func() { _ = conn.Close() }()
					if fault == "attach-ack-lost" {
						return
					}
					_, _ = wire.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: tcp\r\nContent-Type: application/vnd.docker.raw-stream\r\n\r\n")
					_ = wire.Flush()
					_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
					line, err := wire.ReadString('\r')
					if err != nil || line != "exit\r" {
						t.Error("original stdin not received")
						return
					}
					_, _ = wire.WriteString("native-owned\r\n")
					_ = wire.Flush()
					finished.Store(true)
				case "/v1.55/exec/" + id + "/resize":
					resizes.Add(1)
					if r.URL.Query().Get("w") != "100" || r.URL.Query().Get("h") != "30" {
						t.Error("resize target/dimensions changed")
					}
					w.WriteHeader(204)
				case "/v1.55/exec/" + id + "/json":
					item := terminalExecFixture()
					item["Running"] = !finished.Load()
					if fault == "exit-seven" {
						item["ExitCode"] = 7
					}
					if fault == "missing-exit" {
						delete(item, "ExitCode")
					}
					_ = json.NewEncoder(w).Encode(item)
				default:
					t.Errorf("unexpected Engine route=%s", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			for name, value := range map[string]string{"DOCKER_HOST": "tcp://" + server.Listener.Addr().String(), "DOCKER_CONTEXT": "", "DOCKER_API_VERSION": "", "DOCKER_TLS": "", "DOCKER_TLS_VERIFY": "", "DOCKER_CERT_PATH": ""} {
				t.Setenv(name, value)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			input := protocol.TerminalRequest{Protocol: protocol.TerminalProtocol, ProjectID: "project:owned", Container: binding, Input: protocol.TerminalInput{ExpectedContainerID: binding.ContainerID, ServiceName: binding.ServiceName}, DeadlineAt: time.Now().Add(2 * time.Second)}
			var output bytes.Buffer
			result, err := StreamNativeTerminal(ctx, input, io.NopCloser(strings.NewReader("exit\r")), &output, &ownedTerminalSizes{})
			if err != nil || result.Validate() != nil {
				t.Fatalf("terminal result=%+v err=%v", result, err)
			}
			if creates.Load() > 1 || attaches.Load() > 1 {
				t.Fatal("original exec was replayed")
			}
			switch fault {
			case "none", "exit-seven":
				want, code := "succeeded", int64(0)
				if fault == "exit-seven" {
					want, code = "failed", 7
				}
				if result.Status != want || result.ExitCode == nil || *result.ExitCode != code || !result.UIDVerifiedBeforeStart || output.String() != "native-owned\r\n" || creates.Load() != 1 || attaches.Load() != 1 || containerReads.Load() != 2 {
					t.Fatalf("actual original outcome=%+v creates=%d attach=%d reads=%d", result, creates.Load(), attaches.Load(), containerReads.Load())
				}
			case "foreign-before":
				if result.Status != "unknown" || creates.Load() != 0 || attaches.Load() != 0 {
					t.Fatal("foreign container started")
				}
			default:
				if result.Status != "unknown" || result.ExitCode != nil {
					t.Fatalf("unconfirmed result became success=%+v", result)
				}
			}
		})
	}
}
