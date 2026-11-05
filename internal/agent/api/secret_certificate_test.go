package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestSecretCertificateRouteRequiresAuthenticationAndNamespace(t *testing.T) {
	router := gin.New()
	registerConfigurationObjectReadRoutes(router.Group("/platform/ownership-v2", authMiddleware("test-token")), nil)
	for _, tc := range []struct {
		name, token string
		want        int
	}{{"unauthenticated", "", 401}, {"missing namespace", "test-token", 400}} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/platform/ownership-v2/configuration/secrets/web-tls/certificate", nil)
			if tc.token != "" {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Fatalf("response=%d want=%d", w.Code, tc.want)
			}
		})
	}
}
