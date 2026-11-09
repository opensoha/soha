package bootstrap

import (
	"context"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	config "github.com/opensoha/soha/internal/infrastructure/config"
)

type workspaceProtection struct {
	set *domain.ProtectedDestinations
	err error
}

func (p workspaceProtection) CurrentProtection(context.Context) (*domain.ProtectedDestinations, error) {
	return p.set, p.err
}

func TestProxyWorkspaceSourceProtection(t *testing.T) {
	ctx := context.Background()
	store := workspaceProtection{set: &domain.ProtectedDestinations{PolicyVersion: 2, CIDRs: []string{"203.0.113.0/24"}, Hosts: []string{"*.Private.Example."}}}
	ip := proxySourceProtection(store)
	host := proxySourceHostProtection(store)
	if !ip(ctx, netip.MustParseAddr("203.0.113.2")) || ip(ctx, netip.MustParseAddr("8.8.8.8")) || !host(ctx, "private.example") || !host(ctx, "a.private.example.") || host(ctx, "notprivate.example") {
		t.Fatal("source protection did not preserve CIDR and domain suffix boundaries")
	}
	for _, unavailable := range []workspaceProtection{{}, {err: errors.New("offline")}, {set: &domain.ProtectedDestinations{PolicyVersion: 0}}} {
		if !proxySourceProtection(unavailable)(ctx, netip.MustParseAddr("8.8.8.8")) || !proxySourceHostProtection(unavailable)(ctx, "public.example") {
			t.Fatal("missing current protection allowed a source fetch")
		}
	}
}

func TestProxyWorkspaceExecutorCredentialIsolation(t *testing.T) {
	directory := t.TempDir()
	file := func(name, value string) string {
		path := filepath.Join(directory, name)
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	shared := strings.Repeat("a", 32)
	other := strings.Repeat("b", 32)
	cfg := config.Config{Runtime: config.RuntimeConfig{ExecutionRunnerToken: shared,
		ProxyRuntimeExecutors: []config.ProxyRuntimeExecutorConfig{{ExecutorID: "proxy", TokenFile: file("proxy", shared)}}}}
	if _, err := proxyExecutorCredentials(cfg); err == nil {
		t.Fatal("generic runner token reused")
	}
	cfg.Runtime.ExecutionRunnerToken = ""
	cfg.Runtime.OutpostRuntimeExecutors = []config.OutpostRuntimeExecutorConfig{{ExecutorID: "outpost", TokenFile: file("outpost", shared)}}
	if _, err := proxyExecutorCredentials(cfg); err == nil {
		t.Fatal("Outpost executor token reused")
	}
	cfg.Runtime.ProxyRuntimeExecutors[0].TokenFile = file("dedicated", other)
	items, err := proxyExecutorCredentials(cfg)
	if err != nil || len(items) != 1 || items[0].ExecutorID != "proxy" {
		t.Fatalf("dedicated credential failed: %v", err)
	}
}
