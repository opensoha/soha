package handlers

import (
	"crypto/tls"
	"testing"

	infravirtualization "github.com/opensoha/soha/internal/infrastructure/virtualization"
)

func TestBackendWebSocketDialerUsesConsoleTLSOptions(t *testing.T) {
	result := infravirtualization.ConsoleURLResult{
		BackendTLSConfig:      &tls.Config{ServerName: "k8s.example"},
		InsecureSkipTLSVerify: true,
	}

	dialer := backendWebSocketDialer(result)
	if dialer.TLSClientConfig == nil {
		t.Fatalf("TLSClientConfig is nil")
	}
	if dialer.TLSClientConfig == result.BackendTLSConfig {
		t.Fatalf("TLSClientConfig should be cloned")
	}
	if dialer.TLSClientConfig.ServerName != "k8s.example" {
		t.Fatalf("ServerName = %q", dialer.TLSClientConfig.ServerName)
	}
	if !dialer.TLSClientConfig.InsecureSkipVerify {
		t.Fatalf("InsecureSkipVerify = false, want true")
	}
}
