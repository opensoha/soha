package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestHelmValuesUpdateBindsExpectedRevision(t *testing.T) {
	for _, tc := range []struct {
		name, body              string
		status, calls, revision int
		err                     error
	}{
		{name: "legacy", body: `{"content":"{}"}`, status: 200, calls: 1},
		{name: "current", body: `{"content":"{}","expectedRevision":3}`, status: 200, calls: 1, revision: 3},
		{name: "stale", body: `{"content":"{}","expectedRevision":3}`, status: 409, calls: 1, revision: 3, err: apperrors.ErrConflict},
		{name: "null", body: `{"content":"{}","expectedRevision":null}`, status: 400},
		{name: "zero", body: `{"content":"{}","expectedRevision":0}`, status: 400},
		{name: "negative", body: `{"content":"{}","expectedRevision":-3}`, status: 400},
		{name: "string", body: `{"content":"{}","expectedRevision":"3"}`, status: 400},
		{name: "decimal", body: `{"content":"{}","expectedRevision":3.5}`, status: 400},
		{name: "overflow", body: `{"content":"{}","expectedRevision":2147483647}`, status: 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			editor := &helmValuesEditorSpy{err: tc.err}
			router := gin.New()
			router.PUT("/clusters/:clusterID/helm/releases/:releaseName/values", (&helmReleaseResourceHandler{editor: editor}).UpdateHelmReleaseValues)
			req := httptest.NewRequest(http.MethodPut, "/clusters/cluster-a/helm/releases/app/values?namespace=test", strings.NewReader(tc.body))
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != tc.status || editor.calls != tc.calls || editor.revision != tc.revision {
				t.Fatalf("status=%d, calls=%d, revision=%d, response=%s", rec.Code, editor.calls, editor.revision, rec.Body)
			}
		})
	}
}

type helmValuesEditorSpy struct {
	HelmReleaseEditor
	calls, revision int
	err             error
}

func (e *helmValuesEditorSpy) UpdateHelmReleaseValuesWithRevision(_ context.Context, _ domainidentity.Principal, _, _, _, _ string, expectedRevision int) (domainresource.HelmValuesView, error) {
	e.calls++
	e.revision = expectedRevision
	return domainresource.HelmValuesView{}, e.err
}
