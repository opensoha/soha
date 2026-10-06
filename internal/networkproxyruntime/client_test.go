package networkproxyruntime

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	domain "github.com/opensoha/soha/internal/domain/networkproxy"
	"github.com/opensoha/soha/internal/networkprotocol"
)

func TestClientUsesNativeTransportWhenDefaultIsReplaced(t *testing.T) {
	previous := http.DefaultTransport
	http.DefaultTransport = nil
	t.Cleanup(func() { http.DefaultTransport = previous })
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS13}
	client := NewClient("https://control.example.invalid", "proxy-test", tlsConfig)
	transport, ok := client.http.Transport.(*http.Transport)
	if !ok || transport.TLSClientConfig != tlsConfig || transport.Proxy == nil {
		t.Fatal("client lost its TLS configuration or environment proxy")
	}
}

func TestCloseResultWithoutReasonMatchesRuntimeSchema(t *testing.T) {
	var received proxyMessage
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
			t.Errorf("decode close result: %v", err)
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	schemas, err := networkprotocol.CompileSchemas()
	if err != nil {
		t.Fatal(err)
	}
	client := NewClient(server.URL, "proxy-mihomo", nil)
	client.SetSchemas(schemas)
	command := domain.CloseCommand{ID: uuid.NewString(), Status: "closed"}
	if err := client.CompleteClose(context.Background(), command); err != nil {
		t.Fatalf("CompleteClose: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(received.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["commandId"] != command.ID || payload["status"] != "closed" {
		t.Fatalf("close result payload = %v", payload)
	}
	if _, present := payload["reasonCode"]; present {
		t.Fatal("empty reasonCode must be omitted")
	}
}
