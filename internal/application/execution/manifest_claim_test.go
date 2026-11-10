package execution

import (
	"context"
	"errors"
	dd "github.com/opensoha/soha/internal/domain/delivery"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"testing"
)

type manifestAdmissionHydrator struct{ confirmed bool }

func (h manifestAdmissionHydrator) HydrateExecutionTask(_ context.Context, task dd.ExecutionTask) (dd.ExecutionTask, error) {
	task.ManifestAdmissionValidated = h.confirmed
	return task, nil
}

func TestExecutionClaimCannotSkipManifestAdmission(t *testing.T) {
	for _, mode := range []string{"missing", "unconfirmed", "confirmed", "legacy"} {
		t.Run(mode, func(t *testing.T) {
			s := &Service{}
			task := dd.ExecutionTask{ProviderKind: dd.ManifestAdmissionProviderPrefix + "manifest_direct", ManifestAdmissionValidated: true}
			if mode == "legacy" {
				task.ProviderKind = "manifest_direct"
			}
			if mode == "unconfirmed" || mode == "confirmed" {
				s.payloadHydrator = manifestAdmissionHydrator{confirmed: mode == "confirmed"}
			}
			result, err := s.hydrateExecutionClaim(t.Context(), task)
			valid := mode == "confirmed" || mode == "legacy"
			if valid && err != nil || !valid && !errors.Is(err, apperrors.ErrUnsupportedOperation) {
				t.Fatalf("mode=%s err=%v", mode, err)
			}
			if result.ManifestAdmissionValidated != (mode == "confirmed") {
				t.Fatal("stale confirmation survived claim")
			}
		})
	}
}
