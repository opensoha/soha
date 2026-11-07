package virtualization

import (
	"context"
	"net/url"
	"strings"

	"github.com/gorilla/websocket"
	"github.com/opensoha/soha/internal/application/virtualization/consoleport"
)

// DialConsole negotiates the upstream WebSocket for a prepared VNC console.
func DialConsole(ctx context.Context, result ConsoleURLResult) (consoleport.Stream, error) {
	var subprotocols []string
	var tlsError, connectionError consoleport.DialError
	switch {
	case result.Type == "novnc" && result.Token != "":
		subprotocols = []string{"binary"}
		tlsError, connectionError = consoleport.ErrInvalidBackendTLS, consoleport.ErrBackendConnection
	case result.Type == "vnc":
		tlsError, connectionError = consoleport.ErrInvalidKubeVirtTLS, consoleport.ErrKubeVirtConnection
	default:
		return nil, consoleport.ErrUnsupportedConsole
	}

	backendURL := result.BackendURL
	if strings.TrimSpace(backendURL) == "" {
		backendURL = result.URL
	}
	parsedURL, err := url.Parse(backendURL)
	if err != nil {
		return nil, consoleport.ErrInvalidBackendURL
	}
	parsedURL.Scheme = "wss"
	if strings.HasPrefix(backendURL, "http://") {
		parsedURL.Scheme = "ws"
	}
	if result.Type == "novnc" {
		query := parsedURL.Query()
		if query.Get("vncticket") == "" {
			query.Set("vncticket", result.Token)
			parsedURL.RawQuery = query.Encode()
		}
	}

	tlsConfig, err := ConsoleBackendTLSConfig(result)
	if err != nil {
		return nil, tlsError
	}
	dialer := *websocket.DefaultDialer
	dialer.TLSClientConfig = tlsConfig
	if len(subprotocols) != 0 {
		dialer.Subprotocols = subprotocols
	}
	conn, response, err := dialer.DialContext(ctx, parsedURL.String(), ConsoleBackendHeaders(result))
	if response != nil {
		_ = response.Body.Close()
	}
	if err != nil {
		return nil, connectionError
	}
	return conn, nil
}
