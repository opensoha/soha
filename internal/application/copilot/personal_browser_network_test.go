package copilot

import (
	"context"
	"errors"
	"testing"
	"time"

	env "github.com/opensoha/soha-contracts/agent/environment"
	guest "github.com/opensoha/soha-contracts/agent/guest"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type browserFetchFunc func(context.Context, guest.BrowserNetworkRequest) (guest.BrowserNetworkResponse, error)

func (f browserFetchFunc) Fetch(ctx context.Context, r guest.BrowserNetworkRequest) (guest.BrowserNetworkResponse, error) {
	return f(ctx, r)
}
func TestPersonalBrowserNetworkAuthority(t *testing.T) {
	s, store, identity, request, authorization := personalGuestFixture(t)
	if _, err := s.Enroll(context.Background(), identity, authorization, request); err != nil {
		t.Fatal(err)
	}
	binding := store.item.Binding
	op := guest.RuntimeOperation{SchemaVersion: guest.RuntimeOperationVersion, OperationID: binding.ID, Binding: binding, Kind: "browser", Source: "native", Deadline: s.now().Add(30 * time.Second), Request: env.Request{SchemaVersion: env.RequestVersion, Action: "inspect", Lease: env.Lease{SchemaVersion: env.LeaseVersion, ID: binding.ID, RunID: binding.ID, OwnerID: binding.AssistantID, ScopeHash: binding.PolicyRevision, Mode: "restricted-write", IssuedAt: binding.IssuedAt, ExpiresAt: binding.ExpiresAt, Quota: env.Quota{DiskBytes: 16 << 20, MemoryBytes: 2 << 30, CPUUnits: 1000, MaxProcesses: 64}}}, Browser: &guest.BrowserAction{Action: "click", ControlID: binding.ID, Generation: 1, Owner: "agent", TaskID: binding.ID, Selector: "button"}}
	op.ArtifactSHA256 = binding.ProviderIdentityHash[len("sha256:"):]
	s.ConfigureRuntime(func(context.Context, guest.Binding, string) (guest.RuntimeOperation, error) { return op, nil })
	calls := 0
	s.ConfigureBrowserNetwork(browserFetchFunc(func(context.Context, guest.BrowserNetworkRequest) (guest.BrowserNetworkResponse, error) {
		calls++
		return guest.BrowserNetworkResponse{Status: 200}, nil
	}))
	input := guest.BrowserNetworkRequest{URL: "https://example.com/", Method: "POST"}
	if _, err := s.BrowserNetwork(context.Background(), identity, binding.ID, input); !errors.Is(err, apperrors.ErrAccessDenied) || calls != 0 {
		t.Fatal("native write escaped approval boundary")
	}
	input.Method = "GET"
	if _, err := s.BrowserNetwork(context.Background(), identity, binding.ID, input); err != nil || calls != 1 {
		t.Fatal("authorized read refused", err)
	}
	s.ConfigureBrowserNetwork(browserFetchFunc(func(context.Context, guest.BrowserNetworkRequest) (guest.BrowserNetworkResponse, error) {
		store.current = false
		return guest.BrowserNetworkResponse{Status: 200}, nil
	}))
	if _, err := s.BrowserNetwork(context.Background(), identity, binding.ID, input); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatal("late response survived revocation", err)
	}
}
