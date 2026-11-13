package sohacli

import (
	"context"
	"fmt"
	"github.com/gorilla/websocket"
	contractresource "github.com/opensoha/soha/contracts/resource"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func (client APIClient) dialNativeTerminal(ctx context.Context, path, query, id, ticket string) (*websocket.Conn, error) {
	endpoint, err := url.Parse(client.ServerURL)
	if err != nil || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || endpoint.Scheme != "http" && endpoint.Scheme != "https" {
		return nil, fmt.Errorf("invalid terminal control-plane endpoint")
	}
	if endpoint.Scheme == "https" {
		endpoint.Scheme = "wss"
	} else {
		endpoint.Scheme = "ws"
	}
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + path + "/stream"
	endpoint.RawPath = ""
	endpoint.RawQuery = query
	header := http.Header{"Authorization": []string{"Bearer " + client.effectiveToken(client.Token)}, contractresource.NativePodTerminalTicketHeader: []string{ticket}, "User-Agent": []string{userAgent}}
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second, Proxy: http.ProxyFromEnvironment}
	if client.Timeout > 0 && client.Timeout < dialer.HandshakeTimeout {
		dialer.HandshakeTimeout = client.Timeout
	}
	if transport := client.httpClient().Transport; transport != nil {
		httpTransport, ok := transport.(*http.Transport)
		if !ok {
			return nil, fmt.Errorf("custom HTTP transport does not support native terminal WebSockets")
		}
		dialer.TLSClientConfig = httpTransport.TLSClientConfig
		dialer.NetDialContext = httpTransport.DialContext
		dialer.Proxy = httpTransport.Proxy
	}
	conn, response, err := dialer.DialContext(ctx, endpoint.String(), header)
	if err != nil && response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	if err != nil {
		return nil, fmt.Errorf("native terminal connection was not confirmed; query the original --session-id %s; no reconnect was attempted", id)
	}
	return conn, nil
}
