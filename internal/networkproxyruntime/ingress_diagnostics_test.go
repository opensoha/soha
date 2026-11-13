package networkproxyruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	api "github.com/opensoha/soha/contracts/gen/go/sohaapi"
)

type diagnosticLease struct {
	err               error
	closed, refreshed chan struct{}
	closes            atomic.Int32
}

func (l *diagnosticLease) Refresh(time.Time) error { close(l.refreshed); return l.err }
func (l *diagnosticLease) Close() error {
	if l.closes.Add(1) == 1 {
		close(l.closed)
	}
	return nil
}

type diagnosticTransport func(*http.Request) (*http.Response, error)

func (f diagnosticTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestIngressClosureDiagnosticsWhitelistAndBehavior(t *testing.T) {
	const secret = "NEVER_LOG_secret_target_credential_nonce"
	for _, kind := range []string{"http_status", "response_invalid", "denied", "identity_mismatch", "decision_expired", "local_lease_expired", "refresh_failed", "timeout", "canceled", "transport", "hard_expiry", "success"} {
		t.Run(kind, func(t *testing.T) {
			var output bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
			defer slog.SetDefault(previous)
			input := api.NetworkProxyIngressCheckInput{BundleID: secret, NodeID: secret, CredentialID: secret, CredentialSecret: secret, ProcessEpoch: secret, DestinationHost: secret, Network: "udp", Revision: 1, CredentialEpoch: 1}
			client := diagnosticClientFixture(t, kind, secret, input)
			until := time.Now().Add(180 * time.Millisecond)
			p := &publicationIngress{client: client, until: time.Now().Add(time.Second)}
			if kind == "local_lease_expired" {
				p.until = time.Now()
			}
			if kind == "hard_expiry" {
				until = time.Now().Add(-time.Millisecond)
			}
			lease := &diagnosticLease{closed: make(chan struct{}), refreshed: make(chan struct{})}
			if kind == "refresh_failed" {
				lease.err = errors.New(secret)
			}
			done := make(chan struct{})
			finished := make(chan struct{})
			go func() { defer close(finished); p.recheck(done, lease, input, until) }()
			if kind == "success" {
				select {
				case <-lease.refreshed:
					close(done)
				case <-time.After(time.Second):
					t.Fatal("normal renewal failed")
				}
			} else {
				select {
				case <-lease.closed:
				case <-time.After(time.Second):
					close(done)
					t.Fatal("failure did not close lease")
				}
			}
			<-finished
			assertIngressClosureDiagnostic(t, kind, secret, lease, &output)
		})
	}
}

func TestIngressDiagnosticMasksUnknownEnumsAndPreservesErrorText(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	defer slog.SetDefault(previous)
	logIngressClosure("unknown-secret-stage", "unknown-secret-network", time.Millisecond, &ingressCheckError{message: "unknown-secret-message", classification: "unknown-secret-class", decoded: true, reason: "unknown-secret-reason"})
	if strings.Contains(output.String(), "unknown-secret") {
		t.Fatal("unknown enum leaked")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(409) }))
	defer server.Close()
	r := deadlineRunner(time.Now())
	r.client = NewClient(server.URL, "runtime", nil)
	r.config.IngressListen = "existing"
	err := r.updatePublication(context.Background())
	var status controlStatusError
	if err == nil || !strings.HasPrefix(err.Error(), "publication readiness:") || !errors.As(err, &status) || status != 409 {
		t.Fatalf("lost stage/status: %v", err)
	}
	decision := ingressDecisionError("denied", "proxy ingress authorization denied", api.NetworkProxyIngressCheckResult{})
	if decision.Error() != "proxy ingress authorization denied" {
		t.Fatal("validation error text changed")
	}
}

type blockingDiagnosticLog struct {
	entered, release chan struct{}
	calls            atomic.Int32
}

func (*blockingDiagnosticLog) Enabled(context.Context, slog.Level) bool { return true }
func (h *blockingDiagnosticLog) Handle(context.Context, slog.Record) error {
	if h.calls.Add(1) == 1 {
		close(h.entered)
	}
	<-h.release
	return nil
}
func (h *blockingDiagnosticLog) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *blockingDiagnosticLog) WithGroup(string) slog.Handler      { return h }

func TestIngressDiagnosticBlockedLoggerDoesNotDelayClose(t *testing.T) {
	h := &blockingDiagnosticLog{entered: make(chan struct{}), release: make(chan struct{})}
	previous := slog.Default()
	slog.SetDefault(slog.New(h))
	defer slog.SetDefault(previous)
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(h.release) }) }
	defer release()
	lease := &diagnosticLease{closed: make(chan struct{}), refreshed: make(chan struct{})}
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		(&publicationIngress{}).recheck(make(chan struct{}), lease, api.NetworkProxyIngressCheckInput{Network: "tcp"}, time.Now().Add(-time.Millisecond))
	}()
	select {
	case <-h.entered:
	case <-time.After(time.Second):
		t.Fatal("diagnostic did not reach blocked logger")
	}
	select {
	case <-lease.closed:
	default:
		t.Fatal("blocked logging delayed fail-closed action")
	}
	select {
	case <-finished:
		t.Fatal("logger was not actually blocked")
	default:
	}
	release()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("released logger did not finish")
	}
	if lease.closes.Load() != 1 || h.calls.Load() != 1 {
		t.Fatal("logging caused duplicate closure or log")
	}
}

func diagnosticClientFixture(t *testing.T, kind, secret string, input api.NetworkProxyIngressCheckInput) *Client {
	t.Helper()
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if kind == "http_status" {
			w.WriteHeader(429)
			_, _ = w.Write([]byte(secret))
			return
		}
		if kind == "response_invalid" {
			_, _ = w.Write([]byte(secret))
			return
		}
		if kind == "timeout" {
			select {
			case <-r.Context().Done():
			case <-release:
			}
			return
		}
		result := api.NetworkProxyIngressCheckResult{Allowed: true, BundleID: input.BundleID, NodeID: input.NodeID, Revision: 1, CredentialEpoch: 1, ExpiresAt: time.Now().Add(time.Second), ReasonCode: secret}
		switch kind {
		case "denied":
			result.Allowed = false
		case "identity_mismatch":
			result.Revision++
		case "decision_expired":
			result.ExpiresAt = time.Now()
		}
		_ = json.NewEncoder(w).Encode(result)
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(release) })
	client := NewClient(server.URL, secret, nil)
	if kind == "transport" || kind == "canceled" {
		client.http.Transport = diagnosticTransport(func(*http.Request) (*http.Response, error) {
			if kind == "canceled" {
				return nil, fmt.Errorf("%s: %w", secret, context.Canceled)
			}
			return nil, errors.New(secret)
		})
	}
	return client
}

func assertIngressClosureDiagnostic(t *testing.T, kind, secret string, lease *diagnosticLease, output *bytes.Buffer) {
	t.Helper()
	if strings.Contains(output.String(), secret) {
		t.Fatal("diagnostic leaked sensitive input/error/body")
	}
	if kind == "success" {
		if output.Len() != 0 || lease.closes.Load() != 0 {
			t.Fatal("success logged or closed")
		}
		return
	}
	if lease.closes.Load() != 1 || bytes.Count(output.Bytes(), []byte("\n")) != 1 {
		t.Fatalf("expected one closure/log: %s", output.String())
	}
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	want := kind
	if kind == "hard_expiry" {
		want = "local_lease_expired"
	}
	if record["classification"] != want {
		t.Fatalf("classification=%v want=%s", record["classification"], want)
	}
	decoded := kind == "denied" || kind == "identity_mismatch" || kind == "decision_expired" || kind == "local_lease_expired"
	_, hasAllowed := record["allowed"]
	if hasAllowed != decoded {
		t.Fatalf("decoded Allowed presence=%v want=%v", hasAllowed, decoded)
	}
	if kind == "http_status" && record["status"] != float64(429) {
		t.Fatalf("status=%v", record["status"])
	}
	for key := range record {
		switch key {
		case "time", "level", "msg", "stage", "network", "classification", "elapsedMs", "status", "allowed", "reasonCode", "httpConnReused", "httpGetConnMs", "httpGotConnMs", "httpDNSStartMs", "httpDNSDoneMs", "httpTLSStartMs", "httpTLSDoneMs", "httpWroteRequestMs", "httpFirstResponseByteMs":
		default:
			t.Fatalf("non-whitelisted key %s", key)
		}
	}
}
