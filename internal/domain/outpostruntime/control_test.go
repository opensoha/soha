package outpostruntime

import (
	"testing"
	"time"
)

func TestControlGateFollowsManagedLifecycleIncludingStoppedTerminal(t *testing.T) {
	actual := int32(2)
	for _, test := range []struct {
		name  string
		state State
		drain bool
	}{
		{"external stays compatible", State{Owner: "external", DesiredReplicas: 0, LatestTaskID: "task"}, false},
		{"embedded unmanaged", State{Owner: "external"}, false},
		{"stop pending", State{Owner: "soha", DesiredReplicas: 0, LatestTaskID: "task", Phase: "queued"}, true},
		{"stopped terminal stays closed", State{Owner: "soha", DesiredReplicas: 0, LatestTaskID: "task", Phase: "converged"}, true},
		{"shrinking drains", State{Owner: "soha", DesiredReplicas: 1, ActualReplicas: &actual, LatestTaskID: "task", Phase: "reconciling"}, true},
		{"remaining replicas resume", State{Owner: "soha", DesiredReplicas: 1, ActualReplicas: &actual, LatestTaskID: "task", Phase: "resuming"}, false},
		{"start serves", State{Owner: "soha", DesiredReplicas: 2, LatestTaskID: "task", Phase: "queued"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			now := time.Now().UTC()
			control := ControlForState(test.state, now)
			if (control.Action == "drain") != test.drain {
				t.Fatalf("action=%s", control.Action)
			}
			if control.IssuedAt != now || control.ExpiresAt.Sub(now) != 30*time.Second {
				t.Fatal("control time not bounded")
			}
		})
	}
}
