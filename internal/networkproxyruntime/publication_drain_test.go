package networkproxyruntime

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	api "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	"github.com/opensoha/soha/contracts/proxytransport"
	domain "github.com/opensoha/soha/internal/domain/networkproxy"
)

func TestOrdinaryDrainPreservesHandlersPastServeGrantWithoutExtendingOriginalLease(t *testing.T) {
	now := time.Now()
	oldServeExpiry := now.Add(250 * time.Millisecond)
	originalLease := now.Add(1200 * time.Millisecond)
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/network-control/v1/proxies/runtime/ingress/check" {
			t.Errorf("drain unexpectedly published readiness at %s", request.URL.Path)
		}
		var input api.NetworkProxyIngressCheckInput
		_ = json.NewDecoder(request.Body).Decode(&input)
		_ = json.NewEncoder(w).Encode(api.NetworkProxyIngressCheckResult{Allowed: true, BundleID: input.BundleID, NodeID: input.NodeID, Revision: input.Revision, CredentialEpoch: input.CredentialEpoch, ExpiresAt: time.Now().Add(5 * time.Second)})
	}))
	defer control.Close()
	client := NewClient(control.URL, "runtime", nil)
	handler, err := proxytransport.New(map[string]string{"user": "01234567890123456789012345678901"}, func(context.Context, string, string) (net.Conn, error) { return nil, errors.New("unused fixture dial") }, oldServeExpiry)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = handler.Close() }()
	p := &publicationIngress{accepting: true, until: oldServeExpiry, client: client, handlers: map[*proxytransport.Handler]struct{}{handler: {}}}
	p.active.Store(1)
	e := newEngine(Config{Engine: domain.EngineMihomo}, "")
	e.desired = domain.Configuration{Revision: 1, Enabled: true, ContentHash: "content", ValidUntil: originalLease, Publication: &api.NetworkProxyServerPublication{ValidUntil: originalLease}}
	r := &Runner{config: Config{RuntimeID: "runtime", InstanceID: "pod", ManagedDeployment: true}, hostBootEpoch: "boot", enabled: true, engine: e, ingress: p, validUntil: originalLease, appliedRevision: 1}
	grant := api.NetworkProxyDeploymentRuntimeControl{RuntimeID: "runtime", InstanceID: "pod", ProcessEpoch: "boot", CredentialID: "credential", CredentialEpoch: 1, OwnerEpoch: 1, ControlRevision: 2, IssuedAt: now, ExpiresAt: now.Add(2 * time.Second), AcceptingRequests: false}
	if err := r.acceptDeployment(grant, now); err != nil {
		t.Fatal(err)
	}
	if err := r.updatePublication(context.Background()); err != nil {
		t.Fatal(err)
	}
	if p.accepting || p.closed || !p.until.Equal(originalLease) {
		t.Fatal("drain closed active handlers or kept the old serving deadline")
	}
	input := api.NetworkProxyIngressCheckInput{BundleID: "bundle", NodeID: "node", Revision: 1, CredentialEpoch: 1}
	done := make(chan struct{})
	defer close(done)
	go p.recheck(done, handler, input, oldServeExpiry)
	time.Sleep(450 * time.Millisecond)
	if !handler.Ready() {
		t.Fatal("ordinary drain closed an authorized handler at the old serving deadline")
	}
	// Renewed configuration/control observations cannot widen the captured drain lease.
	r.validUntil = now.Add(time.Minute)
	grant.IssuedAt = time.Now()
	grant.ExpiresAt = now.Add(3 * time.Second)
	if err := r.acceptDeployment(grant, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := r.updatePublication(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !p.until.Equal(originalLease) {
		t.Fatal("poll extended the original drain lease")
	}
	r.validUntil = originalLease
	r.config.PollInterval = 10 * time.Second
	if delay := r.nextCycleDelay(time.Now()); delay > time.Until(originalLease)+10*time.Millisecond {
		t.Fatal("runner sleeps beyond the original configuration lease")
	}
	r.applyConfiguration(context.Background(), e.desired)
	if !r.enabled || r.suspended {
		t.Fatal("unchanged configuration polling stopped ordinary drain")
	}
	deadline := originalLease.Add(300 * time.Millisecond)
	for handler.Ready() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if handler.Ready() {
		t.Fatal("drain retained traffic after its original lease")
	}
}
