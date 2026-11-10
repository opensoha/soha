package execution

import (
	"context"
	"errors"
	dd "github.com/opensoha/soha/internal/domain/delivery"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"reflect"
	"testing"
)

type referenceRetryValidator struct {
	err   error
	calls int
}

func (v *referenceRetryValidator) HydrateExecutionTask(_ context.Context, task dd.ExecutionTask) (dd.ExecutionTask, error) {
	return task, nil
}
func (v *referenceRetryValidator) ValidateExecutionTaskRetry(context.Context, dd.ExecutionTask) error {
	v.calls++
	return v.err
}

func TestRetryServiceReferenceValidationPrecedesTaskMutation(t *testing.T) {
	for _, scenario := range []string{"stale", "permission-revoked", "missing-validator", "valid"} {
		t.Run(scenario, func(t *testing.T) {
			repo := newExecutionRepoFake()
			original := dd.ExecutionTask{ID: "source", TaskKind: "manifest_preflight", Status: "failed", CallbackToken: "original-token", ProviderKind: dd.ManifestAdmissionProviderPrefix + "ci_agent_runner", ApplicationID: "app", ApplicationEnvironmentID: "source", Result: map[string]any{"previous": "retained"}}
			repo.tasks[original.ID] = original
			service := New(repo, nil, nil, nil, "", "", "", "", 0, "", nil)
			validator := &referenceRetryValidator{}
			switch scenario {
			case "stale":
				validator.err = apperrors.ErrConflict
			case "permission-revoked":
				validator.err = apperrors.ErrAccessDenied
			}
			if scenario != "missing-validator" {
				service.SetExecutionTaskHydrator(validator)
			}
			_, err := service.RetryExecutionTask(t.Context(), original.ID, dd.ExecutionTaskActionInput{})
			if scenario == "valid" {
				if err != nil || validator.calls != 1 || repo.tasks[original.ID].CallbackToken == original.CallbackToken || len(repo.updates) == 0 {
					t.Fatalf("valid retry blocked: %v", err)
				}
				return
			}
			if err == nil || !reflect.DeepEqual(repo.tasks[original.ID], original) || len(repo.logs) != 0 || len(repo.updates) != 0 {
				t.Fatalf("failed validation mutated retry: %v %+v", err, repo)
			}
			if scenario == "stale" && !errors.Is(err, apperrors.ErrConflict) {
				t.Fatal(err)
			}
		})
	}
}
