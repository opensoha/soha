package access

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestHandlersRejectInvalidPayloadsWithStableErrorCode(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		handler gin.HandlerFunc
	}{
		{name: "user", path: "/access/users", handler: New(Services{}).CreateUser},
		{name: "scope grant", path: "/access/scope-grants", handler: NewScopeGrantHandler(nil).Create},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			router := gin.New()
			router.POST(tt.path, tt.handler)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader("{"))
			request.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(recorder, request)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
			}
			if !strings.Contains(recorder.Body.String(), `"code":"invalid_argument"`) {
				t.Fatalf("body = %s, want stable invalid_argument code", recorder.Body.String())
			}
		})
	}
}

func TestValidateEffect(t *testing.T) {
	for _, tt := range []struct {
		name    string
		value   string
		wantErr bool
	}{
		{name: "default allow"},
		{name: "allow", value: "allow"},
		{name: "deny", value: "deny"},
		{name: "unsupported", value: "audit", wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateEffect(tt.value); (err != nil) != tt.wantErr {
				t.Fatalf("validateEffect(%q) error = %v, wantErr %v", tt.value, err, tt.wantErr)
			}
		})
	}
}
