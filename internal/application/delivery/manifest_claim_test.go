package delivery

import (
	"encoding/json"
	dd "github.com/opensoha/soha/internal/domain/delivery"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"strings"
	"testing"
)

func TestProtectedManifestClaimRequiresValidatorAndEphemeralConfirmation(t *testing.T) {
	for _, mode := range []string{"valid", "missing-validator", "revoked", "wrong-kind", "forged-payload"} {
		t.Run(mode, func(t *testing.T) {
			validator := &rootClaimValidator{}
			task := dd.ExecutionTask{TaskKind: "manifest_preflight", ProviderKind: dd.ManifestAdmissionProviderPrefix + "manifest_direct", ManifestAdmissionValidated: true}
			service := &Service{manifestDelivery: validator}
			switch mode {
			case "missing-validator":
				service.manifestDelivery = nil
			case "revoked":
				validator.err = apperrors.ErrAccessDenied
			case "wrong-kind":
				task.TaskKind = "release"
			case "forged-payload":
				service.manifestDelivery = nil
				task.Payload = map[string]any{"manifestAdmissionValidated": true}
			}
			result, err := service.HydrateExecutionTask(t.Context(), task)
			valid := mode == "valid"
			if (err == nil) != valid || result.ManifestAdmissionValidated != valid {
				t.Fatalf("mode=%s admitted=%v err=%v", mode, result.ManifestAdmissionValidated, err)
			}
			data, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			if valid && strings.Contains(string(data), "manifestAdmissionValidated") {
				t.Fatal("ephemeral admission leaked into task JSON")
			}
		})
	}
}
