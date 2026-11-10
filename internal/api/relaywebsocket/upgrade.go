// Package relaywebsocket owns the downstream AI Gateway WebSocket handshake.
package relaywebsocket

import (
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/gorilla/websocket"
	"github.com/opensoha/soha/internal/application/aigateway/relayport"
)

func Upgrader(writer http.ResponseWriter, req *http.Request) relayport.UpgradeWebSocket {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return relayAllowWebSocketOrigin(r)
		},
	}
	return func(headers http.Header) (relayport.WebSocketConn, error) {
		return upgrader.Upgrade(writer, req, headers)
	}
}

func relayAllowWebSocketOrigin(r *http.Request) bool {
	if r == nil || len(r.Header.Values("Origin")) > 1 {
		return false
	}
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return false
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return false
	}
	if parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Hostname() == "" {
		return false
	}
	if strings.EqualFold(parsed.Host, r.Host) {
		return true
	}
	return relayIsLocalHost(parsed.Hostname()) && relayIsLocalHost(relayHostName(r.Host))
}

func relayHostName(hostport string) string {
	host, _, err := net.SplitHostPort(hostport)
	if err == nil {
		return strings.Trim(host, "[]")
	}
	return strings.Trim(hostport, "[]")
}

func relayIsLocalHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	return host == "localhost" || host == "127.0.0.1" || host == "::1" || strings.HasSuffix(host, ".localhost")
}
