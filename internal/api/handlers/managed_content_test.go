package handlers

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	d "github.com/opensoha/soha/internal/domain/contenttransfer"
)

func TestManagedContentBoundaryRejectsSelfReportedInspection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, raw := range []string{`{"requestId":"id","clean":true}`, `{"requestId":"id","sessionId":"self-reported"}`, `{"requestId":"id"} {"requestId":"second"}`} {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest("POST", "/managed", strings.NewReader(raw))
		var input d.Request
		if managedInput(c, &input) || recorder.Code != 400 {
			t.Fatal("untrusted inspection or trailing request accepted")
		}
		if recorder.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("missing no-store boundary")
		}
	}
}
