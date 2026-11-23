// Package sdktest starts a private storage RPC server for driver conformance tests.
package sdktest

import (
	"context"
	"crypto/rand"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/opensoha/soha/contracts/plugins/storagedriver"
	"github.com/opensoha/soha/contracts/plugins/storagedriver/sdk"
)

// NewClient serves driver over a real Unix domain socket and returns an authenticated
// client. It owns the socket, server and client until t's cleanup. Drivers can use
// it to exercise the same streaming, validation and cancellation path as production.
func NewClient(t testing.TB, options sdk.ServerOptions, driver storagedriver.Driver) *sdk.Client {
	t.Helper()
	dir, err := os.MkdirTemp("", "soha-storage-")
	if err != nil {
		t.Fatalf("create private storage SDK test directory: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	address := filepath.Join(dir, "s")
	secret := make([]byte, storagedriver.LaunchSecretBytes)
	if _, err := rand.Read(secret); err != nil {
		t.Fatalf("generate storage SDK test launch secret: %v", err)
	}
	defer clear(secret)
	input, output := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	var serveErr error
	go func() {
		defer close(stopped)
		defer func() { _ = input.Close() }()
		serveErr = sdk.Serve(ctx, input, options, driver)
	}()
	t.Cleanup(func() {
		cancel()
		_ = input.Close()
		_ = output.Close()
		select {
		case <-stopped:
			if serveErr != nil {
				t.Errorf("storage SDK test server failed: %v", serveErr)
			}
		case <-time.After(5 * time.Second):
			t.Error("storage SDK test server did not stop")
		}
	})
	if err := sdk.WriteBootstrap(output, sdk.Bootstrap{Network: "unix", Address: address, LaunchSecret: secret}); err != nil {
		t.Fatalf("write storage SDK test bootstrap: %v", err)
	}
	_ = output.Close()
	startup, stop := context.WithTimeout(ctx, 5*time.Second)
	defer stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	dialer := net.Dialer{}
	for {
		probe, err := dialer.DialContext(startup, "unix", address)
		if err == nil {
			_ = probe.Close()
			break
		}
		select {
		case <-stopped:
			t.Fatalf("storage SDK test server stopped before listening: %v", serveErr)
		case <-startup.Done():
			t.Fatal("storage SDK test listener did not become ready")
		case <-ticker.C:
		}
	}
	identity := storagedriver.Identity{
		PluginID: options.PluginID, Version: options.Version,
		ProtocolMajor: storagedriver.ProtocolMajor, Capabilities: storagedriver.V1Capabilities,
	}
	client, err := sdk.Dial(startup, sdk.ClientOptions{
		Network: "unix", Address: address, Expected: identity, LaunchSecret: secret,
	})
	if err != nil {
		t.Fatalf("authenticate storage SDK test client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}
