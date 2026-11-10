package middleware

import (
	"context"
	"errors"
	"github.com/gin-gonic/gin"
	identity "github.com/opensoha/soha/internal/domain/identity"
	config "github.com/opensoha/soha/internal/infrastructure/config"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

type dshRejectParser struct{ calls int }

func (p *dshRejectParser) ParseAccessToken(context.Context, string) (identity.Principal, identity.AccessContext, error) {
	p.calls++
	return identity.Principal{}, identity.AccessContext{}, errors.New("invalid user token")
}
func TestDSHCallbackBypassesOnlyExactServiceRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	callbacks := []string{"/custom/copilot/dsh/tools", "/custom/copilot/dsh/model/v1/chat/completions", "/custom/copilot/dsh/assembly", "/custom/copilot/dsh/plugin-tools/authorize", "/custom/copilot/dsh/plugin-tools/settle"}
	stdio := "/custom/copilot/dsh/personal-stdio"
	for _, path := range append(slices.Clone(callbacks), "/custom/copilot/dsh/tools/extra", "/custom/copilot/dsh/assembly/extra", "/custom/copilot/dsh/plugin-tools/settle/extra", stdio, stdio+"/extra", "/copilot/dsh/personal-stdio", "/custom/copilot/sessions", "/copilot/dsh/assembly") {
		for _, method := range []string{http.MethodPost, http.MethodGet} {
			p := &dshRejectParser{}
			e := gin.New()
			e.Use(BuildPrincipalMiddleware(config.AuthConfig{}, p, "/custom"))
			e.Any(path, func(c *gin.Context) { c.Status(204) })
			r := httptest.NewRequest(method, path, nil)
			r.Header.Set("Authorization", "Bearer service-token")
			w := httptest.NewRecorder()
			e.ServeHTTP(w, r)
			expected := 401
			if method == http.MethodPost && slices.Contains(callbacks, path) || method == http.MethodGet && path == stdio {
				expected = 204
			}
			if w.Code != expected {
				t.Fatalf("%s %s %d", method, path, w.Code)
			}
			if expected == 204 && p.calls != 0 {
				t.Fatal("service token parsed as JWT")
			}
		}
	}
}
