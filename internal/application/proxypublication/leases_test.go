package proxypublication

import (
	"context"
	"errors"
	"testing"
	"time"

	api "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domain "github.com/opensoha/soha/internal/domain/proxypublication"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestEmployeeServingLeasePrefersVerifiedRunningMembers(t *testing.T) {
	for _, name := range []string{"shrink", "stop all", "invalid running readback", "all invalid", "configuration cap", "outer cap", "protection floor", "requested drainer", "expired drainer", "requested running", "multiple running", "running not serving"} {
		t.Run(name, func(t *testing.T) {
			b, f, _ := ingressFixture()
			now := b.now()
			drainUntil := now.Add(5 * time.Second)
			activeUntil := now.Add(2 * time.Minute)
			r := f.record
			r.Pool.Members = []domain.PoolMember{{RuntimeID: "proxy-1", Serving: true, Running: true, ServingUntil: &drainUntil}, {RuntimeID: "proxy-2", Serving: true, ServingUntil: &drainUntil}}
			r.Readbacks["proxy-2"] = api.NetworkProxyBundleReadback{ProcessEpoch: "server-epoch", Revision: 1}
			states := fixtureValue[desiredFake](t, b.desired)
			states.states[0].ConfigurationValidUntil = &activeUntil
			second := states.states[0]
			second.InstanceID = "proxy-2"
			states.states = append(states.states, second)
			requested, floor, outer, want, denied := "", 1, now.Add(5*time.Minute), activeUntil, false
			switch name {
			case "stop all":
				r.Pool.Members[0].Running = false
				want = drainUntil
			case "invalid running readback":
				states.states[0].AppliedHash = "drifted"
				want = drainUntil
			case "all invalid":
				states.states[0].AppliedHash, states.states[1].AppliedHash = "drifted", "drifted"
				denied = true
			case "configuration cap":
				activeUntil = now.Add(3 * time.Second)
				want = activeUntil
			case "outer cap":
				outer = now.Add(time.Second)
				want = outer
			case "protection floor":
				floor = 2
				denied = true
			case "requested drainer":
				requested, want = "proxy-2", drainUntil
			case "expired drainer":
				requested, denied = "proxy-2", true
				drainUntil = now
			case "requested running":
				// Explicit-member ingress keeps every existing bound, even if the
				// caller supplies an anomalous running member with a drain cap.
				requested, want = "proxy-1", drainUntil
			case "multiple running":
				r.Pool.Members[1].Running = true
				shorter := now.Add(time.Minute)
				states.states[1].ConfigurationValidUntil = &shorter
				want = shorter
			case "running not serving":
				r.Pool.Members[0].Serving = false
				want = drainUntil
			}
			b.desired = states
			got, err := b.servingLease(context.Background(), r, requested, outer, floor)
			if denied {
				if !errors.Is(err, apperrors.ErrAccessDenied) {
					t.Fatalf("lease=%v error=%v, want denied", got, err)
				}
				return
			}
			if err != nil || !got.Equal(want) {
				t.Fatalf("lease=%v error=%v, want %v", got, err, want)
			}
		})
	}
}
