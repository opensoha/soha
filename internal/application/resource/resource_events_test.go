package resource

import (
	"context"
	"errors"
	"testing"
	"time"

	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestSubscribeResourceEventsFiltersAuthorizedNamespaces(t *testing.T) {
	source := make(chan domainresource.ResourceStreamEvent, 2)
	direct := &resourceEventStreamStub{events: source}
	service := New(Dependencies{
		Connections: stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{
			ID: "direct-cluster", ConnectionMode: domaincluster.ConnectionModeDirectKubeconfig,
		}}},
		Authorizer: scopedResourceAuthorizer{}, Audit: discardAuditRecorder{}, DirectResourceEvents: direct,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, unsubscribe, err := service.GenericResources().SubscribeResourceEvents(ctx, domainidentity.Principal{UserID: "user-1"}, "direct-cluster", "", []string{"Pod"})
	if err != nil {
		t.Fatalf("SubscribeResourceEvents() error = %v", err)
	}
	defer unsubscribe()
	source <- resourceEvent("team-b", "hidden")
	source <- resourceEvent("team-a", "visible")

	select {
	case event := <-events:
		if event.Resource == nil || event.Resource.Name != "visible" {
			t.Fatalf("event = %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for authorized resource event")
	}
}

func TestSubscribeResourceEventsRejectsAgentWithoutRuntimeClient(t *testing.T) {
	service := New(Dependencies{
		Connections: stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{
			ID: "agent-cluster", ConnectionMode: domaincluster.ConnectionModeAgent,
		}}},
		Authorizer: scopedResourceAuthorizer{}, Audit: discardAuditRecorder{},
	})
	_, _, err := service.GenericResources().SubscribeResourceEvents(context.Background(), domainidentity.Principal{UserID: "user-1"}, "agent-cluster", "team-a", []string{"Pod"})
	if err == nil || errors.Is(err, apperrors.ErrUnsupportedOperation) {
		t.Fatalf("SubscribeResourceEvents() error = %v, want explicit unavailable runtime error", err)
	}
}

func resourceEvent(namespace, name string) domainresource.ResourceStreamEvent {
	return domainresource.ResourceStreamEvent{
		Type: "modified", ClusterID: "direct-cluster", ObservedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Resource: &domainresource.ResourceRef{
			ClusterID: "direct-cluster", APIVersion: "v1", Kind: "Pod", Name: name,
			Namespace: namespace, ScopeMode: domainresource.ResourceScopeModeNamespace,
		},
	}
}

type resourceEventStreamStub struct {
	events <-chan domainresource.ResourceStreamEvent
}

func (s *resourceEventStreamStub) SubscribeResourceEvents(string, string, []string) (<-chan domainresource.ResourceStreamEvent, func(), error) {
	return s.events, func() {}, nil
}
