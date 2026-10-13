package virtualization

import (
	"errors"
	"testing"

	domain "github.com/opensoha/soha/internal/domain/virtualization"
)

func TestVMActionFailurePreservesPriorUnknownEffects(t *testing.T) {
	for _, test := range []struct {
		name, previous, expected string
		attempt                  int
		noWrite                  bool
	}{
		{"first preflight", "", "not_started", 1, true},
		{"unknown write", "", "unknown", 1, false},
		{"retry after unknown", "unknown", "unknown", 2, true},
		{"retry after proved no write", "not_started", "not_started", 2, true},
		{"write after proved no write", "not_started", "unknown", 2, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			task := domain.Task{TaskKind: TaskKindVMAction, AttemptCount: test.attempt,
				Payload: map[string]any{"providerDispatchStarted": true}, Result: map[string]any{"providerEffect": test.previous}}
			err := &domain.AdapterError{Cause: errors.New("failed"), NoProviderWrite: test.noWrite}
			applyVMActionFailure(&task, err)
			if task.Result["providerEffect"] != test.expected || boolValue(task.Payload, "providerDispatchStarted") != (test.expected == "unknown") {
				t.Fatalf("unsafe effect/reservation receipt: %#v %#v", task.Result, task.Payload)
			}
		})
	}
}
