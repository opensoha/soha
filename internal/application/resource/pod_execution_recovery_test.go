package resource

import (
	"errors"
	"testing"

	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestPodOriginalRecoveryChecksFrozenInputWithoutExecuting(t *testing.T) {
	service, _, direct, _ := podExecutionTestService()
	input := podExecutionTestInput()
	if _, err := service.FindOriginalPodExecution(t.Context(), podExecutionTestPrincipal, "cluster-a", "team-a", "app", "recovery-original", input); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatal("missing recovery did not remain absent", err)
	}
	first, err := service.ExecutePodArgv(t.Context(), podExecutionTestPrincipal, domainidentity.AccessContext{}, "cluster-a", "team-a", "app", "recovery-original", input)
	if err != nil {
		t.Fatal(err)
	}
	before := direct.calls.Load()
	recovered, err := service.FindOriginalPodExecution(t.Context(), podExecutionTestPrincipal, "cluster-a", "team-a", "app", "recovery-original", input)
	if err != nil || recovered.ID != first.ID || direct.calls.Load() != before {
		t.Fatal("recovery re-executed or changed identity", err)
	}
	input.Argv = []string{"different-input"}
	if _, err := service.FindOriginalPodExecution(t.Context(), podExecutionTestPrincipal, "cluster-a", "team-a", "app", "recovery-original", input); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("recovery substituted another original input", err)
	}
	if direct.calls.Load() != before {
		t.Fatal("conflicting recovery executed a process")
	}
}
