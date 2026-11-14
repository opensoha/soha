package api

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	shared "github.com/opensoha/soha/contracts/resource/runtime"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestResourceGraphErrorResponse(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
	}{
		{"invalid", shared.ErrInvalidResourceGraphRoot, 400},
		{"unsupported", fmt.Errorf("wrapped: %w", shared.ErrUnsupportedResourceGraphRoot), 501},
		{"missing", apierrors.NewNotFound(schema.GroupResource{Resource: "pods"}, "absent"), 404},
		{"forbidden", apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "api", errors.New("private-provider-detail")), 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := gin.New()
			router.GET("/graph", func(c *gin.Context) { writeResourceGraphError(c, tc.err) })
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/graph", nil))
			if w.Code != tc.status || strings.Contains(w.Body.String(), "private-provider-detail") {
				t.Fatalf("response=%d %s", w.Code, w.Body.String())
			}
		})
	}
}
