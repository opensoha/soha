package aigateway

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestRelayHTTPOptionsBoundNonStreamByTotalDeadline(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		stream, flush, cancel bool
		delay, total          time.Duration
		code                  string
	}{
		{name: "slow nonstream headers succeed", delay: 150 * time.Millisecond, total: time.Second},
		{name: "stream first byte remains bounded", stream: true, total: time.Second, code: "upstream_request_failed"},
		{name: "stream idle remains bounded", stream: true, flush: true, total: time.Second, code: "upstream_read_failed"},
		{name: "nonstream total timeout", total: 80 * time.Millisecond, code: "upstream_request_failed"},
		{name: "nonstream client cancellation", cancel: true, total: time.Second, code: "upstream_request_failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entered := make(chan struct{})
			observedCancel := make(chan struct{})
			release := make(chan struct{})
			var once sync.Once
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				once.Do(func() { close(entered) })
				if tc.flush {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, "data: {\"choices\":[]}\n\n")
					if e := http.NewResponseController(w).Flush(); e != nil {
						t.Error(e)
					}
				}
				if tc.delay > 0 {
					timer := time.NewTimer(tc.delay)
					defer timer.Stop()
					select {
					case <-timer.C:
						w.Header().Set("Content-Type", "application/json")
						_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
						return
					case <-release:
						return
					case <-r.Context().Done():
						close(observedCancel)
						return
					}
				}
				select {
				case <-r.Context().Done():
					close(observedCancel)
				case <-release:
				}
			}))
			defer upstream.Close()
			defer close(release)
			repo := relayRepoForUpstream(t, upstream.URL, "openai", "synthetic-upstream-key")
			service := newRelayRuntimeTestService(repo)
			service.relayConfig.DefaultTimeout = tc.total
			service.relayConfig.StreamTimeout = tc.total
			service.relayConfig.FirstByteTimeout = 50 * time.Millisecond
			service.relayConfig.StreamIdleTimeout = 50 * time.Millisecond
			request := LLMRelayHTTPRequest{ProviderKind: "openai", Endpoint: "chat/completions", Method: http.MethodPost, Headers: http.Header{}, Body: []byte(`{"model":"gpt-public","messages":[{"role":"user","content":"hello"}]}`), RequestID: "deadline-test"}
			if tc.stream {
				request.Body = []byte(`{"model":"gpt-public","stream":true,"messages":[{"role":"user","content":"hello"}]}`)
			}
			opts, e := service.relayHTTPOptions(request, relaySelection{upstream: repo.upstreams[0], route: repo.routes[0]}, "gpt-public", tc.stream, httptest.NewRecorder(), relayCacheBypass)
			if e != nil {
				t.Fatal(e)
			}
			if opts.Timeout != tc.total {
				t.Fatal("total deadline changed")
			}
			if tc.stream {
				if opts.FirstByteTimeout != 50*time.Millisecond || opts.IdleTimeout != 50*time.Millisecond || opts.StartStream == nil {
					t.Fatal("stream deadline behavior changed")
				}
			} else if opts.FirstByteTimeout != 0 || opts.IdleTimeout != 0 || opts.StartStream != nil {
				t.Fatal("nonstream inherited stream timing limits")
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cancelDone := make(chan struct{})
			if tc.cancel {
				go func() {
					defer close(cancelDone)
					select {
					case <-entered:
						cancel()
					case <-ctx.Done():
					}
				}()
			} else {
				close(cancelDone)
			}
			writer := httptest.NewRecorder()
			err := service.RelayLLMHTTP(ctx, relayTestPrincipal(), relayTestAccessContext(nil), request, writer)
			cancel()
			<-cancelDone
			log := singleRelayLog(t, repo)
			if tc.cancel && log.Status != "client_cancelled" {
				t.Fatalf("caller cancellation status lost: %s", log.Status)
			}
			if tc.code == "" {
				if err != nil || log.Status != "success" || writer.Code != http.StatusOK {
					t.Fatalf("slow nonstream rejected: status=%s code=%s error=%v", log.Status, log.ErrorCode, err)
				}
			} else {
				if log.ErrorCode != tc.code || log.Status == "success" {
					t.Fatalf("deadline classification: status=%s code=%s want=%s error=%v", log.Status, log.ErrorCode, tc.code, err)
				}
				select {
				case <-observedCancel:
				case <-time.After(time.Second):
					t.Fatal("upstream did not observe bounded cancellation")
				}
			}
		})
	}
}
