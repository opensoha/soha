package delivery_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	appbuild "github.com/opensoha/soha/internal/application/build"
	"github.com/opensoha/soha/internal/application/execution"
	apprelease "github.com/opensoha/soha/internal/application/release"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domainbuild "github.com/opensoha/soha/internal/domain/build"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainrelease "github.com/opensoha/soha/internal/domain/release"
	"github.com/opensoha/soha/internal/platform/apperrors"
	repoapp "github.com/opensoha/soha/internal/repository/application"
	repobuild "github.com/opensoha/soha/internal/repository/build"
	repodelivery "github.com/opensoha/soha/internal/repository/delivery"
	reporelease "github.com/opensoha/soha/internal/repository/release"
)

const publicationTestImage = "registry.example/publication:v1"

type executionPublicationRepo struct {
	*repodelivery.Repository
	onQueued        func(string)
	checkRetryReset func() error
}

func (r executionPublicationRepo) CreateExecutionLog(ctx context.Context, item domaindelivery.ExecutionLog) error {
	if strings.Contains(item.Message, "waiting for runner claim") {
		r.onQueued(item.ExecutionTaskID)
	}
	return r.Repository.CreateExecutionLog(ctx, item)
}

func (r executionPublicationRepo) UpdateReleaseBundle(ctx context.Context, bundle domaindelivery.ReleaseBundle) (domaindelivery.ReleaseBundle, error) {
	stored, err := r.Repository.UpdateReleaseBundle(ctx, bundle)
	if err == nil && bundle.Status == "building" && bundle.Metadata["retryTaskId"] != nil {
		err = r.checkRetryReset()
	}
	return stored, err
}

type buildPublicationRepo struct {
	*repobuild.Repository
	checkWrite    func() error
	beforeFailure func()
}

type retryBuildProjectionRepo struct {
	*repobuild.Repository
	checkReset func(domainbuild.Record) error
}

func (r retryBuildProjectionRepo) Update(ctx context.Context, record domainbuild.Record) (domainbuild.Record, error) {
	stored, err := r.Repository.Update(ctx, record)
	if err == nil && record.Status == "queued" && record.Metadata["retryRequestedAt"] != nil {
		err = r.checkReset(record)
	}
	return stored, err
}

func (r buildPublicationRepo) UpdateUnlinked(ctx context.Context, record domainbuild.Record) (domainbuild.Record, error) {
	if r.beforeFailure != nil {
		r.beforeFailure()
	}
	return r.Repository.UpdateUnlinked(ctx, record)
}

func (r buildPublicationRepo) Update(ctx context.Context, record domainbuild.Record) (domainbuild.Record, error) {
	stored, err := r.Repository.Update(ctx, record)
	if err == nil && record.Metadata["executionTaskId"] != nil {
		err = r.checkWrite()
	}
	return stored, err
}

type releasePublicationRepo struct {
	*reporelease.Repository
	checkWrite func() error
}

func (r releasePublicationRepo) Create(ctx context.Context, record domainrelease.Record) (domainrelease.Record, error) {
	stored, err := r.Repository.Create(ctx, record)
	if err == nil {
		err = r.checkWrite()
	}
	return stored, err
}

type publicationReleaseConfig struct{}

func (publicationReleaseConfig) GetApplicationEnvironment(context.Context, string) (domaincatalog.ApplicationEnvironment, error) {
	return domaincatalog.ApplicationEnvironment{Targets: []domaincatalog.ReleaseTarget{{Enabled: true, TargetKind: "host_service", ExecutorKind: "ci_agent_runner"}}}, nil
}

func (publicationReleaseConfig) GetConnection(_ context.Context, id string) (domaincluster.Connection, error) {
	return domaincluster.Connection{Summary: domaincluster.Summary{ID: id}}, nil
}

func (publicationReleaseConfig) ListRolePermissions(context.Context) (map[string][]string, error) {
	return map[string][]string{"tester": {appaccess.PermDeliveryReleasesTrigger}}, nil
}

func TestExecutionProjectionPublishedWithTaskWithPostgres(t *testing.T) {
	store := openDraftReceiptTestStore(t)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	if err := store.MigrateFromFile(ctx, filepath.Join("..", "..", "..", "migrations", "postgres")); err != nil {
		t.Fatal(err)
	}
	db := store.DB()
	apps, builds, releases, repo := repoapp.New(db), repobuild.New(db), reporelease.New(db), repodelivery.New(db)
	digest := "sha256:" + strings.Repeat("a", 64)
	for _, kind := range []string{"build", "release"} {
		t.Run(kind, func(t *testing.T) {
			appID := uuid.NewString()
			if _, err := apps.Create(ctx, domainapp.UpsertInput{ID: appID, Key: appID, Name: "Execution publication", Enabled: true, BuildImage: "registry.example/publication", DefaultTag: "v1", BuildSources: []domainapp.BuildSourceInput{{ID: uuid.NewString(), Name: "Source", Type: domainapp.BuildSourceTypeRepoDockerfile, Enabled: true, Config: map[string]any{"providerKind": "ci_agent_runner"}}}}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				for _, table := range []string{"build_records", "deploy_records"} {
					if err := db.Exec("DELETE FROM "+table+" WHERE project_id = ?", appID).Error; err != nil {
						t.Error(err)
					}
				}
				if err := db.Exec(`DELETE FROM applications WHERE id = ?`, appID).Error; err != nil {
					t.Error(err)
				}
			})
			validateExecutionPublication(t, ctx, apps, builds, releases, repo, appID, kind, digest)
		})
	}
}

func validateExecutionPublication(t *testing.T, ctx context.Context, apps *repoapp.Repository, builds *repobuild.Repository, releases *reporelease.Repository, repo *repodelivery.Repository, appID, kind, digest string) {
	t.Helper()
	rejectWrite := true
	status := "completed"
	if kind == "build" {
		status = "failed"
	}
	var accepted, claimed domaindelivery.ExecutionTask
	var service *execution.Service
	rejectReset, rejectRecordReset := false, false
	assertUnpublished := func() {
		// Use the outer context so this runner uses a different database connection.
		if task, err := service.ClaimExecutionTask(ctx, []string{"ci_agent_runner"}, "early-runner", ""); !errors.Is(err, apperrors.ErrNotFound) {
			t.Errorf("task became claimable before projection committed: task=%s err=%v", task.ID, err)
		}
	}
	retryBuilds := retryBuildProjectionRepo{builds, func(record domainbuild.Record) error {
		assertUnpublished()
		if record.StartedAt != nil || record.FinishedAt != nil {
			t.Errorf("queued retry retained build timestamps: %+v", record)
		}
		if rejectRecordReset {
			return errCallbackProjectionTest
		}
		return nil
	}}
	service = execution.New(executionPublicationRepo{Repository: repo, checkRetryReset: func() error {
		assertUnpublished()
		if rejectReset {
			return errCallbackProjectionTest
		}
		return nil
	}, onQueued: func(taskID string) {
		var err error
		claimed, err = service.ClaimExecutionTask(ctx, []string{"ci_agent_runner"}, "test-runner", "")
		if err != nil || claimed.ID != taskID {
			t.Fatalf("expected task %s to be claimable after commit: task=%s err=%v", taskID, claimed.ID, err)
		}
		accepted, err = service.RecordCallback(ctx, publicationCallback(claimed.CallbackToken, status, digest))
		if err != nil || accepted.Status != status {
			t.Fatalf("immediate callback status=%q want=%q err=%v", accepted.Status, status, err)
		}
	}}, retryBuilds, releases, nil, "", "", "", "", 0, "", nil)
	checkWrite := func() error {
		assertUnpublished()
		if rejectWrite {
			return errCallbackProjectionTest
		}
		return nil
	}
	trigger := publicationTrigger(t, ctx, apps, builds, releases, repo, service, checkWrite, appID, kind, digest)
	if _, err := trigger(); !errors.Is(err, errCallbackProjectionTest) {
		t.Fatalf("expected projection write failure, got %v", err)
	}
	if tasks, err := repo.ListExecutionTasks(ctx, domaindelivery.ExecutionTaskFilter{ApplicationID: appID}); err != nil || len(tasks) != 0 {
		t.Fatalf("failed projection left tasks: %+v err=%v", tasks, err)
	}
	bundles, err := repo.ListReleaseBundles(ctx, domaindelivery.ReleaseBundleFilter{ApplicationID: appID})
	if err != nil || (kind == "build" && len(bundles) != 0) || (kind == "release" && (len(bundles) != 1 || bundles[0].Status != "ready")) {
		t.Fatalf("failed projection unexpectedly changed bundles: %+v err=%v", bundles, err)
	}
	if records, err := releases.List(ctx, domainrelease.Filter{ApplicationID: appID}); err != nil || len(records) != 0 {
		t.Fatalf("failed publication left a release record: %+v err=%v", records, err)
	}
	rejectWrite = false
	want := "failed"
	if kind == "release" {
		want = "deployed"
	}
	if state, err := trigger(); err != nil || state != want {
		t.Fatalf("trigger returned state=%q want=%q err=%v", state, want, err)
	}
	if kind == "release" {
		if record, err := releases.GetByExecutionTaskID(ctx, claimed.ID); err != nil || record.Status != "deployed" || record.DeployedAt == nil {
			t.Fatalf("release projection did not observe immediate callback: %+v err=%v", record, err)
		}
		if replay, err := service.RecordCallback(ctx, publicationCallback(claimed.CallbackToken, "failed", digest)); err != nil || replay.Status != "completed" {
			t.Fatalf("terminal replay overwrote release result: status=%s err=%v", replay.Status, err)
		}
		return
	}
	oldToken := claimed.CallbackToken
	for _, failing := range []*bool{&rejectReset, &rejectRecordReset} {
		*failing = true
		if _, err := service.RetryExecutionTask(ctx, claimed.ID, domaindelivery.ExecutionTaskActionInput{}); !errors.Is(err, errCallbackProjectionTest) {
			t.Fatalf("expected injected retry reset failure, got %v", err)
		}
		assertFailedRetryRollback(t, ctx, repo, builds, claimed.ID, oldToken, accepted.ReleaseBundleID)
		*failing = false
	}
	status = "completed"
	if _, err := service.RetryExecutionTask(ctx, claimed.ID, domaindelivery.ExecutionTaskActionInput{}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RecordCallback(ctx, publicationCallback(oldToken, "failed", digest)); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("old callback token reached the new attempt: %v", err)
	}
	assertExecutionCallbackProjection(t, ctx, repo, builds, accepted.ID, accepted.ReleaseBundleID, "completed", "ready", "completed", publicationTestImage)
}

func assertFailedRetryRollback(t *testing.T, ctx context.Context, repo *repodelivery.Repository, builds *repobuild.Repository, taskID, oldToken, bundleID string) {
	t.Helper()
	if task, err := repo.GetExecutionTask(ctx, taskID); err != nil || task.Status != "failed" || task.CallbackToken != oldToken {
		t.Fatalf("failed retry reset changed task status or token: status=%q err=%v", task.Status, err)
	}
	if bundle, err := repo.GetReleaseBundle(ctx, bundleID); err != nil || bundle.Status != "failed" {
		t.Fatalf("failed retry reset changed release bundle: status=%q err=%v", bundle.Status, err)
	}
	if record, err := builds.GetByExecutionTaskID(ctx, taskID); err != nil || record.Status != "failed" || record.FinishedAt == nil {
		t.Fatalf("failed retry reset changed build projection: %+v err=%v", record, err)
	}
}

func publicationCallback(token, status, digest string) domaindelivery.ExecutionCallbackInput {
	input := domaindelivery.ExecutionCallbackInput{CallbackToken: token, Status: status}
	if status == "completed" {
		input.Payload = map[string]any{"image": publicationTestImage, "imageDigest": digest}
	}
	return input
}

func publicationTrigger(t *testing.T, ctx context.Context, apps *repoapp.Repository, builds *repobuild.Repository, releases *reporelease.Repository, repo *repodelivery.Repository, service *execution.Service, checkWrite func() error, appID, kind, digest string) func() (string, error) {
	t.Helper()
	if kind == "build" {
		build := appbuild.New(buildPublicationRepo{Repository: builds, checkWrite: checkWrite}, apps, nil, service, nil, nil, nil, nil)
		return func() (string, error) {
			record, err := build.Trigger(ctx, domainidentity.Principal{}, domainbuild.TriggerInput{ApplicationID: appID, RefType: "branch", RefName: "main"})
			return record.Status, err
		}
	}
	now := time.Now().UTC()
	bundle, err := repo.CreateReleaseBundle(ctx, domaindelivery.ReleaseBundle{ID: uuid.NewString(), ApplicationID: appID, Version: "v1", SourceType: "build", Status: "ready", ArtifactRef: publicationTestImage, ArtifactDigest: digest, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	config := publicationReleaseConfig{}
	release := apprelease.New(releasePublicationRepo{releases, checkWrite}, apps, config, config, service, nil, appaccess.NewPermissionResolver(config), nil, nil, nil, nil, nil, nil)
	return func() (string, error) {
		record, err := release.Trigger(ctx, domainidentity.Principal{Roles: []string{"tester"}}, domainrelease.TriggerInput{ApplicationID: appID, ApplicationEnvironmentID: "binding", ClusterID: "cluster", Namespace: "demo", DeploymentName: "app", Image: publicationTestImage, ReleaseBundleID: bundle.ID})
		return record.Status, err
	}
}
