package proxytransport

import (
	"bufio"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestProbeConnectionGETAndHEADNoBody(t *testing.T) {
	const expected = "HTTP/1.1 204 No Content\r\nConnection: close\r\n\r\n"
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		t.Run(method, func(t *testing.T) {
			conn := ProbeConnection()
			defer closePacketFixture(t, conn)
			if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
				t.Fatal(err)
			}
			started := time.Now()
			if _, err := io.WriteString(conn, method+" / HTTP/1.1\r\nHost: private-probe.invalid\r\n\r\n"); err != nil {
				t.Fatal(err)
			}
			raw, err := io.ReadAll(conn)
			if err != nil {
				t.Fatal(err)
			}
			if elapsed := time.Since(started); elapsed < time.Millisecond {
				t.Fatalf("private health response rounded to zero milliseconds: %v", elapsed)
			}
			if string(raw) != expected {
				t.Fatalf("response changed: %q", raw)
			}
			response, err := http.ReadResponse(bufio.NewReader(strings.NewReader(string(raw))), &http.Request{Method: method})
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := response.Body.Close(); err != nil {
					t.Errorf("probe response body close: %v", err)
				}
			}()
			body, err := io.ReadAll(response.Body)
			wantLength := int64(0)
			if method == http.MethodHead {
				// HEAD has no body. Without Content-Length the parser records
				// unknown representation length; do not add that header to 204.
				wantLength = -1
			}
			if err != nil || len(body) != 0 || response.ContentLength != wantLength || response.StatusCode != http.StatusNoContent {
				t.Fatalf("HTTP semantics status=%d length=%d body=%q err=%v", response.StatusCode, response.ContentLength, body, err)
			}
		})
	}
}
func TestProbeConnectionRejectsOtherMethods(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodConnect} {
		t.Run(method, func(t *testing.T) {
			conn := ProbeConnection()
			defer closePacketFixture(t, conn)
			_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
			_, err := io.WriteString(conn, method+" / HTTP/1.1\r\nHost: private-probe.invalid\r\nContent-Length: 0\r\n\r\n")
			if err != nil {
				t.Fatal(err)
			}
			response, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: method})
			if response != nil {
				defer func() {
					if err := response.Body.Close(); err != nil {
						t.Errorf("probe response body close: %v", err)
					}
				}()
			}
			if err == nil || response != nil {
				t.Fatal("unsupported method accepted")
			}
		})
	}
}
func TestProbeConnectionBoundsRequestHeaders(t *testing.T) {
	conn := ProbeConnection()
	defer closePacketFixture(t, conn)
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	written := make(chan error, 1)
	go func() {
		_, err := io.WriteString(conn, "HEAD / HTTP/1.1\r\nHost: private-probe.invalid\r\nX-Padding: "+strings.Repeat("x", 17<<10)+"\r\n\r\n")
		written <- err
	}()
	response, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: http.MethodHead})
	if response != nil {
		defer func() {
			if err := response.Body.Close(); err != nil {
				t.Errorf("probe response body close: %v", err)
			}
		}()
	}
	if err == nil || response != nil {
		t.Fatal("oversized headers accepted")
	}
	select {
	case <-written:
	case <-time.After(3 * time.Second):
		t.Fatal("oversized probe left writer blocked")
	}
}
