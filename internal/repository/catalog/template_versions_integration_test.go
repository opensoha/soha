package catalog

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/infrastructure/config"
	dbstore "github.com/opensoha/soha/internal/infrastructure/db"
	"github.com/opensoha/soha/internal/platform/apperrors"
	apprepo "github.com/opensoha/soha/internal/repository/application"
	"go.uber.org/zap"
)

func TestTemplateVersionsWithPostgres(t *testing.T) {
	portText := os.Getenv("SOHA_CATALOG_TEST_POSTGRES_PORT")
	if portText == "" {
		t.Skip("set SOHA_CATALOG_TEST_POSTGRES_PORT to an isolated PostgreSQL instance")
	}
	port, err := strconv.Atoi(portText)
	requireTemplateNoError(t, err)
	store, err := dbstore.New(config.DatabaseConfig{
		Driver: "postgres", Host: "127.0.0.1", Port: port, Name: "soha", User: "pgsql", Password: "test-only", SSLMode: "disable",
		MaxOpenConns: 6, MaxIdleConns: 4, ConnMaxLifetime: time.Minute,
	}, zap.NewNop())
	requireTemplateNoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	requireTemplateNoError(t, store.MigrateFromFile(ctx, filepath.Join("..", "..", "..", "migrations", "postgres")))
	repo := New(store.DB())
	t.Run("immutable-build-and-concurrent-publish", func(t *testing.T) { verifyBuildTemplateVersions(t, ctx, repo) })
	t.Run("deployment-template-and-service-cas", func(t *testing.T) { verifyDeploymentTemplateVersions(t, ctx, repo) })
	t.Run("binding-keeps-published-workflow", func(t *testing.T) { verifyWorkflowTemplateBinding(t, ctx, repo) })
	t.Run("service-deployment-configuration", func(t *testing.T) { verifyServiceDeploymentConfiguration(t, ctx, repo) })
	t.Run("docker-target-without-cluster", func(t *testing.T) { verifyDockerTargetPersistence(t, ctx, repo) })
	t.Run("workflow-catalog-legacy-build-sources", func(t *testing.T) { verifyWorkflowCatalogLegacySources(t, ctx, repo) })
	t.Run("document-import-atomicity-and-replay", func(t *testing.T) { verifyDocumentImports(t, ctx, repo) })
	t.Run("git-source-transactions-and-provenance", func(t *testing.T) { verifyTemplateSources(t, ctx, repo) })
	t.Run("migrates-existing-templates-and-bindings", func(t *testing.T) { verifyLegacyTemplateMigration(t, ctx, store, port) })
}

func verifyLegacyTemplateMigration(t *testing.T, ctx context.Context, admin *dbstore.Store, port int) {
	t.Helper()
	name := "template_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	requireTemplateNoError(t, admin.Exec(ctx, `CREATE DATABASE `+name))
	t.Cleanup(func() { _ = admin.Exec(context.Background(), `DROP DATABASE `+name+` WITH (FORCE)`) })
	store, err := dbstore.New(config.DatabaseConfig{Driver: "postgres", Host: "127.0.0.1", Port: port, Name: name, User: "pgsql", Password: "test-only", SSLMode: "disable", MaxOpenConns: 2, MaxIdleConns: 1}, zap.NewNop())
	requireTemplateNoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	migrationDir := filepath.Join("..", "..", "..", "migrations", "postgres")
	stagedMigrations := t.TempDir()
	files, err := os.ReadDir(migrationDir)
	requireTemplateNoError(t, err)
	for _, file := range files {
		if strings.HasSuffix(file.Name(), ".sql") && file.Name() < "0080" {
			// #nosec G304 -- ReadDir supplies filenames from the fixed repository migration directory.
			content, readErr := os.ReadFile(filepath.Join(migrationDir, file.Name()))
			requireTemplateNoError(t, readErr)
			requireTemplateNoError(t, os.WriteFile(filepath.Join(stagedMigrations, file.Name()), content, 0o600))
		}
	}
	requireTemplateNoError(t, store.MigrateFromFile(ctx, stagedMigrations))
	requireTemplateNoError(t, store.Exec(ctx, `
INSERT INTO applications (id, app_key, name, app_group, language) VALUES ('legacy-app', 'legacy-app', 'Legacy', '', '');
INSERT INTO build_templates (id, template_key, name, build_commands) VALUES ('legacy-build', 'legacy-build', 'Legacy build', '["echo legacy"]');
INSERT INTO workflow_templates (id, template_key, name, definition) VALUES ('legacy-workflow', 'legacy-workflow', 'Legacy workflow', '{"mode":"release_dag"}');
INSERT INTO application_environments (id, application_id, environment_id, workflow_template_id) VALUES ('legacy-binding', 'legacy-app', 'dev', 'legacy-workflow');
INSERT INTO application_build_sources (id, application_id, source_name, source_type, config) VALUES ('legacy-source', 'legacy-app', 'Legacy build', 'platform_build_template', '{"buildTemplateId":"legacy-build"}');`))
	// Upgrade legacy data through the current schema before using current readers.
	for _, file := range files {
		if strings.HasSuffix(file.Name(), ".sql") && file.Name() >= "0080" {
			// #nosec G304 -- ReadDir supplies filenames from the fixed repository migration directory.
			content, readErr := os.ReadFile(filepath.Join(migrationDir, file.Name()))
			requireTemplateNoError(t, readErr)
			requireTemplateNoError(t, os.WriteFile(filepath.Join(stagedMigrations, file.Name()), content, 0o600))
		}
	}
	requireTemplateNoError(t, store.MigrateFromFile(ctx, stagedMigrations))
	repo := New(store.DB())
	build, err := repo.GetBuildTemplateVersion(ctx, "legacy-build", 1)
	requireTemplateNoError(t, err)
	workflow, err := repo.GetWorkflowTemplateVersion(ctx, "legacy-workflow", 1)
	requireTemplateNoError(t, err)
	binding, err := repo.GetApplicationEnvironment(ctx, "legacy-binding")
	requireTemplateNoError(t, err)
	var version int
	requireTemplateNoError(t, store.DB().Raw(`SELECT (config->>'buildTemplateVersion')::INT FROM application_build_sources WHERE id = 'legacy-source'`).Scan(&version).Error)
	if version != 1 || binding.WorkflowTemplateVersion != 1 || binding.WorkflowTemplate.Name != workflow.Name || build.BuildCommands[0] != "echo legacy" || workflow.CreatedAt.IsZero() {
		t.Fatalf("legacy migration lost content or pins: build=%+v workflow=%+v binding=%+v version=%d", build, workflow, binding, version)
	}
}

func requireTemplateNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func verifyBuildTemplateVersions(t *testing.T, ctx context.Context, repo *Repository) {
	t.Helper()
	draft := false
	input := domaincatalog.BuildTemplateInput{Key: uuid.NewString(), Name: "Build", BuildCommands: []string{"echo v1"}, Enabled: true, Publish: &draft}
	created, err := repo.CreateBuildTemplate(ctx, input)
	requireTemplateNoError(t, err)
	if created.Revision != 1 || created.PublishedVersion != 0 || created.ContentDigest != "" {
		t.Fatalf("draft = %#v", created)
	}
	if _, err := repo.GetBuildTemplateVersion(ctx, created.ID, 1); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("unpublished version: %v", err)
	}
	first, err := repo.PublishBuildTemplate(ctx, created.ID, created.Revision)
	requireTemplateNoError(t, err)
	input.ExpectedRevision, input.BuildCommands = &first.Revision, []string{"echo v2"}
	updated, err := repo.UpdateBuildTemplate(ctx, created.ID, input)
	requireTemplateNoError(t, err)
	old, err := repo.GetBuildTemplateVersion(ctx, created.ID, first.PublishedVersion)
	requireTemplateNoError(t, err)
	if old.BuildCommands[0] != "echo v1" || old.ContentDigest != first.ContentDigest || updated.ContentDigest != "" {
		t.Fatal("draft changed published content")
	}
	if _, err := repo.UpdateBuildTemplate(ctx, created.ID, input); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("stale edit accepted: %v", err)
	}
	results := make(chan error, 2)
	for range 2 {
		go func() { _, err := repo.PublishBuildTemplate(ctx, created.ID, updated.Revision); results <- err }()
	}
	a, b := <-results, <-results
	requireTemplateCASConflict(t, a, b)
	versions, err := repo.ListBuildTemplateVersions(ctx, created.ID)
	requireTemplateNoError(t, err)
	if len(versions) != 2 || versions[0].BuildCommands[0] != "echo v2" || !strings.HasPrefix(versions[0].ContentDigest, "sha256:") {
		t.Fatalf("versions = %#v", versions)
	}
	requireTemplateNoError(t, repo.DeleteBuildTemplate(ctx, created.ID))
	retained, err := repo.GetBuildTemplateVersion(ctx, created.ID, 1)
	requireTemplateNoError(t, err)
	if retained.ContentDigest != first.ContentDigest {
		t.Fatal("deletion changed history")
	}
	if err := repo.db.Exec(`UPDATE catalog_template_versions SET snapshot = '{}' WHERE kind = 'build' AND template_id = ?`, created.ID).Error; err == nil {
		t.Fatal("history mutation was accepted")
	}
}

func verifyWorkflowTemplateBinding(t *testing.T, ctx context.Context, repo *Repository) {
	t.Helper()
	id := uuid.NewString()
	app, err := apprepo.New(repo.db).Create(ctx, domainapp.UpsertInput{ID: id, Key: id, Name: "Template binding", Enabled: true})
	requireTemplateNoError(t, err)
	t.Cleanup(func() { _ = repo.db.Exec(`DELETE FROM applications WHERE id = ?`, id).Error })
	input := domaincatalog.WorkflowTemplateInput{Key: id, Name: "Workflow v1", Enabled: true, Definition: map[string]any{"mode": "release_dag"}}
	first, err := repo.CreateWorkflowTemplate(ctx, input)
	requireTemplateNoError(t, err)
	bindingInput := domaincatalog.ApplicationEnvironmentInput{ApplicationID: app.ID, EnvironmentID: "dev", WorkflowTemplateID: first.ID}
	binding, err := repo.CreateApplicationEnvironment(ctx, bindingInput)
	requireTemplateNoError(t, err)
	input.ExpectedRevision, input.Name = &first.Revision, "Workflow v2"
	second, err := repo.UpdateWorkflowTemplate(ctx, first.ID, input)
	requireTemplateNoError(t, err)
	// An old client saving an unchanged template ID does not opt into an upgrade.
	binding, err = repo.UpdateApplicationEnvironment(ctx, binding.ID, bindingInput)
	requireTemplateNoError(t, err)
	if binding.WorkflowTemplateVersion != 1 || binding.WorkflowTemplate.Name != first.Name {
		t.Fatal("binding drifted to latest version")
	}
	bindingInput.WorkflowTemplateVersion = second.PublishedVersion
	binding, err = repo.UpdateApplicationEnvironment(ctx, binding.ID, bindingInput)
	requireTemplateNoError(t, err)
	if binding.WorkflowTemplateVersion != 2 || binding.WorkflowTemplate.Name != second.Name {
		t.Fatal("explicit version upgrade was lost")
	}
	input.ExpectedRevision, input.Key = nil, "app-"+binding.ID
	private, err := repo.SaveApplicationWorkflow(ctx, app.ID, binding.ID, input)
	requireTemplateNoError(t, err)
	input.ExpectedRevision, input.Name = &private.Revision, "Private v2"
	privateNext, err := repo.SaveApplicationWorkflow(ctx, app.ID, binding.ID, input)
	requireTemplateNoError(t, err)
	if privateNext.ID != private.ID || privateNext.PublishedVersion != 2 {
		t.Fatal("private workflow was not versioned")
	}
	if _, err := repo.SaveApplicationWorkflow(ctx, app.ID, binding.ID, input); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("private workflow stale write accepted: %v", err)
	}
}

func verifyDeploymentTemplateVersions(t *testing.T, ctx context.Context, repo *Repository) {
	t.Helper()
	id := uuid.NewString()
	input := domaincatalog.DeploymentTemplateInput{DeploymentTemplateSpec: domaincatalog.DeploymentTemplateSpec{
		Key: id, Name: "Deployment v1", Enabled: true,
		ParameterSchema: domaincatalog.ParameterSchema{Type: "object"}, Defaults: map[string]any{},
		Source: domaincatalog.DeploymentTemplateSource{Renderer: "raw_yaml", Files: []domainmanifest.File{{Path: "config.yaml", Content: "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n"}}},
		Health: domaincatalog.DeploymentTemplateHealth{Mode: "configuration_only", TimeoutSeconds: 60},
	}}
	draft, err := repo.SaveDeploymentTemplate(ctx, "", input)
	requireTemplateNoError(t, err)
	if draft.PublishedVersion != 0 || draft.PublicationState != "draft" {
		t.Fatalf("new template not a draft: %+v", draft)
	}
	first, err := repo.PublishDeploymentTemplate(ctx, draft.ID, draft.Revision)
	requireTemplateNoError(t, err)
	input.ExpectedRevision, input.Name = &first.Revision, "Deployment v2"
	nextDraft, err := repo.SaveDeploymentTemplate(ctx, first.ID, input)
	requireTemplateNoError(t, err)
	old, err := repo.GetDeploymentTemplateVersion(ctx, first.ID, 1)
	requireTemplateNoError(t, err)
	if old.Name != "Deployment v1" || old.ContentDigest != first.ContentDigest || nextDraft.ContentDigest != "" {
		t.Fatal("published deployment template drifted")
	}
	results := make(chan error, 2)
	for range 2 {
		go func() { _, err := repo.PublishDeploymentTemplate(ctx, first.ID, nextDraft.Revision); results <- err }()
	}
	a, b := <-results, <-results
	requireTemplateCASConflict(t, a, b)
	appRepo := apprepo.New(repo.db)
	app, err := appRepo.Create(ctx, domainapp.UpsertInput{ID: id, Key: id, Name: "Deployment service", Enabled: true})
	requireTemplateNoError(t, err)
	t.Cleanup(func() { _ = repo.db.Exec(`DELETE FROM applications WHERE id = ?`, id).Error })
	serviceInput := domainapp.ServiceInput{Key: "api", Name: "API", Enabled: true, DeploymentTemplate: &domaincatalog.DeploymentTemplateBinding{TemplateID: first.ID, Version: 1, Parameters: map[string]any{"replicas": float64(2)}}}
	service, err := appRepo.CreateService(ctx, app.ID, serviceInput)
	requireTemplateNoError(t, err)
	serviceInput.ExpectedVersion = &service.Version
	for range 2 {
		go func() { _, err := appRepo.UpdateService(ctx, app.ID, service.ID, serviceInput); results <- err }()
	}
	a, b = <-results, <-results
	requireTemplateCASConflict(t, a, b)
	serviceInput.DeploymentTemplate, serviceInput.ExpectedVersion, serviceInput.Name = nil, nil, "Legacy edit"
	updated, err := appRepo.UpdateService(ctx, app.ID, service.ID, serviceInput)
	requireTemplateNoError(t, err)
	if updated.Version != 3 || updated.DeploymentTemplate == nil || updated.DeploymentTemplate.Version != 1 || updated.DeploymentTemplate.Parameters["replicas"] != float64(2) || !updated.CreatedAt.Equal(service.CreatedAt) {
		t.Fatalf("legacy edit lost frozen reference: %+v", updated)
	}
	requireTemplateNoError(t, repo.DeprecateDeploymentTemplate(ctx, first.ID))
	retained, err := repo.GetDeploymentTemplateVersion(ctx, first.ID, 1)
	requireTemplateNoError(t, err)
	if retained.ContentDigest != first.ContentDigest {
		t.Fatal("deprecation changed frozen template")
	}
	if err := repo.db.Exec(`UPDATE catalog_template_versions SET snapshot = '{}' WHERE kind = 'deployment' AND template_id = ?`, first.ID).Error; err == nil {
		t.Fatal("deployment history mutation accepted")
	}
}

func requireTemplateCASConflict(t *testing.T, first, second error) {
	t.Helper()
	if first == nil && errors.Is(second, apperrors.ErrConflict) {
		return
	}
	if second == nil && errors.Is(first, apperrors.ErrConflict) {
		return
	}
	t.Fatalf("concurrent writes must have one success and one conflict: %v / %v", first, second)
}
