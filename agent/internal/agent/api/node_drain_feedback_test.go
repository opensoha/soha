package api

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	cr "github.com/opensoha/soha-contracts/resource"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestDrainErrorReturnsKnownConflictWithoutProviderText(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	writeError(c, &cr.NodeDrainError{Code: "node_drain_pdb_blocked", Cause: errors.New("private token")})
	if w.Code != 409 || !strings.Contains(w.Body.String(), "node_drain_pdb_blocked") || strings.Contains(w.Body.String(), "private") {
		t.Fatalf("unsafe drain error %d %s", w.Code, w.Body.String())
	}
}

func TestDrainCredentialDenialRemainsSafeForbidden(t *testing.T) {
	for _, err := range []error{
		apierrors.NewForbidden(schema.GroupResource{Resource: "pods/eviction"}, "pod", errors.New("private token")),
		apierrors.NewUnauthorized("private token"),
	} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		writeError(c, err)
		if w.Code != 403 || strings.Contains(w.Body.String(), "private") {
			t.Fatalf("unsafe permission error: %d %s", w.Code, w.Body.String())
		}
	}
}
