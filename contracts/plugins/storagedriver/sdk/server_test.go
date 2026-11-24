package sdk

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/opensoha/soha/contracts/plugins/storagedriver"
)

var testIdentity = storagedriver.Identity{
	PluginID: "example.storage", Version: "1.2.3",
	ProtocolMajor: storagedriver.ProtocolMajor, Capabilities: storagedriver.V1Capabilities,
}

type probeDriver struct {
	storagedriver.Driver
	calls atomic.Int64
}

func (d *probeDriver) TestConnection(_ context.Context, binding storagedriver.Binding) error {
	d.calls.Add(1)
	if secret := binding.Credentials["credential"]; secret != "" {
		return errors.New(secret)
	}
	return nil
}

func TestTLSHandshakeAndMessageBoundaries(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	driver := new(probeDriver)
	cfg := startTLSFixture(t, driver)
	wrongPin := cfg
	wrongPin.TLSLeafSHA256[0] ^= 0xff
	if _, err := Dial(ctx, wrongPin); !errors.Is(err, ErrTransport) {
		t.Fatalf("wrong TLS pin was accepted: %v", err)
	}
	rawTransport := clientTransport(cfg)
	rawTransport.ExpectContinueTimeout = time.Second
	defer rawTransport.CloseIdleConnections()
	raw := &http.Client{Transport: rawTransport}
	endpoint := "https://storage-driver"
	status, body := exchange(t, ctx, raw, http.MethodPost, endpoint+storagedriver.PathTestConnection, "", "malformed", nil)
	if status != http.StatusUnauthorized || !strings.Contains(body, string(storagedriver.CodeUnauthenticated)) {
		t.Fatalf("pre-handshake request got %d %q", status, body)
	}
	wrongID := cfg
	wrongID.Expected.Version = "1.2.4"
	if _, err := Dial(ctx, wrongID); err != rpcError(storagedriver.CodeHandshakeMismatch) {
		t.Fatalf("wrong driver version was accepted: %v", err)
	}
	client, err := Dial(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if _, err := Dial(ctx, cfg); err != rpcError(storagedriver.CodeUnauthenticated) {
		t.Fatalf("second handshake was accepted: %v", err)
	}
	testPath := endpoint + storagedriver.PathTestConnection
	openPath := endpoint + storagedriver.PathOpenObject
	large := strings.Repeat("x", storagedriver.MaxControlBytes+1)
	for _, tc := range []struct {
		name, method, url, token, payload string
		headers                           http.Header
		want                              int
	}{
		{name: "wrong bearer", method: http.MethodPost, url: testPath, token: "Bearer forged",
			payload: `{"binding":{"id":"a","container":"b"}}`, want: http.StatusUnauthorized},
		{name: "bad object", method: http.MethodPost, url: testPath, token: client.bearer,
			payload: `{"binding":{"id":"a","container":"b"}} {}`, want: http.StatusBadRequest},
		{name: "unknown field", method: http.MethodPost, url: testPath, token: client.bearer,
			payload: `{"binding":{"id":"a","container":"b"},"unexpected":true}`, want: http.StatusBadRequest},
		{name: "oversize control", method: http.MethodPost, url: testPath, token: client.bearer,
			payload: large, headers: http.Header{"Expect": {"100-continue"}}, want: http.StatusRequestEntityTooLarge},
		{name: "oversize metadata", method: http.MethodGet, url: openPath, token: client.bearer,
			headers: http.Header{storagedriver.HeaderMetadata: {strings.Repeat("a", 4*storagedriver.MaxMetadataBytes/3+2)}},
			want:    http.StatusRequestEntityTooLarge},
		{name: "duplicate metadata", method: http.MethodGet, url: openPath, token: client.bearer,
			headers: http.Header{storagedriver.HeaderMetadata: {"e30", "e30"}}, want: http.StatusBadRequest},
		{name: "oversize headers", method: http.MethodPost, url: testPath, token: client.bearer,
			headers: http.Header{"X-Padding": {strings.Repeat("x", storagedriver.MaxHeaderBytes)}}, want: http.StatusBadRequest},
		{name: "unexpected query", method: http.MethodPost, url: testPath + "?", token: client.bearer,
			payload: `{"binding":{"id":"a","container":"b"}}`, want: http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, _ := exchange(t, ctx, raw, tc.method, tc.url, tc.token, tc.payload, tc.headers)
			if status != tc.want {
				t.Fatalf("HTTP %d, want %d", status, tc.want)
			}
		})
	}
	if driver.calls.Load() != 0 {
		t.Fatal("invalid or unauthenticated request reached the driver")
	}
	binding := storagedriver.Binding{ID: "a", Container: "b"}
	if err := client.TestConnection(ctx, binding); err != nil {
		t.Fatal(err)
	}
	binding.Credentials = map[string]string{"credential": "synthetic-provider-secret"}
	if err := client.TestConnection(ctx, binding); err != rpcError(storagedriver.CodeInternal) {
		t.Fatalf("provider error was not normalized: %v", err)
	}
}

func TestBootstrapRejectsUnprotectedEndpoints(t *testing.T) {
	secret := make([]byte, storagedriver.LaunchSecretBytes)
	var out bytes.Buffer
	for _, cfg := range []Bootstrap{
		{Network: "unix", Address: "relative.sock", LaunchSecret: secret},
		{Network: "tcp", Address: "0.0.0.0:9443", LaunchSecret: secret},
		{Network: "tcp", Address: "localhost:9443", LaunchSecret: secret},
		{Network: "tcp", Address: "127.0.0.1:0", LaunchSecret: secret},
		{Network: "tcp", Address: "127.0.0.1:9443", LaunchSecret: secret},
	} {
		if err := WriteBootstrap(&out, cfg); !errors.Is(err, ErrBootstrap) {
			t.Fatalf("accepted invalid endpoint: %v", err)
		}
	}
	for _, input := range []string{"null", `{} {}`, `{"unexpected":true}`, strings.Repeat("x", maxBootstrapBytes+1)} {
		if _, err := readBootstrap(context.Background(), strings.NewReader(input)); !errors.Is(err, ErrBootstrap) {
			t.Fatalf("accepted invalid bootstrap: %v", err)
		}
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o755); err != nil { //nolint:gosec // G302: intentionally world-accessible parent for the negative bootstrap fixture.
		t.Fatal(err)
	}
	cfg := Bootstrap{Network: "unix", Address: filepath.Join(dir, "s"), LaunchSecret: secret}
	if err := WriteBootstrap(&out, cfg); err != nil {
		t.Fatal(err)
	}
	err := Serve(context.Background(), &out, ServerOptions{PluginID: testIdentity.PluginID, Version: testIdentity.Version}, new(probeDriver))
	if !errors.Is(err, ErrBootstrap) {
		t.Fatalf("accepted world-accessible socket parent: %v", err)
	}
}

func startTLSFixture(t *testing.T, driver storagedriver.Driver) ClientOptions {
	t.Helper()
	public, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	leaf := &x509.Certificate{
		SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, leaf, leaf, public, key)
	if err != nil {
		t.Fatal(err)
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := listen(Bootstrap{
		Network: "tcp", Address: "127.0.0.1:0",
		TLS: &TLSBootstrap{CertificateDER: der, PrivateKeyDER: privateDER},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	launchSecret := make([]byte, storagedriver.LaunchSecretBytes)
	if _, err := rand.Read(launchSecret); err != nil {
		t.Fatal(err)
	}
	exit := make(chan error, 1)
	go func() {
		exit <- serve(ctx, listener, testIdentity, bytes.Clone(launchSecret), 5*time.Second, driver)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-exit:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("TLS fixture did not stop")
		}
	})
	return ClientOptions{
		Network: "tcp", Address: listener.Addr().String(), Expected: testIdentity,
		LaunchSecret: launchSecret, TLSLeafSHA256: sha256.Sum256(der), CallTimeout: 5 * time.Second,
	}
}

func exchange(t *testing.T, ctx context.Context, client *http.Client, method, url, token, payload string,
	headers http.Header,
) (int, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, method, url, strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	for name, values := range headers {
		req.Header[name] = values
	}
	if payload != "" {
		req.Header.Set("Content-Type", storagedriver.MediaTypeJSON)
	}
	if token != "" {
		req.Header.Set("Authorization", token)
	}
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, 4096))
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, string(body)
}

func TestOneShotHandshakeUnderConcurrency(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cfg := startTLSFixture(t, new(probeDriver))
	results := make(chan error, 2)
	for range 2 {
		go func() {
			client, err := Dial(ctx, cfg)
			if client != nil {
				_ = client.Close()
			}
			results <- err
		}()
	}
	accepted := 0
	for range 2 {
		switch err := <-results; err {
		case nil:
			accepted++
		case rpcError(storagedriver.CodeUnauthenticated):
		default:
			t.Fatal(err)
		}
	}
	if accepted != 1 {
		t.Fatalf("accepted %d handshakes, want exactly one", accepted)
	}
}

type droppingOpenDriver struct {
	probeDriver
	opens atomic.Int32
}

func (d *droppingOpenDriver) OpenObject(_ context.Context, _ storagedriver.OpenObjectRequest) (storagedriver.OpenObjectResult, error) {
	d.opens.Add(1)
	panic(http.ErrAbortHandler)
}

func TestClientDoesNotReplayOpenWithoutResponse(t *testing.T) {
	driver := new(droppingOpenDriver)
	cfg := startTLSFixture(t, driver)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := Dial(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	binding := storagedriver.Binding{ID: "binding", Container: "container"}
	if err := client.TestConnection(ctx, binding); err != nil {
		t.Fatal(err)
	}
	if _, err := client.OpenObject(ctx, storagedriver.OpenObjectRequest{Binding: binding, Key: "object"}); !errors.Is(err, ErrTransport) {
		t.Fatalf("dropped Open returned %v", err)
	}
	if opens := driver.opens.Load(); opens != 1 {
		t.Fatalf("Open was dispatched %d times, want exactly one", opens)
	}
}

func TestServerRejectsHTTP10(t *testing.T) {
	driver := new(probeDriver)
	handler := &serverHandler{driver: driver, bearer: "Bearer test"}
	request := httptest.NewRequest(http.MethodPost, storagedriver.PathTestConnection, nil)
	request.Proto, request.ProtoMinor = "HTTP/1.0", 0
	request.Header.Set("Authorization", "Bearer test")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || driver.calls.Load() != 0 {
		t.Fatalf("HTTP/1.0 reached the handler or returned an unexpected status: %d", response.Code)
	}
}

func TestReadJSONChunkedLimit(t *testing.T) {
	const limit = 64
	valid := `{"key":"` + strings.Repeat("x", limit-10) + `"}`
	var result map[string]string
	if err := readJSON(strings.NewReader(valid), limit, &result); err != nil {
		t.Fatal(err)
	}
	if err := readJSON(strings.NewReader(valid+" "), limit, &result); err != rpcError(storagedriver.CodeTooLarge) {
		t.Fatalf("oversized request did not get too_large: %v", err)
	}
	if raw, _ := json.Marshal(rpcError(storagedriver.CodeTooLarge)); string(raw) != `{"code":"too_large"}` {
		t.Fatalf("unexpected error JSON: %s", raw)
	}
}
