package radiusdeployment

import (
	"errors"
	"math"
	"testing"

	api "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domain "github.com/opensoha/soha/internal/domain/radiusdeployment"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestReplicaConversionsRejectOverflow(t *testing.T) {
	state := domain.State{Slot: domain.Slot{MaxReplicas: 2}}
	for _, value := range []int{-1, math.MaxInt32 + 1} {
		if _, err := controlTarget(state, api.NetworkRADIUSControlInput{Action: "scale", Replicas: &value}); !errors.Is(err, apperrors.ErrInvalidArgument) {
			t.Fatalf("scale %d: %v", value, err)
		}
		state.Pool.DesiredReplicas = value
		if _, err := controlTarget(state, api.NetworkRADIUSControlInput{Action: "reconcile"}); !errors.Is(err, apperrors.ErrInvalidArgument) {
			t.Fatalf("reconcile %d: %v", value, err)
		}
		state.Pool.ObservedReplicas = &value
		if _, err := maxObserved(state); !errors.Is(err, apperrors.ErrConflict) {
			t.Fatalf("observed %d: %v", value, err)
		}
	}
	value := 2
	state.Pool.ObservedReplicas = &value
	if actual, err := maxObserved(state); err != nil || actual != 2 {
		t.Fatalf("bounded observed: %d %v", actual, err)
	}
}
