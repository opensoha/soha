package resourcebackend

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"k8s.io/client-go/tools/portforward"
)

type portForwardRunner interface {
	ForwardPorts() error
}

type portForwardFactory func(context.Context, chan struct{}) (portForwardRunner, error)

type directPortForwardSession struct {
	cancel  context.CancelFunc
	doneCh  chan struct{}
	errMu   sync.RWMutex
	lastErr error
}

func startDirectPortForward(ctx context.Context, factory portForwardFactory) (*directPortForwardSession, error) {
	// The HTTP request owns startup only; the persisted session owns its lifetime.
	sessionCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	session := &directPortForwardSession{cancel: cancel, doneCh: make(chan struct{})}
	ready := make(chan struct{})
	go session.run(sessionCtx, factory, ready, time.Second)
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		session.Stop()
		return nil, ctx.Err()
	case <-timer.C:
		session.Stop()
		return nil, fmt.Errorf("port forward did not become ready within 10s")
	case <-session.doneCh:
		session.Stop()
		return nil, fmt.Errorf("start port forward: %w", session.error())
	case <-ready:
		return session, nil
	}
}

func (s *directPortForwardSession) run(ctx context.Context, factory portForwardFactory, firstReady chan struct{}, retryDelay time.Duration) {
	defer close(s.doneCh)
	defer s.cancel()
	var readyOnce sync.Once
	retries := 0
	for ctx.Err() == nil {
		var readyAt time.Time
		err := runPortForwardAttempt(ctx, factory, func() {
			readyAt = time.Now()
			s.setError(nil)
			readyOnce.Do(func() { close(firstReady) })
		})
		if ctx.Err() != nil {
			return
		}
		if err == nil {
			err = errors.New("port forward ended unexpectedly")
		}
		s.setError(err)
		// Never retry configuration, authorization, binding, or upgrade errors.
		if !errors.Is(err, portforward.ErrLostConnectionToPod) {
			return
		}
		if !readyAt.IsZero() && time.Since(readyAt) >= time.Minute {
			retries = 0
		}
		retries++
		if retries > 5 {
			s.setError(fmt.Errorf("port forward reconnect limit reached: %w", err))
			return
		}
		timer := time.NewTimer(retryDelay * time.Duration(1<<(retries-1)))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func runPortForwardAttempt(ctx context.Context, factory portForwardFactory, onReady func()) error {
	ready := make(chan struct{})
	forwarder, err := factory(ctx, ready)
	if err != nil {
		return err
	}
	result := make(chan error, 1)
	go func() { result <- forwarder.ForwardPorts() }()
	select {
	case <-ctx.Done():
		return waitForPortForwardStop(ctx, result)
	case err := <-result:
		return err
	case <-ready:
		onReady()
	}
	select {
	case <-ctx.Done():
		return waitForPortForwardStop(ctx, result)
	case err := <-result:
		return err
	}
}

func waitForPortForwardStop(ctx context.Context, result <-chan error) error {
	// ForwardPorts owns the listener. Wait for its deferred Close before reporting
	// a stopped session, while still bounding an unresponsive upgrade handshake.
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case <-result:
	case <-timer.C:
	}
	return ctx.Err()
}

func (s *directPortForwardSession) Stop() {
	if s == nil {
		return
	}
	s.cancel()
	select {
	case <-s.doneCh:
	case <-time.After(5 * time.Second):
	}
}

func (s *directPortForwardSession) LastError() string {
	if s == nil {
		return ""
	}
	if err := s.error(); err != nil {
		return err.Error()
	}
	return ""
}

func (s *directPortForwardSession) error() error {
	s.errMu.RLock()
	defer s.errMu.RUnlock()
	return s.lastErr
}

func (s *directPortForwardSession) setError(err error) {
	s.errMu.Lock()
	s.lastErr = err
	s.errMu.Unlock()
}
