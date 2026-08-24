package resource

import (
	"testing"

	domainresource "github.com/opensoha/soha/internal/domain/resource"
)

func TestNormalizeHelmRollbackRequest(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		release   string
		input     domainresource.HelmReleaseRollbackInput
		wantErr   bool
	}{
		{name: "valid", namespace: " platform ", release: " gateway ", input: domainresource.HelmReleaseRollbackInput{Revision: 2}},
		{name: "missing namespace", release: "gateway", input: domainresource.HelmReleaseRollbackInput{Revision: 2}, wantErr: true},
		{name: "missing release", namespace: "platform", input: domainresource.HelmReleaseRollbackInput{Revision: 2}, wantErr: true},
		{name: "timeout too large", namespace: "platform", release: "gateway", input: domainresource.HelmReleaseRollbackInput{Revision: 2, TimeoutSeconds: 3601}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			namespace, release, input, err := normalizeHelmRollbackRequest(test.namespace, test.release, test.input)
			if (err != nil) != test.wantErr {
				t.Fatalf("normalizeHelmRollbackRequest() error = %v, wantErr %v", err, test.wantErr)
			}
			if err == nil && (namespace != "platform" || release != "gateway" || input.TimeoutSeconds != 300) {
				t.Fatalf("normalizeHelmRollbackRequest() = %q, %q, %#v", namespace, release, input)
			}
		})
	}
}
