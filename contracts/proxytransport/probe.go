package proxytransport

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"time"
)

// ProbeConnection returns a bounded, local HTTP responder used only after a
// consumer has matched its unpredictable, private health-probe hostname.
// It never opens a network socket or forwards data to a real destination.
func ProbeConnection() net.Conn {
	client, server := net.Pipe()
	go func() {
		defer func() { _ = server.Close() }()
		_ = server.SetDeadline(time.Now().Add(2 * time.Second))
		request, err := http.ReadRequest(bufio.NewReader(io.LimitReader(server, 16<<10)))
		if err != nil {
			return
		}
		_ = request.Body.Close()
		if request.Method != http.MethodGet {
			return
		}
		_, _ = io.WriteString(server, "HTTP/1.1 204 No Content\r\nConnection: close\r\n\r\n")
	}()
	return client
}
