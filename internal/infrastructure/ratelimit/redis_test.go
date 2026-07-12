package ratelimit

import (
	"testing"
	"time"

	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
)

func TestNewRedisBackendRequiresAddress(t *testing.T) {
	if _, err := NewRedisBackend(cfgpkg.AIGatewayRateLimitConfig{}); err == nil {
		t.Fatalf("expected missing redis address to fail")
	}
}

func TestNewRedisBackendUsesDefaults(t *testing.T) {
	backend, err := NewRedisBackend(cfgpkg.AIGatewayRateLimitConfig{
		Redis: cfgpkg.AIGatewayRateLimitRedisConfig{Addr: "redis:6379"},
	})
	if err != nil {
		t.Fatalf("build redis backend: %v", err)
	}
	defer func() { _ = backend.Close() }()
	if backend.prefix != defaultRedisRateLimitPrefix {
		t.Fatalf("expected default prefix %q, got %q", defaultRedisRateLimitPrefix, backend.prefix)
	}
	if backend.timeout != 500*time.Millisecond {
		t.Fatalf("expected default timeout 500ms, got %s", backend.timeout)
	}
	if got := backend.key("counter", "abc"); got != defaultRedisRateLimitPrefix+":counter:abc" {
		t.Fatalf("unexpected redis key: %s", got)
	}
}
