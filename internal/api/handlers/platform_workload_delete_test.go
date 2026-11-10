package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type workloadDeleteSpy struct {
	PodResourceEditor
	GenericResourceService
	calls     int
	kind, uid string
	err       error
}

func (s *workloadDeleteSpy) DeletePodWithExpectedUID(_ context.Context, _ domainidentity.Principal, _, _, _, uid string) error {
	s.calls++
	s.kind, s.uid = "Pod", uid
	return s.err
}

func (s *workloadDeleteSpy) DeleteDeploymentWithExpectedUID(_ context.Context, _ domainidentity.Principal, _, _, _, uid string) error {
	s.calls++
	s.kind, s.uid = "Deployment", uid
	return s.err
}

func (s *workloadDeleteSpy) DeleteResourceByKind(_ context.Context, _ domainidentity.Principal, _, _, kind, _ string) error {
	s.calls++
	s.kind, s.uid = kind, ""
	return s.err
}

func TestWorkloadDeleteExpectedUIDQuery(t *testing.T) {
	for _, kind := range []string{"Pod", "Deployment"} {
		for _, tc := range []struct {
			name, query, uid string
			err              error
			status, calls    int
		}{
			{name: "legacy", status: 204, calls: 1},
			{name: "guarded", query: "&expectedUid=observed-uid", uid: "observed-uid", status: 204, calls: 1},
			{name: "replacement", query: "&expectedUid=old-uid", uid: "old-uid", err: apperrors.ErrConflict, status: 409, calls: 1},
			{name: "old-agent", query: "&expectedUid=observed-uid", uid: "observed-uid", err: apperrors.ErrUnsupportedOperation, status: 501, calls: 1},
			{name: "empty", query: "&expectedUid=", status: 400},
			{name: "malformed-escape", query: "&expectedUid=%z", status: 400},
			{name: "whitespace", query: "&expectedUid=uid%20", status: 400},
			{name: "duplicate", query: "&expectedUid=old&expectedUid=new", status: 400},
			{name: "too-long", query: "&expectedUid=" + strings.Repeat("a", 129), status: 400},
		} {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				router := gin.New()
				spy := &workloadDeleteSpy{err: tc.err}
				router.DELETE("/clusters/:clusterID/workloads/pods/:podName", (&podResourceHandler{editor: spy}).DeletePod)
				(&workloadInventoryResourceHandler{generic: spy}).RegisterWorkloadDeleteRoutes(router)
				path := "/clusters/prod/workloads/" + strings.ToLower(kind) + "s/demo?namespace=team" + tc.query
				response := httptest.NewRecorder()
				router.ServeHTTP(response, httptest.NewRequest(http.MethodDelete, path, nil))
				if response.Code != tc.status || spy.calls != tc.calls {
					t.Fatalf("status=%d, calls=%d, body=%q", response.Code, spy.calls, response.Body.String())
				}
				if tc.calls == 1 && (spy.uid != tc.uid || spy.kind != kind) {
					t.Fatalf("routed kind=%q uid=%q", spy.kind, spy.uid)
				}
			})
		}
	}
}
