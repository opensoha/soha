package virtualization

import (
	"testing"
)

func TestConsoleBackendHeadersReturnsDefensiveHTTPHeader(t *testing.T) {
	result := ConsoleURLResult{
		BackendHeaders: map[string][]string{"Authorization": {"Bearer token"}},
	}
	headers := ConsoleBackendHeaders(result)
	if got := headers.Get("Authorization"); got != "Bearer token" {
		t.Fatalf("Authorization = %q", got)
	}
	headers.Set("Authorization", "changed")
	if got := result.BackendHeaders["Authorization"][0]; got != "Bearer token" {
		t.Fatalf("domain header changed through infrastructure mapping: %q", got)
	}
}

func TestConsoleBackendTLSConfigRejectsInvalidMaterial(t *testing.T) {
	tests := []struct {
		name   string
		config BackendTLS
	}{
		{name: "invalid CA", config: BackendTLS{CAData: []byte("not pem")}},
		{name: "certificate without key", config: BackendTLS{CertData: []byte("cert")}},
		{name: "key without certificate", config: BackendTLS{KeyData: []byte("key")}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ConsoleBackendTLSConfig(ConsoleURLResult{BackendTLS: test.config}); err == nil {
				t.Fatal("ConsoleBackendTLSConfig() error = nil")
			}
		})
	}
}

func TestConsoleBackendTLSConfigMapsPureTransportOptions(t *testing.T) {
	result := ConsoleURLResult{BackendTLS: BackendTLS{
		ServerName:         "cluster.example",
		InsecureSkipVerify: true,
		NextProtos:         []string{"http/1.1"},
	}}
	config, err := ConsoleBackendTLSConfig(result)
	if err != nil {
		t.Fatalf("ConsoleBackendTLSConfig() error = %v", err)
	}
	if config == nil || config.ServerName != "cluster.example" || !config.InsecureSkipVerify {
		t.Fatalf("TLS config = %#v", config)
	}
	config.NextProtos[0] = "changed"
	if result.BackendTLS.NextProtos[0] != "http/1.1" {
		t.Fatal("domain TLS options changed through infrastructure mapping")
	}
}
