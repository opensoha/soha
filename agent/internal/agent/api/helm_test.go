package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	domainresource "github.com/opensoha/soha-agent/internal/domain/resource"
	helmruntime "github.com/opensoha/soha-contracts/helmrelease/runtime"
)

func TestHelmErrorsPreserveConflictWithoutProviderDetails(t *testing.T) {
	for _, test := range []struct {
		err    error
		status int
	}{
		{fmt.Errorf("%w: secret provider details", helmruntime.ErrConflict), http.StatusConflict},
		{fmt.Errorf("secret provider details"), http.StatusBadGateway},
	} {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		writeHelmError(ctx, test.err)
		if recorder.Code != test.status || strings.Contains(recorder.Body.String(), "secret provider details") {
			t.Fatalf("response = %d, %s", recorder.Code, recorder.Body.String())
		}
	}
}

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
		{name: "timeout below range", namespace: "platform", release: "gateway", input: domainresource.HelmReleaseRollbackInput{Revision: 2, TimeoutSeconds: -1}, wantErr: true},
		{name: "timeout above range", namespace: "platform", release: "gateway", input: domainresource.HelmReleaseRollbackInput{Revision: 2, TimeoutSeconds: 3601}, wantErr: true},
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
