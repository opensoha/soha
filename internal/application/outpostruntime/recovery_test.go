package outpostruntime

import (
	"errors"
	"testing"

	domain "github.com/opensoha/soha/internal/domain/outpostruntime"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestReconcileRejectsWriteParameters(t *testing.T) {
	for _, input := range []domain.ControlInput{{Action: "reconcile", Replicas: ptr(int32(0))}, {Action: "reconcile", Replicas: ptr(int32(2))}, {Action: "reconcile", DrainTimeoutSeconds: 30}} {
		if _, _, err := controlTarget(input, 3); !errors.Is(err, apperrors.ErrInvalidArgument) {
			t.Fatalf("write parameters accepted: %+v %v", input, err)
		}
	}
	if _, _, err := controlTarget(domain.ControlInput{Action: "reconcile"}, 3); err != nil {
		t.Fatal(err)
	}
}
