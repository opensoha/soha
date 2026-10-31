package sohacli

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	contractresource "github.com/opensoha/soha-contracts/resource"
)

func fixturePodExecutionReceipt(status string) contractresource.PodExecutionReceipt {
	now := time.Now().UTC()
	code := int64(0)
	item := contractresource.PodExecutionReceipt{ID: "11111111-1111-4111-8111-111111111111", Status: status, ClusterID: "cluster-a", Namespace: "team-a", PodName: "app", ExpectedUID: "reviewed", Container: "main", CreatedAt: now, DeadlineAt: now.Add(time.Minute), ExpiresAt: now.Add(time.Hour)}
	if status == "running" {
		return item
	}
	item.FinishedAt = &now
	if status == "unknown" {
		return item
	}
	if status == "failed" {
		code = 42
	}
	item.OutputAvailable = true
	item.Output = &contractresource.PodExecutionOutput{Status: status, ExitCode: &code, Stdout: "remote output", StdoutBytes: 13, StdoutEncoding: "utf-8", StderrEncoding: "utf-8", MaxOutputBytes: 65536, UIDVerifiedBeforeStart: true}
	return item
}
func podExecCLIArgs() []string {
	return []string{"k8s", "pods", "exec", "app", "--cluster-id", "cluster-a", "--namespace", "team-a", "--expected-uid", "reviewed", "--idempotency-key", "original", "--yes", "--profile", "dev"}
}
func TestPodExecPreservesArgvAfterSeparatorAndBinaryStdin(t *testing.T) {
	want := []string{"printf", "$(literal)", "--help", "--timeout", "60s", ""}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "POST" || r.URL.Path != "/api/v1/clusters/cluster-a/workloads/pods/app/executions" || r.URL.Query().Get("namespace") != "team-a" || r.Header.Get("Idempotency-Key") != "original" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.String())
		}
		var input contractresource.PodArgvInput
		if json.NewDecoder(r.Body).Decode(&input) != nil || !reflect.DeepEqual(input.Argv, want) || input.StdinBase64 != base64.StdEncoding.EncodeToString([]byte{0, 255, 27}) {
			t.Errorf("argv/stdin changed: %+v", input)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": fixturePodExecutionReceipt("succeeded")})
	}))
	defer server.Close()
	args := append(podExecCLIArgs(), "--stdin-file", "-", "--")
	args = append(args, want...)
	var out, errOut bytes.Buffer
	code := Run(t.Context(), args, Runtime{In: bytes.NewReader([]byte{0, 255, 27}), Out: &out, Err: &errOut, ConfigPath: writeTestConfig(t, server.URL)})
	if code != 0 || calls != 1 || !strings.Contains(out.String(), "remote output") {
		t.Fatalf("code=%d calls=%d out=%s err=%s", code, calls, out.String(), errOut.String())
	}
}
func TestPodExecReturnsOriginalProcessStatusAndRejectsMismatchedReceipts(t *testing.T) {
	for _, scenario := range []string{"succeeded", "failed", "unknown", "running", "wrong-uid", "invalid-output"} {
		t.Run(scenario, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				item := fixturePodExecutionReceipt(scenario)
				if scenario == "wrong-uid" {
					item = fixturePodExecutionReceipt("succeeded")
					item.ExpectedUID = "replacement"
					item.Output.Stdout = "private-output-marker"
				}
				if scenario == "invalid-output" {
					item = fixturePodExecutionReceipt("succeeded")
					item.Output.StdoutBytes = 0
					item.Output.Stdout = "private-output-marker"
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"data": item})
			}))
			defer server.Close()
			var out, errOut bytes.Buffer
			args := append(podExecCLIArgs(), "--", "true")
			code := Run(t.Context(), args, Runtime{Out: &out, Err: &errOut, ConfigPath: writeTestConfig(t, server.URL)})
			want := 1
			if scenario == "succeeded" {
				want = 0
			}
			if scenario == "failed" {
				want = 42
			}
			if code != want || calls != 1 || strings.Contains(out.String()+errOut.String(), "private-output-marker") {
				t.Fatalf("code=%d want=%d calls=%d err=%s", code, want, calls, errOut.String())
			}
		})
	}
}
func TestPodExecutionRecoveryOnlyReadsOriginalKey(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(fmt.Sprint(missing), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "GET" || r.Header.Get("Idempotency-Key") != "original" || r.URL.Query().Get("namespace") != "team-a" {
					t.Errorf("recovery executed a command: %s", r.Method)
				}
				if missing {
					w.WriteHeader(404)
					_, _ = fmt.Fprint(w, `{"error":{"code":"not_found","message":"not received"}}`)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"data": fixturePodExecutionReceipt("succeeded")})
			}))
			defer server.Close()
			var out, errOut bytes.Buffer
			code := Run(t.Context(), []string{"k8s", "pods", "execution", "app", "--cluster-id", "cluster-a", "--namespace", "team-a", "--idempotency-key", "original", "--profile", "dev"}, Runtime{Out: &out, Err: &errOut, ConfigPath: writeTestConfig(t, server.URL)})
			if (code == 0) == missing || calls != 1 {
				t.Fatalf("code=%d calls=%d err=%s", code, calls, errOut.String())
			}
		})
	}
}
func TestPodExecResponseLossDoesNotSubmitAnotherAttempt(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			t.Error("fixture writer does not support hijacking")
			return
		}
		connection, _, err := hijacker.Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		_ = connection.Close()
	}))
	defer server.Close()
	var out, errOut bytes.Buffer
	code := Run(t.Context(), append(podExecCLIArgs(), "--", "true"), Runtime{Out: &out, Err: &errOut, ConfigPath: writeTestConfig(t, server.URL)})
	if code == 0 || calls.Load() != 1 || !strings.Contains(errOut.String(), "query the original") {
		t.Fatalf("code=%d calls=%d err=%s", code, calls.Load(), errOut.String())
	}
}
func TestPodExecTextNeutralizesRemoteTerminalControls(t *testing.T) {
	item := fixturePodExecutionReceipt("succeeded")
	item.Output.Stdout = "\x1b]52;c;clipboard\a\u202eevil"
	item.Output.StdoutBytes = int64(len(item.Output.Stdout))
	var out, errOut bytes.Buffer
	if err := writeKubernetesPodExecution(Runtime{Out: &out, Err: &errOut}, "text", item, nil); err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(out.String(), "\x1b\a\u202e") || !strings.Contains(out.String(), "clipboard") {
		t.Fatalf("unsafe terminal output %q", out.String())
	}
}
func TestPodExecRejectsIgnoredAndUnsafeInputWithoutHTTP(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	defer server.Close()
	for _, scenario := range []string{"stdin-limit", "missing-argv", "key-whitespace", "ignored-read-flag"} {
		t.Run(scenario, func(t *testing.T) {
			args := podExecCLIArgs()
			stdin := ""
			switch scenario {
			case "stdin-limit":
				args = append(args, "--stdin-file", "-", "--", "true")
				stdin = strings.Repeat("x", 65537)
			case "missing-argv":
				args = append(args, "--")
			case "key-whitespace":
				for i, v := range args {
					if v == "original" {
						args[i] = "not valid"
					}
				}
				args = append(args, "--", "true")
			case "ignored-read-flag":
				args = []string{"k8s", "pods", "execution", "app", "--cluster-id", "cluster-a", "--idempotency-key", "original", "--yes=false", "--profile", "dev"}
			}
			var out, errOut bytes.Buffer
			code := Run(t.Context(), args, Runtime{In: strings.NewReader(stdin), Out: &out, Err: &errOut, ConfigPath: writeTestConfig(t, server.URL)})
			if code != 2 || calls != 0 {
				t.Fatalf("code=%d calls=%d err=%s", code, calls, errOut.String())
			}
		})
	}
}
