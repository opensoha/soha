package handlers

import (
	"context"
	"github.com/gin-gonic/gin"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestControllerRevisionNamespaceDefaultAndExplicitScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, query := range []string{"", "?namespace=team-a"} {
		want := "default"
		if query != "" {
			want = "team-a"
		}
		router := gin.New()
		router.GET("/clusters/:clusterID/items/:name/history", func(c *gin.Context) {
			writeControllerHistory(c, "name", func(_ context.Context, _ domainidentity.Principal, cluster, namespace, name string) ([]domainresource.RolloutHistoryView, error) {
				if cluster != "cluster" || namespace != want || name != "agent" {
					t.Fatalf("wrong scope %s/%s/%s", cluster, namespace, name)
				}
				return []domainresource.RolloutHistoryView{}, nil
			})
		})
		router.GET("/clusters/:clusterID/items/:name/history/:revisionName/yaml", func(c *gin.Context) {
			writeControllerRevision(c, "name", func(_ context.Context, _ domainidentity.Principal, cluster, namespace, name, revision string) (domainresource.ResourceYAMLView, error) {
				if cluster != "cluster" || namespace != want || name != "agent" || revision != "rev" {
					t.Fatalf("wrong YAML scope %s/%s/%s/%s", cluster, namespace, name, revision)
				}
				return domainresource.ResourceYAMLView{}, nil
			})
		})
		for _, path := range []string{"/clusters/cluster/items/agent/history", "/clusters/cluster/items/agent/history/rev/yaml"} {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path+query, nil))
			if response.Code != http.StatusOK {
				t.Fatalf("status %d", response.Code)
			}
		}
	}
}
