package networkproxyruntime

import (
	"errors"
	"net"
	"net/http"
	"sync/atomic"
	"time"
)

type readinessSnapshot struct {
	ready bool
	until time.Time
}

// HTTP readers consume only an immutable snapshot. Runner state, certificates,
// controller endpoints and business ingress are never exposed by this listener.
type runtimeHealth struct {
	snapshot atomic.Pointer[readinessSnapshot]
	server   *http.Server
	listener net.Listener
}

func (h *runtimeHealth) start(address string) error {
	if address == "" {
		return nil
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return errors.New("proxy health listener unavailable")
	}
	h.listener = listener
	h.server = &http.Server{Handler: h, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 3 * time.Second, WriteTimeout: 3 * time.Second, IdleTimeout: 5 * time.Second, MaxHeaderBytes: 8 << 10}
	go func() { _ = h.server.Serve(listener); h.invalidate() }()
	return nil
}

func (h *runtimeHealth) close() {
	h.invalidate()
	if h.server != nil {
		_ = h.server.Close()
	}
	if h.listener != nil {
		_ = h.listener.Close()
	}
}

func (h *runtimeHealth) invalidate() { h.snapshot.Store(&readinessSnapshot{}) }

func (h *runtimeHealth) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	switch r.URL.Path {
	case "/healthz":
		w.WriteHeader(http.StatusOK)
	case "/readyz":
		s := h.snapshot.Load()
		if s == nil || !s.ready || !s.until.After(time.Now()) {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (r *Runner) publishReadiness(engineHealthy bool, now time.Time) {
	if !r.configurationAcknowledged() || r.suspended || !r.validUntil.After(now) {
		r.health.invalidate()
		return
	}
	if r.enabled && !engineHealthy {
		r.health.invalidate()
		return
	}
	until := r.validUntil
	if r.config.ManagedDeployment {
		if !r.deployment.mayApply(now) {
			r.health.invalidate()
			return
		}
		until = minTime(until, r.deployment.grant.ExpiresAt)
	}
	// A stalled loop must stop advertising a last-known healthy process even
	// when a longer configuration/control lease has not expired yet.
	until = minTime(until, now.Add(max(2*r.config.PollInterval, 15*time.Second)))
	r.health.snapshot.Store(&readinessSnapshot{ready: true, until: until})
}

func (r *Runner) configurationAcknowledged() bool {
	return r.engine != nil && r.lastAttempt != nil && r.lastAttempt.Status == "applied" && r.attemptReported && r.lastAttempt.Revision == r.appliedRevision && r.seenRevision == r.appliedRevision && r.lastAttempt.ReadbackHash == r.engine.desired.Digest()
}
