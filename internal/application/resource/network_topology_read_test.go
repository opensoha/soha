package resource

import (
	"context"
	"errors"
	"fmt"
	cr "github.com/opensoha/soha/contracts/resource"
	"testing"
	"time"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

type topologyReadAuthorizer struct {
	requests    []domainaccess.Request
	serviceErr  error
	denyService bool
	blockKind   string
}

func (a *topologyReadAuthorizer) Authorize(ctx context.Context, request domainaccess.Request) (domainaccess.Decision, error) {
	a.requests = append(a.requests, request)
	if request.Resource.Kind == a.blockKind {
		<-ctx.Done()
		return domainaccess.Decision{}, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return domainaccess.Decision{}, err
	}
	if request.Resource.Kind == "Service" {
		if a.serviceErr != nil {
			return domainaccess.Decision{}, a.serviceErr
		}
		if a.denyService {
			return domainaccess.Decision{Allowed: false, Reason: "service read denied"}, nil
		}
	}
	return domainaccess.Decision{Allowed: true}, nil
}
func topologyReadFixture(a *topologyReadAuthorizer, audit *declarativeAuditRecorder) (*Network, *topologyDirectFixture) {
	connection := domaincluster.Connection{Summary: domaincluster.Summary{ID: "c", ConnectionMode: domaincluster.ConnectionModeDirectKubeconfig}}
	direct := &topologyDirectFixture{}
	return &Network{resourceAccess: &resourceAccess{resolver: stubConnectionResolver{connection: connection}, authorizer: a, audit: audit}, topologyDirect: direct}, direct
}
func TestTopologyReadChecksOnlyTheRequestedReadPermissions(t *testing.T) {
	for _, namespace := range []string{"", "team"} {
		t.Run(fmt.Sprintf("namespace=%s", namespace), func(t *testing.T) {
			auth := &topologyReadAuthorizer{}
			network, _ := topologyReadFixture(auth, &declarativeAuditRecorder{})
			if _, err := network.GetNetworkTopology(context.Background(), domainidentity.Principal{}, "c", namespace); err != nil {
				t.Fatal(err)
			}
			seen := map[string]bool{}
			for _, request := range auth.requests {
				if request.Action != domainaccess.ActionView && request.Action != domainaccess.ActionList {
					t.Fatalf("topology queried unrelated action %s for %s", request.Action, request.Resource.Kind)
				}
				key := fmt.Sprintf("%s/%s/%s/wide=%t", request.Resource.Kind, request.Namespace.Namespace, request.Action, request.RequireClusterWideNamespace)
				if seen[key] {
					t.Fatalf("duplicate authorization for the same topology read: %s", key)
				}
				seen[key] = true
			}
		})
	}
}
func TestTopologyReadDoesNotTurnDependencyErrorsIntoPermissionDenials(t *testing.T) {
	for _, item := range []struct {
		name   string
		err    error
		reason string
	}{
		{"deadline", context.DeadlineExceeded, "timeout"},
		{"canceled", context.Canceled, "timeout"},
		{"dependency", errors.New("policy store unavailable"), "source_unavailable"},
	} {
		t.Run(item.name, func(t *testing.T) {
			auth := &topologyReadAuthorizer{serviceErr: item.err}
			audit := &declarativeAuditRecorder{}
			network, direct := topologyReadFixture(auth, audit)
			view, err := network.GetNetworkTopology(context.Background(), domainidentity.Principal{}, "c", "team")
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, source := range view.Coverage {
				if source.SourceID == "kubernetes.services" {
					found = true
					if source.State != "error" || source.ReasonCode != item.reason {
						t.Fatalf("dependency failure mislabeled: %#v", source)
					}
				}
			}
			if !found {
				t.Fatal("service coverage missing")
			}
			for _, query := range direct.queries {
				if query.SourceID == "kubernetes.services" {
					t.Fatal("read proceeded without an authorization decision")
				}
			}
			if len(audit.entries) != 0 {
				t.Fatalf("dependency error recorded as a permission denial: %#v", audit.entries)
			}
		})
	}
}
func TestTopologyReadKeepsDeniedSourcesOutOfSnapshotAndAuditsDenial(t *testing.T) {
	auth := &topologyReadAuthorizer{denyService: true}
	audit := &declarativeAuditRecorder{}
	network, direct := topologyReadFixture(auth, audit)
	view, err := network.GetNetworkTopology(context.Background(), domainidentity.Principal{}, "c", "team")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, source := range view.Coverage {
		if source.SourceID == "kubernetes.services" {
			found = true
			if source.State != "denied" || source.ReasonCode != "soha_permission" {
				t.Fatalf("denied coverage = %#v", source)
			}
		}
	}
	if !found {
		t.Fatal("denied source coverage missing")
	}
	for _, query := range direct.queries {
		if query.SourceID == "kubernetes.services" {
			t.Fatal("denied service queried")
		}
	}
	if len(audit.entries) != 1 || audit.entries[0].Result != "deny" {
		t.Fatalf("denial audit missing: %#v", audit.entries)
	}
}

type topologyBudgetDirect struct {
	contextErr error
	queries    []cr.NetworkTopologySourceQuery
}

func (d *topologyBudgetDirect) NetworkTopologySnapshot(ctx context.Context, clusterID string, input cr.NetworkTopologySnapshotRequest) (cr.NetworkTopologySnapshot, error) {
	d.contextErr = ctx.Err()
	d.queries = input.Queries
	if d.contextErr != nil {
		return cr.NetworkTopologySnapshot{}, d.contextErr
	}
	return topologySourceFixture(clusterID, input), nil
}
func TestTopologyReadReservesBudgetForAlreadyAuthorizedSources(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	auth := &topologyReadAuthorizer{blockKind: "HTTPRoute"}
	network, _ := topologyReadFixture(auth, &declarativeAuditRecorder{})
	direct := &topologyBudgetDirect{}
	network.topologyDirect = direct
	view, err := network.GetNetworkTopology(ctx, domainidentity.Principal{}, "c", "team")
	if err != nil {
		t.Fatal(err)
	}
	if direct.contextErr != nil {
		t.Fatalf("source planning starved the actual snapshot read: %v", direct.contextErr)
	}
	if len(direct.queries) == 0 || len(view.Nodes) == 0 {
		t.Fatal("already-authorized sources were discarded by a slow optional policy lookup")
	}
	available, timedOut := false, false
	for _, source := range view.Coverage {
		if source.SourceID == "kubernetes.services" && source.State == "available" {
			available = true
		}
		if source.SourceID == "gateway.httproutes" && source.State == "error" && source.ReasonCode == "timeout" {
			timedOut = true
		}
	}
	if !available || !timedOut {
		t.Fatalf("basic resources and optional failure must remain independent: available=%v timeout=%v", available, timedOut)
	}
}
