package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	cr "github.com/opensoha/soha/contracts/resource"
	appaccess "github.com/opensoha/soha/internal/application/access"
	catalogservice "github.com/opensoha/soha/internal/application/catalog"
	da "github.com/opensoha/soha/internal/domain/application"
	dc "github.com/opensoha/soha/internal/domain/catalog"
	di "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	apprepo "github.com/opensoha/soha/internal/repository/application"
)

func verifyRelationRulesPersistence(t *testing.T, ctx context.Context, repo *Repository) {
	t.Helper()
	apps := apprepo.New(repo.db)
	appID, clusterID := uuid.NewString(), uuid.NewString()
	_, err := apps.Create(ctx, da.UpsertInput{ID: appID, Key: appID, Name: "Relations", Enabled: true})
	requireTemplateNoError(t, err)
	requireTemplateNoError(t, repo.db.Exec(`INSERT INTO clusters (id,name) VALUES (?, 'Relations')`, clusterID).Error)
	t.Cleanup(func() {
		_ = repo.db.Exec(`DELETE FROM applications WHERE id = ?`, appID).Error
		_ = repo.db.Exec(`DELETE FROM clusters WHERE id = ?`, clusterID).Error
	})
	metadata := map[string]any{"importSource": "kubernetes", "resourceSource": `{"resource":{"uid":"root"}}`, "unrelated": "keep", "resourceRelations": cr.RelationConfig{Version: "v1", RootUID: "root", Sources: []cr.RelationSource{}, Rules: []cr.RelationRule{}}}
	input := dc.ApplicationEnvironmentInput{ApplicationID: appID, EnvironmentID: "dev", ClusterID: clusterID, Namespace: "relations", Targets: []dc.ReleaseTargetInput{{ClusterID: clusterID, Namespace: "relations", WorkloadKind: "CustomResource", WorkloadName: "widget", Enabled: true, Metadata: metadata}}}
	binding, err := repo.CreateApplicationEnvironment(ctx, input)
	requireTemplateNoError(t, err)
	access := deploymentSetupAccess{}
	catalog := catalogservice.New(repo, access, apps, appaccess.NewPermissionResolver(access), nil, nil)
	principal := di.Principal{UserID: "template-test", Roles: []string{"template-test"}}
	input.Targets[0].ID = binding.Targets[0].ID
	if _, err = catalog.UpdateApplicationEnvironment(ctx, principal, binding.ID, input); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("unfenced replacement accepted: %v", err)
	}
	input.ExpectedUpdatedAt = &binding.UpdatedAt
	results := make(chan error, 2)
	for _, value := range []string{"one", "two"} {
		go func() {
			current := input
			current.Targets = append([]dc.ReleaseTargetInput(nil), input.Targets...)
			current.Targets[0].Metadata = map[string]any{}
			for key, value := range metadata {
				current.Targets[0].Metadata[key] = value
			}
			current.Targets[0].Metadata["resourceRelations"] = cr.RelationConfig{Version: "v1", RootUID: "root", Sources: []cr.RelationSource{}, Rules: []cr.RelationRule{{ID: "pods", Type: "selector", Target: cr.RelationSource{APIVersion: "v1", Resource: "pods", Kind: "Pod", Namespace: "relations"}, Selector: map[string]string{"app": value}}}}
			_, err := catalog.UpdateApplicationEnvironment(ctx, principal, binding.ID, current)
			results <- err
		}()
	}
	requireTemplateCASConflict(t, <-results, <-results)
	saved, err := repo.GetApplicationEnvironment(ctx, binding.ID)
	requireTemplateNoError(t, err)
	data, err := json.Marshal(saved.Targets[0].Metadata["resourceRelations"])
	requireTemplateNoError(t, err)
	var config cr.RelationConfig
	requireTemplateNoError(t, json.Unmarshal(data, &config))
	if len(config.Rules) != 1 || config.RootUID != "root" || saved.Targets[0].Metadata["unrelated"] != "keep" {
		t.Fatalf("rules or unrelated metadata lost: %+v", saved.Targets)
	}
	if _, err = catalog.UpdateApplicationEnvironment(ctx, principal, binding.ID, input); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("stale revision accepted: %v", err)
	}
}
