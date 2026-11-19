package delivery_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opensoha/soha/internal/application/execution"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domainbuild "github.com/opensoha/soha/internal/domain/build"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	repoapp "github.com/opensoha/soha/internal/repository/application"
	repobuild "github.com/opensoha/soha/internal/repository/build"
	repodelivery "github.com/opensoha/soha/internal/repository/delivery"
)

var errCallbackProjectionTest = errors.New("injected callback build projection failure")

type failingCallbackBuildRepository struct{ *repobuild.Repository }

func (r failingCallbackBuildRepository) Update(ctx context.Context, record domainbuild.Record) (domainbuild.Record, error) {
	record, err := r.Repository.Update(ctx, record)
	if err != nil {
		return record, err
	}
	return record, errCallbackProjectionTest
}

func TestExecutionCallbackProjectionWithPostgres(t *testing.T) {
	store := openDraftReceiptTestStore(t)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	if err := store.MigrateFromFile(ctx, filepath.Join("..", "..", "..", "migrations", "postgres")); err != nil {
		t.Fatal(err)
	}
	db := store.DB()
	repo, builds := repodelivery.New(db), repobuild.New(db)
	appID, token, taskID, bundleID := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	if _, err := repoapp.New(db).Create(ctx, domainapp.UpsertInput{ID: appID, Key: appID, Name: "Callback projection check", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Exec(`DELETE FROM build_records WHERE project_id = ?`, appID).Error; err != nil {
			t.Error(err)
		}
		if err := db.Exec(`DELETE FROM applications WHERE id = ?`, appID).Error; err != nil {
			t.Error(err)
		}
	})
	now := time.Now().UTC().Add(-time.Minute)
	if _, err := repo.CreateReleaseBundle(ctx, domaindelivery.ReleaseBundle{
		ID: bundleID, ApplicationID: appID, Version: "v1", SourceType: "build",
		Status: "building", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateExecutionTask(ctx, domaindelivery.ExecutionTask{
		ID: taskID, ReleaseBundleID: bundleID, ApplicationID: appID, TaskKind: "build", ProviderKind: "ci_agent_runner",
		Status: "running", AttemptCount: 1, CallbackToken: token, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := builds.Create(ctx, domainbuild.TriggerInput{ApplicationID: appID}, map[string]any{"executionTaskId": taskID}); err != nil {
		t.Fatal(err)
	}

	digest := "sha256:" + strings.Repeat("a", 64)
	callback := func(tag string) domaindelivery.ExecutionCallbackInput {
		return domaindelivery.ExecutionCallbackInput{CallbackToken: token, Status: "completed", Payload: map[string]any{
			"image": "registry.example/app:" + tag, "imageDigest": digest,
		}}
	}
	failing := execution.New(repo, failingCallbackBuildRepository{builds}, nil, nil, "", "", "", "", 0, "", nil)
	if _, err := failing.RecordCallback(ctx, callback("failed-projection")); !errors.Is(err, errCallbackProjectionTest) {
		t.Fatalf("callback should return projection error, got %v", err)
	}
	assertExecutionCallbackProjection(t, ctx, repo, builds, taskID, bundleID, "running", "building", "queued", "")
	var callbacks int
	if err := db.WithContext(ctx).Raw(`SELECT count(*) FROM execution_callbacks WHERE execution_task_id = ?`, taskID).Scan(&callbacks).Error; err != nil || callbacks != 0 {
		t.Fatalf("failed callback receipt persisted: count=%d err=%v", callbacks, err)
	}

	service := execution.New(repo, builds, nil, nil, "", "", "", "", 0, "", nil)
	type outcome struct {
		task domaindelivery.ExecutionTask
		err  error
	}
	outcomes := make(chan outcome, 2)
	for _, tag := range []string{"retry-a", "retry-b"} {
		go func() {
			task, err := service.RecordCallback(ctx, callback(tag))
			outcomes <- outcome{task, err}
		}()
	}
	first, second := <-outcomes, <-outcomes
	if first.err != nil || second.err != nil || first.task.Result["image"] != second.task.Result["image"] {
		t.Fatalf("terminal callbacks must share one durable result: first=%+v second=%+v", first, second)
	}
	image, _ := first.task.Result["image"].(string)
	assertExecutionCallbackProjection(t, ctx, repo, builds, taskID, bundleID, "completed", "ready", "completed", image)
}

func assertExecutionCallbackProjection(t *testing.T, ctx context.Context, repo *repodelivery.Repository, builds *repobuild.Repository, taskID, bundleID, taskStatus, bundleStatus, buildStatus, artifactRef string) {
	t.Helper()
	if task, err := repo.GetExecutionTask(ctx, taskID); err != nil || task.Status != taskStatus {
		t.Fatalf("task projection status = %q, want %q, error = %v", task.Status, taskStatus, err)
	}
	if bundle, err := repo.GetReleaseBundle(ctx, bundleID); err != nil || bundle.Status != bundleStatus || bundle.ArtifactRef != artifactRef {
		t.Fatalf("bundle projection = %q/%q, want %q/%q, error = %v", bundle.Status, bundle.ArtifactRef, bundleStatus, artifactRef, err)
	}
	if record, err := builds.GetByExecutionTaskID(ctx, taskID); err != nil || record.Status != buildStatus {
		t.Fatalf("build projection status = %q, want %q, error = %v", record.Status, buildStatus, err)
	}
	artifacts, err := repo.ListExecutionArtifacts(ctx, taskID)
	if err != nil || artifactRef == "" && len(artifacts) != 0 || artifactRef != "" && (len(artifacts) != 1 || artifacts[0].Ref != artifactRef) {
		t.Fatalf("artifact projection = %+v, want ref %q, error = %v", artifacts, artifactRef, err)
	}
}
