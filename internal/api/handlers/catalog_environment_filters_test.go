package handlers

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

type environmentFilterSource struct{ ApplicationEnvironmentService }

func (environmentFilterSource) ListApplicationEnvironments(context.Context, domainidentity.Principal) ([]domaincatalog.ApplicationEnvironment, error) {
	return []domaincatalog.ApplicationEnvironment{
		{ID: "wanted", ApplicationID: "app-1", EnvironmentID: "dev"},
		{ID: "other-app", ApplicationID: "app-2", EnvironmentID: "dev"},
		{ID: "other-env", ApplicationID: "app-1", EnvironmentID: "prod"},
	}, nil
}

func TestApplicationEnvironmentListAppliesDeclaredFiltersAfterAuthorization(t *testing.T) {
	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	ctx.Request = httptest.NewRequest("GET", "/application-environments?applicationId=app-1&environmentId=dev", nil)
	(&CatalogHandler{environments: environmentFilterSource{}}).ListApplicationEnvironments(ctx)
	var result struct {
		Data  []domaincatalog.ApplicationEnvironment `json:"data"`
		Items []domaincatalog.ApplicationEnvironment `json:"items"`
	}
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &result) != nil || len(result.Data) != 1 || len(result.Items) != 1 || result.Data[0].ID != "wanted" || result.Items[0].ID != "wanted" {
		t.Fatal("declared filters broadened or lost authorized rows")
	}
}
