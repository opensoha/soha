package resourcebackend

import (
	"context"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/streaming/pkg/httpstream"
	streamspdy "k8s.io/streaming/pkg/httpstream/spdy"
)

type portForwardFunc func() error

func (f portForwardFunc) ForwardPorts() error { return f() }

func awaitPortForwardSignal(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for port forward")
	}
}

func TestDirectPortForwardReconnectsLostConnection(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	s := &directPortForwardSession{cancel: cancel, doneCh: make(chan struct{})}
	firstReady, disconnected, recovering, allowReady, recovered := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	var attempts atomic.Int32
	factory := func(ctx context.Context, ready chan struct{}) (portForwardRunner, error) {
		attempt := attempts.Add(1)
		return portForwardFunc(func() error {
			if attempt == 1 {
				close(ready)
				<-disconnected
				return portforward.ErrLostConnectionToPod
			}
			close(recovering)
			<-allowReady
			close(ready)
			<-ctx.Done()
			close(recovered)
			return nil
		}), nil
	}
	go s.run(ctx, factory, firstReady, time.Millisecond)
	t.Cleanup(s.Stop)
	awaitPortForwardSignal(t, firstReady)
	close(disconnected)
	awaitPortForwardSignal(t, recovering)
	if !strings.Contains(s.LastError(), portforward.ErrLostConnectionToPod.Error()) {
		t.Fatalf("disconnect was not exposed: %q", s.LastError())
	}
	close(allowReady)
	deadline := time.Now().Add(3 * time.Second)
	for s.LastError() != "" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if s.LastError() != "" {
		t.Fatalf("ready did not clear disconnect error: %s", s.LastError())
	}
	s.Stop()
	awaitPortForwardSignal(t, recovered)
	if attempts.Load() != 2 {
		t.Fatalf("attempts = %d, want 2", attempts.Load())
	}
}

func TestDirectPortForwardReconnectIsBounded(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	s := &directPortForwardSession{cancel: cancel, doneCh: make(chan struct{})}
	var attempts atomic.Int32
	go s.run(ctx, func(context.Context, chan struct{}) (portForwardRunner, error) {
		attempts.Add(1)
		return portForwardFunc(func() error { return portforward.ErrLostConnectionToPod }), nil
	}, make(chan struct{}), time.Millisecond)
	t.Cleanup(s.Stop)
	awaitPortForwardSignal(t, s.doneCh)
	if attempts.Load() != 6 || !strings.Contains(s.LastError(), "reconnect limit") {
		t.Fatalf("attempts=%d error=%q", attempts.Load(), s.LastError())
	}
}

func TestDirectPortForwardStopCancelsBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	s := &directPortForwardSession{cancel: cancel, doneCh: make(chan struct{})}
	var attempts atomic.Int32
	failed := make(chan struct{})
	go s.run(ctx, func(context.Context, chan struct{}) (portForwardRunner, error) {
		attempts.Add(1)
		return portForwardFunc(func() error { close(failed); return portforward.ErrLostConnectionToPod }), nil
	}, make(chan struct{}), time.Hour)
	awaitPortForwardSignal(t, failed)
	s.Stop()
	awaitPortForwardSignal(t, s.doneCh)
	if attempts.Load() != 1 {
		t.Fatalf("stopped session reconnected: %d", attempts.Load())
	}
}

func TestDirectPortForwardDoesNotRetryPermanentErrors(t *testing.T) {
	for _, test := range []struct {
		name       string
		buildError bool
	}{{"target resolution", true}, {"upgrade or binding", false}} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			s := &directPortForwardSession{cancel: cancel, doneCh: make(chan struct{})}
			failure := errors.New("forbidden or invalid target")
			var attempts atomic.Int32
			go s.run(ctx, func(context.Context, chan struct{}) (portForwardRunner, error) {
				attempts.Add(1)
				if test.buildError {
					return nil, failure
				}
				return portForwardFunc(func() error { return failure }), nil
			}, make(chan struct{}), time.Millisecond)
			t.Cleanup(s.Stop)
			awaitPortForwardSignal(t, s.doneCh)
			if attempts.Load() != 1 || !errors.Is(s.error(), failure) {
				t.Fatalf("attempts=%d error=%v", attempts.Load(), s.error())
			}
		})
	}
}

func TestDirectPortForwardOutlivesStartupRequest(t *testing.T) {
	requestCtx, cancelRequest := context.WithCancel(context.Background())
	closed := make(chan struct{})
	s, err := startDirectPortForward(requestCtx, func(ctx context.Context, ready chan struct{}) (portForwardRunner, error) {
		return portForwardFunc(func() error {
			close(ready)
			<-ctx.Done()
			close(closed)
			return nil
		}), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Stop)
	cancelRequest()
	select {
	case <-closed:
		t.Fatal("request completion stopped the session")
	case <-time.After(20 * time.Millisecond):
	}
	s.Stop()
	awaitPortForwardSignal(t, closed)
}

func TestDirectPortForwardStartupFailureReturnsCause(t *testing.T) {
	failure := errors.New("invalid port")
	s, err := startDirectPortForward(context.Background(), func(context.Context, chan struct{}) (portForwardRunner, error) {
		return nil, failure
	})
	if s != nil || !errors.Is(err, failure) {
		t.Fatalf("session=%v error=%v", s, err)
	}
}

func TestDirectPortForwardSDKReconnectRebindsOriginalPort(t *testing.T) {
	connections := make(chan httpstream.Connection, 8)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(httpstream.HeaderProtocolVersion, portforward.PortForwardProtocolV1Name)
		conn := streamspdy.NewResponseUpgrader().UpgradeResponse(w, r, func(httpstream.Stream, <-chan struct{}) error { return nil })
		if conn != nil {
			connections <- conn
			<-conn.CloseChan()
		}
	}))
	t.Cleanup(server.Close)
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_, port, _ := net.SplitHostPort(address)
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	serverURL, _ := url.Parse(server.URL)
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	config := &rest.Config{Host: server.URL, TLSClientConfig: rest.TLSClientConfig{CAData: ca}}
	session, err := startDirectPortForward(context.Background(), func(ctx context.Context, ready chan struct{}) (portForwardRunner, error) {
		dialer, err := newDirectPortForwardDialer(ctx, config, serverURL)
		if err != nil {
			return nil, err
		}
		return portforward.NewOnAddressesForStreamingWithContext(ctx, dialer, []string{"127.0.0.1"}, []string{port + ":8080"}, ready, io.Discard, io.Discard)
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(session.Stop)
	first := <-connections
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case second := <-connections:
		t.Cleanup(func() { _ = second.Close() })
	case <-time.After(5 * time.Second):
		t.Fatal("SDK connection was not recreated")
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		conn, dialErr := net.DialTimeout("tcp4", address, 50*time.Millisecond)
		if dialErr == nil {
			_ = conn.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("original port was not rebound: %v", dialErr)
		}
		time.Sleep(time.Millisecond)
	}
	if session.LastError() != "" {
		t.Fatalf("recovered SDK session still has error: %s", session.LastError())
	}
	session.Stop()
	rebound, err := net.Listen("tcp4", address)
	if err != nil {
		t.Fatalf("Stop returned before the original port was released: %v", err)
	}
	_ = rebound.Close()
}

func TestDirectPortForwardHandshakeStopsWhileWaitingForUpgrade(t *testing.T) {
	for _, tc := range []struct {
		name    string
		timeout time.Duration
		cancel  bool
		want    error
	}{
		{name: "session canceled", timeout: time.Minute, cancel: true, want: context.Canceled},
		{name: "upgrade deadline elapsed", timeout: 500 * time.Millisecond, want: context.DeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			received, disconnected := make(chan struct{}), make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				close(received)
				<-r.Context().Done()
				close(disconnected)
			}))
			defer server.Close()
			endpoint, err := url.Parse(server.URL)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			dialer, err := newDirectPortForwardDialer(ctx, &rest.Config{Host: server.URL}, endpoint)
			if err != nil {
				t.Fatal(err)
			}
			dialer.timeout = tc.timeout
			result := make(chan error, 1)
			go func() {
				conn, _, err := dialer.Dial(portforward.PortForwardProtocolV1Name)
				if conn != nil {
					_ = conn.Close()
				}
				result <- err
			}()
			awaitPortForwardSignal(t, received)
			if tc.cancel {
				cancel()
			}
			select {
			case err := <-result:
				if !errors.Is(err, tc.want) {
					t.Fatalf("handshake error = %v, want %v", err, tc.want)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("upgrade request did not return")
			}
			awaitPortForwardSignal(t, disconnected)
		})
	}
}
