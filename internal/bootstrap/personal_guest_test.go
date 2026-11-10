package bootstrap

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
)

func TestPersonalGuestServerIsDisabledUnlessFullyConfigured(t *testing.T) {
	server, preparation, err := newPersonalGuestServer(cfgpkg.PersonalGuestConfig{}, nil, nil, nil)
	if err != nil || server != nil || preparation != nil {
		t.Fatal("existing startup gained guest listener", err)
	}
	if _, _, err := newPersonalGuestServer(cfgpkg.PersonalGuestConfig{Enabled: true}, nil, nil, nil); err == nil {
		t.Fatal("incomplete guest configuration accepted")
	}
}

func guestLifecycleTLS(t *testing.T) (*tls.Config, *tls.Config) {
	t.Helper()
	testServer := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(testServer.Close)
	pool := x509.NewCertPool()
	pool.AddCert(testServer.Certificate())
	serverTLS := testServer.TLS.Clone()
	serverTLS.MinVersion = tls.VersionTLS13
	serverTLS.ClientAuth = tls.RequireAndVerifyClientCert
	serverTLS.ClientCAs = pool
	return serverTLS, &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: pool}
}

func guestLifecycleAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return address
}

func TestPersonalGuestLifecycleStartsBothListenersAndShutsDown(t *testing.T) {
	serverTLS, clientTLS := guestLifecycleTLS(t)
	app := &App{HTTP: &http.Server{Addr: guestLifecycleAddress(t), ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })},
		PersonalGuest: &http.Server{Addr: guestLifecycleAddress(t), ReadHeaderTimeout: time.Second, TLSConfig: serverTLS, Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })}}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := app.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	})
	result := make(chan error, 1)
	go func() { result <- app.Run() }()
	client := &http.Client{Timeout: 100 * time.Millisecond}
	deadline := time.Now().Add(3 * time.Second)
	for {
		response, err := client.Get("http://" + app.HTTP.Addr)
		if err == nil {
			if err := response.Body.Close(); err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != http.StatusNoContent {
				t.Fatal("management listener returned unexpected status")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("management listener did not start", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	transport := &http.Transport{TLSClientConfig: clientTLS}
	defer transport.CloseIdleConnections()
	guestClient := &http.Client{Transport: transport, Timeout: time.Second}
	response, err := guestClient.Get("https://" + app.PersonalGuest.Addr)
	if err == nil {
		if closeErr := response.Body.Close(); closeErr != nil {
			t.Fatal(closeErr)
		}
		t.Fatal("guest listener accepted a client without a certificate")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := app.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("dual listeners did not terminate")
	}
}

func TestPersonalGuestLifecycleFailsBeforeManagementStartup(t *testing.T) {
	serverTLS, _ := guestLifecycleTLS(t)
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := occupied.Close(); err != nil {
			t.Error(err)
		}
	}()
	app := &App{HTTP: &http.Server{Addr: guestLifecycleAddress(t), ReadHeaderTimeout: time.Second}, PersonalGuest: &http.Server{Addr: occupied.Addr().String(), ReadHeaderTimeout: time.Second, TLSConfig: serverTLS}}
	if err := app.Run(); err == nil {
		t.Fatal("occupied guest listener was ignored")
	}
	app.PersonalGuest.TLSConfig = nil
	if err := app.Run(); err == nil {
		t.Fatal("guest lifecycle accepted plaintext")
	}
}
