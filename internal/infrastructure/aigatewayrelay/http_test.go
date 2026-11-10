package aigatewayrelay

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/opensoha/soha/internal/application/aigateway/relayport"
)

func TestRelayHTTPTransportEnforcesStreamDeadlines(t *testing.T) {
	for _, test := range []struct {
		name         string
		flushHeaders bool
		failureCode  string
	}{
		{name: "first byte", failureCode: "upstream_request_failed"},
		{name: "idle body", flushHeaders: true, failureCode: "upstream_read_failed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			cancelled := make(chan struct{})
			release := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				if test.flushHeaders {
					w.WriteHeader(http.StatusOK)
					if err := http.NewResponseController(w).Flush(); err != nil {
						t.Errorf("flush response headers: %v", err)
					}
				}
				select {
				case <-r.Context().Done():
					close(cancelled)
				case <-release:
				}
			}))
			defer server.Close()
			defer close(release)
			adapter := New(Config{AllowInsecureUpstreamHTTP: true, AllowPrivateUpstreamHosts: true})
			opts := relayport.HTTPOptions{
				Timeout: 3 * time.Second, FirstByteTimeout: 100 * time.Millisecond, IdleTimeout: 100 * time.Millisecond,
				StartStream: func(*http.Response) io.Writer { return io.Discard },
			}
			started := time.Now()
			result, err := executeRelayTestRequest(context.Background(), adapter, relayport.Target{BaseURL: server.URL, ProviderKind: "openai"}, opts)
			if err != nil || result.Failure == nil || result.Failure.Code != test.failureCode || result.Wrote != test.flushHeaders {
				t.Fatalf("execute result = %+v, err = %v", result, err)
			}
			if elapsed := time.Since(started); elapsed >= time.Second {
				t.Fatalf("deadline elapsed = %v", elapsed)
			}
			select {
			case <-cancelled:
			case <-time.After(time.Second):
				t.Fatal("upstream did not observe stream cancellation")
			}
		})
	}
}

func TestRelayHTTPTransportResponseLimits(t *testing.T) {
	t.Run("non-stream rejects oversized response", func(t *testing.T) {
		body := strings.Repeat("x", relayMaxNonStreamResponseBytes+1)
		resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}
		result := readResponse(resp, relayport.HTTPResult{Response: resp}, nil, 0, func() {})
		if result.Failure == nil || result.Failure.Code != "upstream_response_too_large" {
			t.Fatalf("failure = %+v", result.Failure)
		}
		if len(result.Body) != relayMaxNonStreamResponseBytes || !result.BodyTruncated {
			t.Fatalf("buffered=%d truncated=%v", len(result.Body), result.BodyTruncated)
		}
	})
	t.Run("stream retains bounded audit tail", func(t *testing.T) {
		body := strings.Repeat("prefix", relayStreamAuditBufferBytes/6+100) + "usage-tail"
		resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}
		writer := httptest.NewRecorder()
		result := readResponse(resp, relayport.HTTPResult{Response: resp}, writer, 0, func() {})
		if result.Failure != nil || !result.Wrote || result.OutputBytes != int64(len(body)) {
			t.Fatalf("failure=%+v wrote=%v output bytes=%d", result.Failure, result.Wrote, result.OutputBytes)
		}
		if len(result.Body) != relayStreamAuditBufferBytes || !result.BodyTruncated || !strings.HasSuffix(string(result.Body), "usage-tail") {
			t.Fatalf("buffered=%d truncated=%v", len(result.Body), result.BodyTruncated)
		}
		if writer.Body.Len() != len(body) {
			t.Fatalf("forwarded bytes=%d, want %d", writer.Body.Len(), len(body))
		}
	})
}

func TestRelayHTTPTransportUsesOperationDeadline(t *testing.T) {
	if got := New(Config{}).client.Timeout; got != 0 {
		t.Fatalf("HTTP client timeout = %v, want request context to own deadlines", got)
	}
}
