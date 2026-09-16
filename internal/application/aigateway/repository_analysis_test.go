package aigateway

import (
	"context"
	"reflect"
	"testing"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

type repositoryAnalysisApp struct {
	ApplicationService
	principal     domainidentity.Principal
	applicationID string
	input         sohaapi.RepositoryAnalysisInput
	result        sohaapi.RepositoryAnalysis
}

func (a *repositoryAnalysisApp) AnalyzeRepository(_ context.Context, p domainidentity.Principal, id string, input sohaapi.RepositoryAnalysisInput) (sohaapi.RepositoryAnalysis, error) {
	a.principal, a.applicationID, a.input = p, id, input
	return a.result, nil
}

func TestAIRepositoryAnalysisUsesSameAuthorizedServiceResult(t *testing.T) {
	app := &repositoryAnalysisApp{result: sohaapi.RepositoryAnalysis{ApplicationID: "app", RepositoryID: "repo", ResolvedCommit: "fixed", Status: sohaapi.AnalysisMultipleCandidates}}
	s := &Service{apps: app}
	principal := domainidentity.Principal{UserID: "developer"}
	result, _, err := s.invokeDeliveryApplicationTool(context.Background(), principal, "delivery.repositories.analyze", map[string]any{"applicationId": "app", "repositoryId": "repo", "refType": "branch", "refName": "main", "projectPath": "services/api"})
	if err != nil || !reflect.DeepEqual(result, app.result) || app.principal.UserID != "developer" || app.applicationID != "app" || app.input.ProjectPath != "services/api" {
		t.Fatalf("result=%+v app=%+v error=%v", result, app, err)
	}
	for _, files := range [][]string{{"go.mod"}, {"pom.xml"}, {"pyproject.toml"}} {
		if framework := inferDeliveryFramework(files, inferDeliveryLanguage(files)); framework != "" {
			t.Fatalf("fabricated framework %q for %v", framework, files)
		}
	}
}
