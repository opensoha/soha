package agent

import (
	"context"
	"net"
	"net/http"
	"strings"
	"time"
)

// Share the connection pool, not credentials: each Client keeps its current token.
func (r *Registry) reverseTransport(clusterID string) *http.Transport {
	clusterID = strings.TrimSpace(clusterID)
	r.sessionsMu.Lock()
	defer r.sessionsMu.Unlock()
	if transport := r.transports[clusterID]; transport != nil {
		return transport
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return r.Open(ctx, clusterID)
		},
		MaxConnsPerHost:       8,
		MaxIdleConnsPerHost:   8,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: time.Duration(r.defaultTimeout.Load()),
	}
	r.transports[clusterID] = transport
	return transport
}

// Caller holds sessionsMu; replacement/shutdown must discard old idle streams.
func (r *Registry) closeReverseTransportLocked(clusterID string) {
	if transport := r.transports[clusterID]; transport != nil {
		transport.CloseIdleConnections()
		delete(r.transports, clusterID)
	}
}
