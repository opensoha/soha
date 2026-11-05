package sohaapi

import (
	"encoding/json"
	"testing"
)

func TestWorkloadAndCertificateSDKKeepExplicitZeroValues(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		destination any
	}{
		{"rollout", `{"matchesCurrentTemplate":false}`, new(KubernetesDeploymentRollout)},
		{"job", `{"completionTarget":0,"suspend":false}`, new(KubernetesJob)},
		{"job detail", `{"completionTarget":0,"suspend":false,"backoffLimit":0,"activeDeadlineSeconds":0,"ttlSecondsAfterFinished":0}`, new(KubernetesJobDetail)},
		{"certificate", `{"selfSigned":false}`, new(KubernetesSecretCertificate)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := json.Unmarshal([]byte(tc.input), tc.destination); err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(tc.destination)
			if err != nil {
				t.Fatal(err)
			}
			var before, after map[string]json.RawMessage
			if err := json.Unmarshal([]byte(tc.input), &before); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(encoded, &after); err != nil {
				t.Fatal(err)
			}
			for key, value := range before {
				if string(after[key]) != string(value) {
					t.Errorf("%s = %s, want %s", key, after[key], value)
				}
			}
		})
	}
}
