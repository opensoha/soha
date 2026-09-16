package catalog

import (
	"context"
	"github.com/google/uuid"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	apprepo "github.com/opensoha/soha/internal/repository/application"
	"reflect"
	"testing"
)

func verifyDockerTargetPersistence(t *testing.T, ctx context.Context, repo *Repository) {
	t.Helper()
	appID := uuid.NewString()
	_, err := apprepo.New(repo.db).Create(ctx, domainapp.UpsertInput{ID: appID, Key: appID, Name: "Docker target check", Enabled: true})
	requireTemplateNoError(t, err)
	t.Cleanup(func() { _ = repo.db.Exec(`DELETE FROM applications WHERE id = ?`, appID).Error })
	config := &sohaapi.DockerDeliveryConfiguration{HostID: "host", ProjectID: "project", ImageMappings: map[string]string{"api": "main"}}
	input := domaincatalog.ApplicationEnvironmentInput{ApplicationID: appID, EnvironmentID: "dev", Targets: []domaincatalog.ReleaseTargetInput{{ID: uuid.NewString(), TargetKind: "host_service", ExecutorKind: "docker_compose", WorkloadKind: "DockerProject", WorkloadName: "project", Enabled: true, Docker: config}}}
	binding, err := repo.CreateApplicationEnvironment(ctx, input)
	requireTemplateNoError(t, err)
	stored, err := repo.GetApplicationEnvironment(ctx, binding.ID)
	requireTemplateNoError(t, err)
	if len(stored.Targets) != 1 || stored.Targets[0].ClusterID != "" || !reflect.DeepEqual(stored.Targets[0].Docker, config) {
		t.Fatalf("Docker target round trip: %+v", stored.Targets)
	}
	input.Targets[0].Docker = &sohaapi.DockerDeliveryConfiguration{HostID: "new-host", ProjectID: "new-project", ImageMappings: map[string]string{"worker": "main"}}
	input.Targets[0].WorkloadName = "new-project"
	_, err = repo.UpdateApplicationEnvironment(ctx, binding.ID, input)
	requireTemplateNoError(t, err)
	stored, err = repo.GetApplicationEnvironment(ctx, binding.ID)
	requireTemplateNoError(t, err)
	if !reflect.DeepEqual(stored.Targets[0].Docker, input.Targets[0].Docker) {
		t.Fatal("Docker configuration lost on binding update")
	}
}
