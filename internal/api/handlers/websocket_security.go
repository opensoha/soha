package handlers

import (
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/gorilla/websocket"
)

const websocketControlMessageReadLimit = 1 << 20

func configureWebSocketReadLimit(conn *websocket.Conn) {
	if conn != nil {
		conn.SetReadLimit(websocketControlMessageReadLimit)
	}
}

func allowWebSocketOrigin(r *http.Request) bool {
	if r == nil || len(r.Header.Values("Origin")) > 1 {
		return false
	}
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	parsed, ok := parseWebSocketOrigin(origin)
	if !ok {
		return false
	}
	if strings.EqualFold(parsed.Host, r.Host) {
		return true
	}
	return isLocalWebSocketHost(parsed.Hostname()) && isLocalWebSocketHost(webSocketHostName(r.Host))
}

func parseWebSocketOrigin(value string) (*url.URL, bool) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return nil, false
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return nil, false
	}
	if parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Hostname() == "" {
		return nil, false
	}
	return parsed, true
}

func webSocketHostName(hostport string) string {
	host, _, err := net.SplitHostPort(hostport)
	if err == nil {
		return strings.Trim(host, "[]")
	}
	return strings.Trim(hostport, "[]")
}

func isLocalWebSocketHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
