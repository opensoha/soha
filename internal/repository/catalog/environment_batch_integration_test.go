package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
	apprepo "github.com/opensoha/soha/internal/repository/application"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type environmentBatchTrace struct {
	logger.Interface
	statements []string
}

func (l *environmentBatchTrace) Trace(_ context.Context, _ time.Time, sql func() (string, int64), _ error) {
	statement, _ := sql()
	l.statements = append(l.statements, statement)
}

func verifyApplicationEnvironmentBatch(t *testing.T, ctx context.Context, repo *Repository) {
	t.Helper()
	appID, otherID := uuid.NewString(), uuid.NewString()
	apps := apprepo.New(repo.db)
	for _, id := range []string{appID, otherID} {
		_, err := apps.Create(ctx, domainapp.UpsertInput{ID: id, Key: id, Name: "Environment batch check", Enabled: true})
		requireTemplateNoError(t, err)
		t.Cleanup(func() { requireTemplateNoError(t, repo.db.Exec(`DELETE FROM applications WHERE id = ?`, id).Error) })
	}
	input := domaincatalog.WorkflowTemplateInput{Key: uuid.NewString(), Name: "Pinned v1", Definition: map[string]any{"mode": "release_dag"}, Enabled: true}
	first, err := repo.CreateWorkflowTemplate(ctx, input)
	requireTemplateNoError(t, err)
	input.ExpectedRevision, input.Name = &first.Revision, "Pinned v2"
	second, err := repo.UpdateWorkflowTemplate(ctx, first.ID, input)
	requireTemplateNoError(t, err)

	oldTarget, newTarget := uuid.NewString(), uuid.NewString()
	docker := &sohaapi.DockerDeliveryConfiguration{HostID: "host", ProjectID: "project"}
	old, err := repo.CreateApplicationEnvironment(ctx, domaincatalog.ApplicationEnvironmentInput{
		ApplicationID: appID, EnvironmentID: "dev", WorkflowTemplateID: first.ID, WorkflowTemplateVersion: 1,
		Targets: []domaincatalog.ReleaseTargetInput{
			{ID: newTarget, TargetKind: "host_service", ExecutorKind: "docker_compose", WorkloadKind: "DockerProject", WorkloadName: "new", Enabled: true, Docker: docker},
			{ID: oldTarget, TargetKind: "host_service", ExecutorKind: "docker_compose", WorkloadKind: "DockerProject", WorkloadName: "old", Enabled: true, Docker: docker},
		},
	})
	requireTemplateNoError(t, err)
	requireTemplateNoError(t, repo.db.Exec(`UPDATE release_targets SET created_at = created_at + INTERVAL '1 second' WHERE id = ?`, newTarget).Error)
	current, err := repo.CreateApplicationEnvironment(ctx, domaincatalog.ApplicationEnvironmentInput{
		ApplicationID: appID, EnvironmentID: "prod", WorkflowTemplateID: first.ID, WorkflowTemplateVersion: 2,
	})
	requireTemplateNoError(t, err)
	plain, err := repo.CreateApplicationEnvironment(ctx, domaincatalog.ApplicationEnvironmentInput{ApplicationID: appID, EnvironmentID: "plain"})
	requireTemplateNoError(t, err)
	other, err := repo.CreateApplicationEnvironment(ctx, domaincatalog.ApplicationEnvironmentInput{
		ApplicationID: otherID, EnvironmentID: "dev", WorkflowTemplateID: first.ID, WorkflowTemplateVersion: 2,
	})
	requireTemplateNoError(t, err)

	trace := &environmentBatchTrace{Interface: logger.Default}
	db := repo.db.Session(&gorm.Session{Logger: trace})
	counted := New(db)
	all, err := counted.ListApplicationEnvironments(ctx)
	requireTemplateNoError(t, err)
	if len(all) < 4 || len(trace.statements) != 3 {
		t.Fatalf("unscoped rows=%d reads=%d, want all bindings in three reads", len(all), len(trace.statements))
	}
	trace.statements = nil
	scoped, err := counted.ListApplicationEnvironmentsByApplication(ctx, appID)
	requireTemplateNoError(t, err)
	if len(trace.statements) != 3 || !strings.Contains(trace.statements[0], "WHERE ae.application_id =") {
		t.Fatalf("scoped list used %d reads, want three with application predicate", len(trace.statements))
	}
	if len(scoped) != 3 || scoped[0].ID != plain.ID || scoped[1].ID != current.ID || scoped[2].ID != old.ID {
		t.Fatalf("unexpected scoped bindings/order: %+v", scoped)
	}
	if scoped[0].WorkflowTemplate != nil || scoped[0].Targets == nil || len(scoped[0].Targets) != 0 {
		t.Fatalf("unbound environment changed empty fields: %+v", scoped[0])
	}
	assertWorkflowPin(t, scoped[1], second)
	assertWorkflowPin(t, scoped[2], first)
	targets := scoped[2].Targets
	if len(targets) != 2 || targets[0].ID != oldTarget || targets[1].ID != newTarget {
		t.Fatalf("targets were lost or reordered: %+v", targets)
	}
	trace.statements = nil
	empty, err := counted.ListApplicationEnvironmentsByApplication(ctx, "")
	if err != nil || empty == nil || len(empty) != 0 || len(trace.statements) != 1 {
		t.Fatalf("empty scope queried unrelated bindings: count=%d reads=%d err=%v", len(empty), len(trace.statements), err)
	}

	verifyBatchReadCorruptPins(t, ctx, db, appID, other, first)
}

func assertWorkflowPin(t *testing.T, binding domaincatalog.ApplicationEnvironment, version domaincatalog.WorkflowTemplate) {
	t.Helper()
	got := binding.WorkflowTemplate
	if got == nil || got.Name != version.Name || got.ContentDigest != version.ContentDigest || got.PublishedVersion != version.PublishedVersion {
		t.Fatalf("binding %q lost its workflow pin: %+v", binding.ID, got)
	}
}

func verifyBatchReadCorruptPins(t *testing.T, ctx context.Context, db *gorm.DB, appID string, other domaincatalog.ApplicationEnvironment, template domaincatalog.WorkflowTemplate) {
	t.Helper()
	repo := New(db)
	rollback := errors.New("rollback test-only pin edits")
	err := dbtx.Within(ctx, db, func(ctx context.Context) error {
		tx := dbtx.DB(ctx, db)
		requireTemplateNoError(t, tx.Exec(`UPDATE application_environments SET workflow_template_version = 911 WHERE id = ?`, other.ID).Error)
		requireTemplateNoError(t, tx.Exec(`INSERT INTO catalog_template_versions (kind, template_id, version, snapshot, content_digest) VALUES ('build', ?, 911, '{}'::jsonb, ?)`, template.ID, template.ContentDigest).Error)
		scoped, err := repo.ListApplicationEnvironmentsByApplication(ctx, appID)
		if err != nil || len(scoped) != 3 {
			t.Fatalf("unrelated missing workflow pin affected scoped read: count=%d err=%v", len(scoped), err)
		}
		if bindings, err := repo.ListApplicationEnvironments(ctx); !errors.Is(err, apperrors.ErrNotFound) || bindings != nil {
			t.Fatalf("missing workflow pin returned partial list or matched wrong kind: count=%d err=%v", len(bindings), err)
		}
		requireTemplateNoError(t, tx.Exec(`INSERT INTO catalog_template_versions (kind, template_id, version, snapshot, content_digest) VALUES ('workflow', ?, 911, '[]'::jsonb, ?)`, template.ID, template.ContentDigest).Error)
		bindings, err := repo.ListApplicationEnvironmentsByApplication(ctx, other.ApplicationID)
		var decodeErr *json.UnmarshalTypeError
		if !errors.As(err, &decodeErr) || bindings != nil {
			t.Fatalf("invalid workflow snapshot returned partial list: count=%d err=%v", len(bindings), err)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("corrupt pin test did not finish its rollback: %v", err)
	}
}
