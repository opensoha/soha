package networkproxyruntime

import (
	"context"
	"errors"
	api "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"sync"
	"testing"
	"time"
)

func TestIngressHTTPTraceConcurrentCallbacksAndError(t *testing.T) {
	trace := newIngressHTTPTrace()
	hooks := httptrace.ContextClientTrace(trace.context(context.Background()))
	cause := errors.New("private hostname and body")
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			hooks.GetConn("private-host")
			hooks.GotConn(httptrace.GotConnInfo{Reused: true})
			hooks.WroteRequest(httptrace.WroteRequestInfo{Err: cause})
			_ = trace.wrap(cause)
		}()
	}
	wg.Wait()
	err := trace.wrap(cause)
	if !errors.Is(err, cause) || err.Error() != cause.Error() {
		t.Fatal("error semantics changed")
	}
	var wrapped *ingressHTTPError
	if !errors.As(err, &wrapped) || !wrapped.trace.getConn.seen || !wrapped.trace.gotConn.seen || !wrapped.trace.reused || !wrapped.trace.wrote.seen || wrapped.trace.firstByte.seen {
		t.Fatal("unexpected trace snapshot")
	}
}

func TestIngressHTTPTraceRequestStages(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if r.URL.Query().Get("block") != "" {
			<-release
			return
		}
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()
	c := NewClient(server.URL, "private", nil)
	// The first failed HTTP status still returns its connection to the pool.
	_, _ = c.CheckIngress(context.Background(), api.NetworkProxyIngressCheckInput{})
	_, err := c.CheckIngress(context.Background(), api.NetworkProxyIngressCheckInput{})
	var traced *ingressHTTPError
	if !errors.As(err, &traced) || !traced.trace.gotConn.seen || !traced.trace.reused || !traced.trace.wrote.seen || !traced.trace.firstByte.seen {
		t.Fatalf("reused request: %#v", traced)
	}
	// Use a blocking server to capture a request awaiting its first byte.
	blocked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.Copy(io.Discard, r.Body); <-release }))
	defer blocked.Close()
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	c = NewClient(blocked.URL, "private", nil)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = c.CheckIngress(ctx, api.NetworkProxyIngressCheckInput{})
	if !errors.As(err, &traced) || !traced.trace.gotConn.seen || !traced.trace.wrote.seen || traced.trace.firstByte.seen || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("blocked response: %#v %v", traced, err)
	}
	releaseOnce.Do(func() { close(release) })
	c = NewClient("http://private.invalid", "private", nil)
	c.http.Transport = &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) { <-ctx.Done(); return nil, ctx.Err() }}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel2()
	_, err = c.CheckIngress(ctx2, api.NetworkProxyIngressCheckInput{})
	if !errors.As(err, &traced) || !traced.trace.getConn.seen || traced.trace.gotConn.seen || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("blocked dial: %#v %v", traced, err)
	}
}
