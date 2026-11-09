package proxydeployment

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/opensoha/soha/internal/domain/identity"
)

func TestViewReplicaJSONPresence(t *testing.T) {
	zero, two := int32(0), int32(2)
	for _, tc := range []struct {
		name                             string
		desired                          int32
		observed                         *int32
		phase, publicState, wantObserved string
	}{
		{"managed stopped", 0, &zero, "converged", "stopped", "0"},
		{"not yet observed", 0, nil, "unobserved", "pending", ""},
		{"positive replicas", 2, &two, "converged", "ready", "2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			slot := testSlot()
			service, repo := newPinService(t, slot, slot)
			repo.state.DesiredReplicas, repo.state.ActualReplicas, repo.state.Phase = tc.desired, tc.observed, tc.phase
			view, err := service.View(context.Background(), identity.Principal{UserID: "op"}, "master")
			if err != nil {
				t.Fatal(err)
			}
			if string(view.State) != tc.publicState || view.ObjectUID != slot.UID || view.ObservedGeneration != repo.state.ObservedGeneration {
				t.Fatalf("view metadata changed: %+v", view)
			}
			raw, err := json.Marshal(view)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(raw, &fields); err != nil {
				t.Fatal(err)
			}
			wantDesired, _ := json.Marshal(tc.desired)
			if string(fields["desiredReplicas"]) != string(wantDesired) {
				t.Fatalf("desired count lost: %s", raw)
			}
			observed, present := fields["observedReplicas"]
			if present != (tc.observed != nil) || string(observed) != tc.wantObserved {
				t.Fatalf("observed presence lost: %s", raw)
			}
			if repo.commits != 0 || repo.grants != 0 {
				t.Fatal("view mutated deployment")
			}
		})
	}
}
