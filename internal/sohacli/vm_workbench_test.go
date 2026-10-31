package sohacli

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVMCreationUsesServerPlanAndOriginalKey(t *testing.T) {
	for _, scenario := range []string{"ready", "blocked", "retry-original", "lost-response"} {
		t.Run(scenario, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/api/v1/virtualization/vms/plan" {
					if scenario == "retry-original" {
						t.Error("original retry must not repeat the client plan")
					}
					_, _ = fmt.Fprintf(w, `{"data":{"ready":%t}}`, scenario != "blocked")
					return
				}
				if r.URL.Path != "/api/v1/virtualization/vms" || r.Method != "POST" || r.Header.Get("Idempotency-Key") != "original-key" {
					t.Errorf("unexpected submission %s %s", r.Method, r.URL.String())
				}
				if scenario == "lost-response" {
					w.WriteHeader(503)
					_, _ = fmt.Fprint(w, `{"error":{"code":"service_unavailable","message":"unavailable"}}`)
					return
				}
				_, _ = fmt.Fprint(w, `{"data":{"id":"operation-1","status":"queued"}}`)
			}))
			defer server.Close()
			args := []string{"compute", "vms", "create", "--input", "-", "--idempotency-key", "original-key", "--yes", "--profile", "dev"}
			if scenario == "retry-original" {
				args = append(args, "--retry-original")
			}
			var out, errOut bytes.Buffer
			code := Run(t.Context(), args, Runtime{In: strings.NewReader(`{"name":"fixture-vm"}`), Out: &out, Err: &errOut, ConfigPath: writeTestConfig(t, server.URL)})
			want := 2
			if scenario == "blocked" || scenario == "retry-original" {
				want = 1
			}
			if calls != want || (code == 0) != (scenario == "ready" || scenario == "retry-original") {
				t.Fatalf("calls=%d code=%d stderr=%s", calls, code, errOut.String())
			}
		})
	}
}

func TestVMWriteRejectsMissingKeyAndIgnoredScopeFlags(t *testing.T) {
	for _, extra := range [][]string{{}, {"--idempotency-key", "key", "--namespace", "other-team"}} {
		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
		args := []string{"compute", "vms", "power", "vm-a", "--input", "-", "--yes", "--profile", "dev"}
		args = append(args, extra...)
		var out, errOut bytes.Buffer
		code := Run(t.Context(), args, Runtime{In: strings.NewReader(`{"action":"start"}`), Out: &out, Err: &errOut, ConfigPath: writeTestConfig(t, server.URL)})
		server.Close()
		if code == 0 || calls != 0 {
			t.Fatalf("unsafe flags reached server: code=%d calls=%d", code, calls)
		}
	}
}

func TestVMConsoleHidesAccessValuesUnlessExplicitlyRequested(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"data":{"type":"vnc","ready":true,"url":"wss://fixture.invalid/?vncticket=ACCESS_VALUE","backendUrl":"wss://fixture.invalid/?ticket=ACCESS_VALUE","token":"ACCESS_VALUE","message":"ticket=ACCESS_VALUE"}}`)
		}))
		var out, errOut bytes.Buffer
		args := []string{"compute", "vms", "console", "vm-a", "--profile", "dev"}
		if explicit {
			args = append(args, "--show-access-details")
		}
		code := Run(t.Context(), args, Runtime{Out: &out, Err: &errOut, ConfigPath: writeTestConfig(t, server.URL)})
		server.Close()
		if code != 0 || strings.Contains(out.String(), "ACCESS_VALUE") != explicit {
			t.Fatalf("explicit=%t code=%d out=%s", explicit, code, out.String())
		}
	}
}
