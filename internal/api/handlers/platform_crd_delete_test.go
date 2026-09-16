package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

type observedCRDEditor struct {
	CRDEditor
	uid string
}

func (e *observedCRDEditor) DeleteCRDResource(_ context.Context, _ domainidentity.Principal, cluster, crd, namespace, name, uid string) error {
	e.uid = strings.Join([]string{cluster, crd, namespace, name, uid}, "/")
	return nil
}

func TestCRDDeletionPassesObservedUID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	editor := &observedCRDEditor{}
	handler := &crdResourceHandler{editor: editor}
	router := gin.New()
	router.DELETE("/clusters/:clusterID/extensions/crds/:crdName/resources/:name", handler.DeleteCRDResource)
	path := "/clusters/demo/extensions/crds/workloadcronjobs.workloads.soha.io/resources/task?namespace=apps&expectedUid="
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodDelete, path+"root-uid", nil))
	if response.Code != http.StatusNoContent || editor.uid != "demo/workloadcronjobs.workloads.soha.io/apps/task/root-uid" {
		t.Fatalf("response %d, target %s", response.Code, editor.uid)
	}
	editor.uid = ""
	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodDelete, path+strings.Repeat("a", 129), nil))
	if response.Code != http.StatusBadRequest || editor.uid != "" {
		t.Fatal("invalid identity reached deletion")
	}
}
