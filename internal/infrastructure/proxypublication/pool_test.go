package proxypublication

import (
	"context"
	"errors"
	proxy "github.com/opensoha/soha/internal/domain/networkproxy"
	deployment "github.com/opensoha/soha/internal/domain/proxydeployment"
	domain "github.com/opensoha/soha/internal/domain/proxypublication"
	"github.com/opensoha/soha/internal/infrastructure/config"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"testing"
	"time"
)

type controlPoolFixture struct{ pool deployment.ApprovedPool }

func (s controlPoolFixture) ApprovedPool(context.Context, string) (deployment.ApprovedPool, error) {
	return s.pool, nil
}

type controlInstanceFixture struct{ lookups []string }

func (s *controlInstanceFixture) Get(_ context.Context, id string) (proxy.Instance, error) {
	s.lookups = append(s.lookups, id)
	switch id {
	case "runtime-0":
		return proxy.Instance{ID: id, DesiredRevision: 3}, nil
	case "runtime-1":
		return proxy.Instance{ID: id, DesiredRevision: 4}, nil
	default:
		return proxy.Instance{}, apperrors.ErrNotFound
	}
}
func TestProxyPoolUsesRuntimeIdentityAndInstalledEndpoint(t *testing.T) {
	members := []deployment.Member{{InstanceID: "pod-0", RuntimeID: "runtime-0", Ordinal: 0}, {InstanceID: "pod-1", RuntimeID: "runtime-1", Ordinal: 1}}
	observed := int32(1)
	servingUntil := time.Now().UTC().Add(time.Minute)
	pool := deployment.ApprovedPool{Owner: "soha-managed", Phase: "converged", ObservedReplicas: &observed, ServingUntil: &servingUntil, MasterID: "master", UID: "uid", OwnerEpoch: 2, Members: members, MaxReplicas: 2, DesiredReplicas: 1, MemberSetHash: (deployment.Slot{Members: members}).MemberSetHash()}
	slot := config.ProxyRuntimeSlotConfig{ProxyID: "master", UID: "uid", Members: []config.ProxyRuntimeMemberConfig{{InstanceID: "pod-0", RuntimeID: "runtime-0", Ordinal: 0}, {InstanceID: "pod-1", RuntimeID: "runtime-1", Ordinal: 1}}, Endpoints: []config.ProxyEmployeeEndpointConfig{{EndpointID: "private", URL: "https://proxy.example"}}}
	for _, name := range []string{"valid", "missing policy", "ambiguous policy", "altered members", "unmanaged"} {
		t.Run(name, func(t *testing.T) {
			p := pool
			p.Members = append([]deployment.Member(nil), members...)
			slots := []config.ProxyRuntimeSlotConfig{slot}
			switch name {
			case "missing policy":
				slots = nil
			case "ambiguous policy":
				slots = append(slots, slot)
			case "altered members":
				p.Members[0].RuntimeID = "foreign"
			case "unmanaged":
				p.UID = ""
			}
			instances := &controlInstanceFixture{}
			adapter := Pool{deployment: controlPoolFixture{p}, instances: instances, slots: slots}
			got, err := adapter.ApprovedPool(context.Background(), "master")
			if name == "valid" {
				assertInstalledPool(t, got, err, servingUntil)
			} else if name == "unmanaged" {
				if err != nil || len(got.Endpoints) != 0 || got.Members[0].Running {
					t.Fatalf("unmanaged pool=%+v err=%v", got, err)
				}
			} else if !errors.Is(err, apperrors.ErrServiceUnavailable) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestPoolConstructionRejectsMissingDependencies(t *testing.T) {
	var typedNil *controlInstanceFixture
	if _, err := NewPool(controlPoolFixture{}, typedNil, nil); !errors.Is(err, apperrors.ErrServiceUnavailable) {
		t.Fatal("typed nil instance reader accepted")
	}
	if _, err := NewPool(nil, &controlInstanceFixture{}, nil); !errors.Is(err, apperrors.ErrServiceUnavailable) {
		t.Fatal("nil pool reader accepted")
	}
}

func TestProxyPoolSeparatesTargetReplicasFromDrainingMembers(t *testing.T) {
	actualTwo, actualOne, actualZero := int32(2), int32(1), int32(0)
	cases := []struct {
		name                     string
		desired                  int32
		actual                   *int32
		drain                    bool
		phase, owner             string
		wantRunning, wantServing [2]bool
	}{
		{"shrink drains both", 1, &actualTwo, true, "reconciling", "soha-managed", [2]bool{true, false}, [2]bool{true, true}},
		{"stop drains both", 0, &actualTwo, true, "queued", "soha-managed", [2]bool{false, false}, [2]bool{true, true}},
		{"unknown outcome", 1, &actualTwo, true, "unknown", "soha-managed", [2]bool{}, [2]bool{}},
		{"external owner", 1, &actualTwo, true, "reconciling", "external", [2]bool{}, [2]bool{}},
		{"unknown observed count", 1, nil, true, "reconciling", "soha-managed", [2]bool{true, false}, [2]bool{}},
		{"observed stopped", 0, &actualZero, true, "converged", "soha-managed", [2]bool{}, [2]bool{}},
		{"ordinary serve", 2, &actualTwo, false, "converged", "soha-managed", [2]bool{true, true}, [2]bool{true, true}},
		{"ordinary startup", 2, &actualOne, false, "resuming", "soha-managed", [2]bool{true, true}, [2]bool{true, false}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			deadline := time.Now().UTC().Add(5 * time.Second)
			members := []deployment.Member{{InstanceID: "pod-0", RuntimeID: "runtime-0", Ordinal: 0}, {InstanceID: "pod-1", RuntimeID: "runtime-1", Ordinal: 1}}
			pool := deployment.ApprovedPool{MasterID: "master", UID: "uid", Owner: tt.owner, Phase: tt.phase, MaxReplicas: 2, DesiredReplicas: tt.desired, ObservedReplicas: tt.actual, DrainHeld: tt.drain, ServingUntil: &deadline, Members: members, MemberSetHash: (deployment.Slot{Members: members}).MemberSetHash()}
			slot := config.ProxyRuntimeSlotConfig{ProxyID: "master", UID: "uid", Members: []config.ProxyRuntimeMemberConfig{{InstanceID: "pod-0", RuntimeID: "runtime-0", Ordinal: 0}, {InstanceID: "pod-1", RuntimeID: "runtime-1", Ordinal: 1}}, Endpoints: []config.ProxyEmployeeEndpointConfig{{EndpointID: "private", URL: "https://proxy.example"}}}
			adapter := Pool{deployment: controlPoolFixture{pool}, instances: &controlInstanceFixture{}, slots: []config.ProxyRuntimeSlotConfig{slot}}
			mapped, err := adapter.ApprovedPool(context.Background(), "master")
			if err != nil {
				t.Fatal(err)
			}
			for ordinal := int32(0); ordinal < 2; ordinal++ {
				running, serving := memberAvailability(pool, ordinal)
				if running != tt.wantRunning[ordinal] || serving != tt.wantServing[ordinal] {
					t.Fatalf("member %d running=%v serving=%v", ordinal, running, serving)
				}
				member := mapped.Members[ordinal]
				if member.Running != running || member.Serving != serving || running && member.ServingUntil != nil || !running && (member.ServingUntil == nil || !member.ServingUntil.Equal(deadline)) {
					t.Fatalf("mapped member %d: %+v", ordinal, member)
				}
			}
		})
	}
}

func assertInstalledPool(t *testing.T, got domain.ApprovedPool, err error, servingUntil time.Time) {
	t.Helper()
	if err != nil || len(got.Members) != 2 || !got.Members[0].Running || got.Members[1].Running || got.Members[0].ExpectedRevision != 3 || got.Members[1].ExpectedRevision != 4 || len(got.Endpoints) != 1 || got.Endpoints[0].EndpointID != "private" || got.Members[0].ServingUntil != nil || got.Members[1].ServingUntil == nil || !got.Members[1].ServingUntil.Equal(servingUntil) {
		t.Fatalf("pool=%+v error=%v", got, err)
	}
}
