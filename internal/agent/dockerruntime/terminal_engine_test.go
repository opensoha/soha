package dockerruntime

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moby/moby/client"
)

func terminalExecFixture() map[string]any {
	return map[string]any{"ID": strings.Repeat("a", 64), "ContainerID": strings.Repeat("b", 64), "Running": false, "ExitCode": 0, "Pid": 123}
}

func TestTerminalEngineSDKAndOriginalInspectionUseSameDaemon(t *testing.T) {
	for _, secure := range []bool{false, true} {
		t.Run(map[bool]string{false: "tcp", true: "mutual-tls"}[secure], func(t *testing.T) {
			var pings, inspections, proxyCalls atomic.Int32
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { proxyCalls.Add(1); w.WriteHeader(502) }))
			defer proxy.Close()
			t.Setenv("HTTP_PROXY", proxy.URL)
			t.Setenv("HTTPS_PROXY", proxy.URL)
			t.Setenv("NO_PROXY", "")
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/daemon/_ping" {
					pings.Add(1)
					w.Header().Set("API-Version", "1.55")
					w.WriteHeader(200)
					return
				}
				if r.URL.Path != "/daemon/v1.55/exec/"+strings.Repeat("a", 64)+"/json" || r.Method != "GET" {
					t.Errorf("unexpected original API target=%s %s", r.Method, r.URL.Path)
					w.WriteHeader(404)
					return
				}
				inspections.Add(1)
				if secure && (r.TLS == nil || len(r.TLS.VerifiedChains) == 0) {
					t.Error("mutual TLS not verified")
				}
				_ = json.NewEncoder(w).Encode(terminalExecFixture())
			})
			server := httptest.NewUnstartedServer(handler)
			env := terminalEngineEnvironment{}
			if secure {
				certificate, pool, dir := terminalTestCertificates(t)
				server.TLS = &tls.Config{Certificates: []tls.Certificate{certificate}, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: pool, MinVersion: tls.VersionTLS12}
				server.StartTLS()
				env.certPath, env.tlsVerify = dir, "1"
			} else {
				server.Start()
			}
			defer server.Close()
			env.host = "tcp://" + server.Listener.Addr().String() + "/daemon"
			engine, err := terminalEngineFromEnvironment(env)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = engine.Close() }()
			if _, err := engine.client.Ping(t.Context(), client.PingOptions{NegotiateAPIVersion: true}); err != nil {
				t.Fatal(err)
			}
			item, err := engine.inspectOriginalExec(t.Context(), strings.Repeat("a", 64), strings.Repeat("b", 64))
			if err != nil || item.ExitCode == nil || *item.ExitCode != 0 || item.Running {
				t.Fatalf("original outcome not verified: %v", err)
			}
			if pings.Load() != 1 || inspections.Load() != 1 || proxyCalls.Load() != 0 {
				t.Fatalf("pings=%d inspections=%d proxy=%d", pings.Load(), inspections.Load(), proxyCalls.Load())
			}
		})
	}
}

func terminalTestCertificates(t *testing.T) (tls.Certificate, *x509.CertPool, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Owned terminal test"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM, keyPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	dir := t.TempDir()
	for name, value := range map[string][]byte{"ca.pem": certPEM, "cert.pem": certPEM, "key.pem": keyPEM} {
		if err := os.WriteFile(filepath.Join(dir, name), value, 0600); err != nil {
			t.Fatal(err)
		}
	}
	certificate, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(certPEM) {
		t.Fatal("owned certificate pool failed")
	}
	return certificate, pool, dir
}

func TestTerminalEngineInspectionHonorsCancellationAndOriginalTarget(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if strings.Contains(r.URL.Path, "/exec/") {
			<-r.Context().Done()
			return
		}
		w.WriteHeader(200)
	}))
	defer server.Close()
	engine, err := terminalEngineFromEnvironment(terminalEngineEnvironment{host: "tcp://" + server.Listener.Addr().String(), apiVersion: "1.55"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = engine.Close() }()
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := engine.inspectOriginalExec(ctx, strings.Repeat("a", 64), strings.Repeat("b", 64)); err == nil {
		t.Fatal("unconfirmed inspect accepted")
	}
	if time.Since(start) > time.Second || calls.Load() != 1 {
		t.Fatal("inspection cancellation not bounded")
	}
}

func TestTerminalEngineRejectsAmbiguousDaemonConfiguration(t *testing.T) {
	for _, env := range []terminalEngineEnvironment{
		// #nosec G101 -- synthetic URL used only to prove credentials are rejected before any dial; it is not an operational credential.
		{context: "other-context"}, {host: "ssh://shared-daemon"}, {host: "http://daemon"}, {host: "tcp://user:password@daemon:2375"},
		{host: "npipe:////./pipe/docker_engine"},
		{host: "tcp://daemon:2375", tlsEnabled: "1"}, {host: "tcp://daemon:2376", certPath: "relative", tlsVerify: "1"},
		{host: "unix:///owned.sock", tlsVerify: "1"},
	} {
		if engine, err := terminalEngineFromEnvironment(env); err == nil {
			_ = engine.Close()
			t.Fatal("ambiguous daemon accepted")
		}
	}
}

func TestOriginalExecInspectionRequiresActualBoundExitCode(t *testing.T) {
	valid, _ := json.Marshal(terminalExecFixture())
	if _, err := decodeOriginalExec(valid); err != nil {
		t.Fatal(err)
	}
	failedStart := terminalExecFixture()
	failedStart["Pid"], failedStart["ExitCode"] = 0, 126
	failedRaw, _ := json.Marshal(failedStart)
	if item, err := decodeOriginalExec(failedRaw); err != nil || item.ExitCode == nil || *item.ExitCode != 126 {
		t.Fatal("known failed start with no PID was not retained")
	}
	for _, fault := range []string{"missing-exit", "null-exit", "missing-running", "null-running", "wrong-id", "negative-exit", "duplicate-running", "trailing"} {
		t.Run(fault, func(t *testing.T) {
			fixture := terminalExecFixture()
			switch fault {
			case "missing-exit":
				delete(fixture, "ExitCode")
			case "null-exit":
				fixture["ExitCode"] = nil
			case "missing-running":
				delete(fixture, "Running")
			case "null-running":
				fixture["Running"] = nil
			case "wrong-id":
				fixture["ID"] = "prefix"
			case "negative-exit":
				fixture["ExitCode"] = -1
			}
			raw, _ := json.Marshal(fixture)
			if fault == "duplicate-running" {
				raw = append(raw[:len(raw)-1], []byte(`,"Running":false}`)...)
			}
			if fault == "trailing" {
				raw = append(raw, []byte(`{}`)...)
			}
			if _, err := decodeOriginalExec(raw); err == nil {
				t.Fatal("unknown/malformed outcome accepted")
			}
		})
	}
}
