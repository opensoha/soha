package execution

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainbuild "github.com/opensoha/soha/internal/domain/build"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainrelease "github.com/opensoha/soha/internal/domain/release"
)

func TestTaskHeartbeatExpiredUsesHeartbeatStartedAndCreatedAt(t *testing.T) {
	now := time.Date(2026, 6, 4, 10, 30, 0, 0, time.UTC)
	startedAt := now.Add(-4 * time.Minute)
	lastHeartbeatAt := now.Add(-2 * time.Minute)

	cases := []struct {
		name string
		task domaindelivery.ExecutionTask
		want bool
	}{
		{
			name: "non running status is never expired",
			task: domaindelivery.ExecutionTask{
				Status:         "completed",
				CreatedAt:      now.Add(-10 * time.Minute),
				TimeoutSeconds: 60,
			},
		},
		{
			name: "last heartbeat is preferred",
			task: domaindelivery.ExecutionTask{
				Status:          "running",
				CreatedAt:       now.Add(-10 * time.Minute),
				StartedAt:       &startedAt,
				LastHeartbeatAt: &lastHeartbeatAt,
				TimeoutSeconds:  300,
			},
		},
		{
			name: "started time is used without heartbeat",
			task: domaindelivery.ExecutionTask{
				Status:         "dispatching",
				CreatedAt:      now.Add(-10 * time.Minute),
				StartedAt:      &startedAt,
				TimeoutSeconds: 60,
			},
			want: true,
		},
		{
			name: "created time is fallback",
			task: domaindelivery.ExecutionTask{
				Status:         "running",
				CreatedAt:      now.Add(-10 * time.Minute),
				TimeoutSeconds: 60,
			},
			want: true,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := taskHeartbeatExpired(tt.task, now); got != tt.want {
				t.Fatalf("taskHeartbeatExpired() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRecordCallbackIgnoresLateTerminalCallback(t *testing.T) {
	repo := newExecutionRepoFake()
	now := time.Date(2026, 6, 4, 10, 30, 0, 0, time.UTC)
	repo.tasks["task-1"] = domaindelivery.ExecutionTask{
		ID:            "task-1",
		ApplicationID: "app-1",
		TaskKind:      "build",
		ProviderKind:  "ci_agent_runner",
		Status:        "canceled",
		CallbackToken: "callback-token",
		Result:        map[string]any{},
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	service := New(repo, nil, nil, nil, "", "", "", "", 0, "", nil)

	updated, err := service.RecordCallback(context.Background(), domaindelivery.ExecutionCallbackInput{
		CallbackToken: " callback-token ",
		Status:        "completed",
		Payload:       map[string]any{"logs": []any{"should not be replayed"}},
	})
	if err != nil {
		t.Fatalf("RecordCallback() error = %v", err)
	}
	if updated.Status != "canceled" {
		t.Fatalf("late callback changed task status to %q", updated.Status)
	}
	if len(repo.callbacks) != 1 {
		t.Fatalf("callbacks recorded = %d, want 1", len(repo.callbacks))
	}
	if len(repo.updates) != 0 {
		t.Fatalf("late terminal callback updated task %d times", len(repo.updates))
	}
	if !repo.hasLogContaining("ignored late callback") {
		t.Fatalf("late callback warning log was not recorded")
	}
}

func TestRecordCallbackRefreshesHeartbeatAndPersistsLogs(t *testing.T) {
	repo := newExecutionRepoFake()
	now := time.Date(2026, 6, 4, 10, 30, 0, 0, time.UTC)
	repo.tasks["task-1"] = domaindelivery.ExecutionTask{
		ID:            "task-1",
		ApplicationID: "app-1",
		TaskKind:      "build",
		ProviderKind:  "ci_agent_runner",
		Status:        "queued",
		CallbackToken: "callback-token",
		Result:        map[string]any{},
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	service := New(repo, nil, nil, nil, "", "", "", "", 0, "", nil)

	updated, err := service.RecordCallback(context.Background(), domaindelivery.ExecutionCallbackInput{
		CallbackToken: " callback-token ",
		Status:        "running",
		Payload: map[string]any{
			"logs": []any{"checkout started", "build started"},
		},
	})
	if err != nil {
		t.Fatalf("RecordCallback() error = %v", err)
	}
	if updated.Status != "running" {
		t.Fatalf("status = %q, want running", updated.Status)
	}
	if updated.OperationState == nil || updated.OperationState.Phase != "running" || !updated.OperationState.Cancelable {
		t.Fatalf("operation state was not attached to running callback: %#v", updated.OperationState)
	}
	if updated.StartedAt == nil {
		t.Fatalf("started timestamp was not set")
	}
	if updated.LastHeartbeatAt == nil {
		t.Fatalf("last heartbeat timestamp was not set")
	}
	if len(repo.callbacks) != 1 {
		t.Fatalf("callbacks recorded = %d, want 1", len(repo.callbacks))
	}
	if !repo.hasLogContaining("checkout started") || !repo.hasLogContaining("build started") {
		t.Fatalf("callback logs were not persisted: %#v", repo.logs)
	}
}

func TestRecordCallbackCompletesBuildAndBackfillsBundleAndBuildRecord(t *testing.T) {
	repo := newExecutionRepoFake()
	builds := newBuildRecordRepoFake()
	now := time.Date(2026, 6, 4, 10, 30, 0, 0, time.UTC)
	repo.bundles["bundle-1"] = domaindelivery.ReleaseBundle{
		ID:            "bundle-1",
		ApplicationID: "app-1",
		Version:       "v1",
		Status:        "building",
		Metadata:      map[string]any{},
		CreatedAt:     now.Add(-time.Minute),
		UpdatedAt:     now.Add(-time.Minute),
	}
	repo.tasks["task-1"] = domaindelivery.ExecutionTask{
		ID:              "task-1",
		ReleaseBundleID: "bundle-1",
		ApplicationID:   "app-1",
		TaskKind:        "build",
		ProviderKind:    "ci_agent_runner",
		Status:          "running",
		CallbackToken:   "callback-token",
		Payload:         map[string]any{},
		Result:          map[string]any{},
		Artifacts: []domaindelivery.ExecutionArtifact{
			{ID: "artifact-1", Kind: "image", Name: "app", Ref: "registry.example/app:v1", Digest: "sha256:abc"},
		},
		CreatedAt: now.Add(-time.Minute),
		UpdatedAt: now.Add(-time.Minute),
	}
	builds.records["task-1"] = domainbuild.Record{
		ID:            "build-1",
		ApplicationID: "app-1",
		Status:        "running",
		Metadata:      map[string]any{},
		CreatedAt:     now.Add(-time.Minute),
	}
	service := New(repo, builds, nil, nil, "", "", "", "", 0, "", nil)

	updated, err := service.RecordCallback(context.Background(), domaindelivery.ExecutionCallbackInput{
		CallbackToken: "callback-token",
		Status:        "completed",
		Payload: map[string]any{
			"image":       "registry.example/app:v1",
			"imageDigest": "sha256:abc",
			"logs":        []any{"build complete"},
		},
	})
	if err != nil {
		t.Fatalf("RecordCallback() error = %v", err)
	}
	if updated.Status != "completed" || updated.FinishedAt == nil {
		t.Fatalf("build task not completed: status=%q finished=%v", updated.Status, updated.FinishedAt)
	}
	bundle := repo.bundles["bundle-1"]
	if bundle.Status != "ready" {
		t.Fatalf("bundle status = %q, want ready", bundle.Status)
	}
	if bundle.ArtifactRef != "registry.example/app:v1" || bundle.ArtifactDigest != "sha256:abc" {
		t.Fatalf("bundle artifact = %q/%q", bundle.ArtifactRef, bundle.ArtifactDigest)
	}
	build := builds.records["task-1"]
	if build.Status != "completed" {
		t.Fatalf("build status = %q, want completed", build.Status)
	}
	if build.FinishedAt == nil {
		t.Fatalf("build finished timestamp was not backfilled")
	}
	if build.Metadata["executionTaskStatus"] != "completed" {
		t.Fatalf("build metadata executionTaskStatus = %#v", build.Metadata["executionTaskStatus"])
	}
	if len(repo.artifacts) != 1 {
		t.Fatalf("artifacts persisted = %d, want 1", len(repo.artifacts))
	}
	if repo.artifacts[0].ReleaseBundleID != "bundle-1" || repo.artifacts[0].ApplicationID != "app-1" {
		t.Fatalf("artifact ownership was not populated: %#v", repo.artifacts[0])
	}
}

func TestRecordCallbackCompletesReleaseAndBackfillsReleaseRecord(t *testing.T) {
	repo := newExecutionRepoFake()
	releases := newReleaseRecordRepoFake()
	now := time.Date(2026, 6, 4, 10, 30, 0, 0, time.UTC)
	repo.bundles["bundle-1"] = domaindelivery.ReleaseBundle{
		ID:            "bundle-1",
		ApplicationID: "app-1",
		Version:       "v1",
		Status:        "releasing",
		Metadata:      map[string]any{},
		CreatedAt:     now.Add(-time.Minute),
		UpdatedAt:     now.Add(-time.Minute),
	}
	repo.tasks["task-1"] = domaindelivery.ExecutionTask{
		ID:              "task-1",
		ReleaseBundleID: "bundle-1",
		ApplicationID:   "app-1",
		TaskKind:        "release",
		ProviderKind:    "ci_agent_runner",
		Status:          "running",
		CallbackToken:   "callback-token",
		Result:          map[string]any{},
		CreatedAt:       now.Add(-time.Minute),
		UpdatedAt:       now.Add(-time.Minute),
	}
	releases.records["task-1"] = domainrelease.Record{
		ID:            "release-1",
		ApplicationID: "app-1",
		Status:        "running",
		Metadata:      map[string]any{},
		CreatedAt:     now.Add(-time.Minute),
	}
	service := New(repo, nil, releases, nil, "", "", "", "", 0, "", nil)

	updated, err := service.RecordCallback(context.Background(), domaindelivery.ExecutionCallbackInput{
		CallbackToken: "callback-token",
		Status:        "completed",
		Payload:       map[string]any{"releaseName": "prod"},
	})
	if err != nil {
		t.Fatalf("RecordCallback() error = %v", err)
	}
	if updated.Status != "completed" {
		t.Fatalf("release task status = %q, want completed", updated.Status)
	}
	release := releases.records["task-1"]
	if release.Status != "deployed" {
		t.Fatalf("release record status = %q, want deployed", release.Status)
	}
	if release.DeployedAt == nil {
		t.Fatalf("release deployed timestamp was not backfilled")
	}
	if release.Metadata["executionTaskStatus"] != "completed" {
		t.Fatalf("release metadata executionTaskStatus = %#v", release.Metadata["executionTaskStatus"])
	}
}

func TestCancelExecutionTaskBackfillsBuildAndRejectsLateCallback(t *testing.T) {
	repo := newExecutionRepoFake()
	builds := newBuildRecordRepoFake()
	now := time.Date(2026, 6, 4, 10, 30, 0, 0, time.UTC)
	repo.bundles["bundle-1"] = domaindelivery.ReleaseBundle{
		ID:            "bundle-1",
		ApplicationID: "app-1",
		Version:       "v1",
		Status:        "building",
		Metadata:      map[string]any{},
		CreatedAt:     now.Add(-time.Minute),
		UpdatedAt:     now.Add(-time.Minute),
	}
	repo.tasks["task-1"] = domaindelivery.ExecutionTask{
		ID:              "task-1",
		ReleaseBundleID: "bundle-1",
		ApplicationID:   "app-1",
		TaskKind:        "build",
		ProviderKind:    "ci_agent_runner",
		Status:          "running",
		CallbackToken:   "callback-token",
		Result:          map[string]any{},
		CreatedAt:       now.Add(-time.Minute),
		UpdatedAt:       now.Add(-time.Minute),
	}
	builds.records["task-1"] = domainbuild.Record{
		ID:            "build-1",
		ApplicationID: "app-1",
		Status:        "running",
		Metadata:      map[string]any{},
		CreatedAt:     now.Add(-time.Minute),
	}
	service := New(repo, builds, nil, nil, "", "", "", "", 0, "", nil)

	canceled, err := service.CancelExecutionTask(context.Background(), " task-1 ", domaindelivery.ExecutionTaskActionInput{Reason: "operator stopped it"})
	if err != nil {
		t.Fatalf("CancelExecutionTask() error = %v", err)
	}
	if canceled.Status != "canceled" {
		t.Fatalf("status = %q, want canceled", canceled.Status)
	}
	if canceled.OperationState == nil || !canceled.OperationState.Terminal || !canceled.OperationState.Retryable || canceled.OperationState.FailureMessage != "operator stopped it" {
		t.Fatalf("unexpected canceled operation state: %#v", canceled.OperationState)
	}
	if repo.bundles["bundle-1"].Status != "failed" {
		t.Fatalf("bundle status = %q, want failed", repo.bundles["bundle-1"].Status)
	}
	if builds.records["task-1"].Status != "failed" {
		t.Fatalf("build status = %q, want failed", builds.records["task-1"].Status)
	}
	updatesAfterCancel := len(repo.updates)

	late, err := service.RecordCallback(context.Background(), domaindelivery.ExecutionCallbackInput{
		CallbackToken: "callback-token",
		Status:        "completed",
		Payload:       map[string]any{"logs": []any{"late completion"}},
	})
	if err != nil {
		t.Fatalf("RecordCallback() late error = %v", err)
	}
	if late.Status != "canceled" {
		t.Fatalf("late callback returned status = %q, want canceled", late.Status)
	}
	if len(repo.updates) != updatesAfterCancel {
		t.Fatalf("late callback updated terminal task: updates before=%d after=%d", updatesAfterCancel, len(repo.updates))
	}
	if !repo.hasLogContaining("ignored late callback") {
		t.Fatalf("late callback warning was not recorded")
	}
}

func TestRecoverStaleTasksTimesOutAndBackfillsRelease(t *testing.T) {
	repo := newExecutionRepoFake()
	releases := newReleaseRecordRepoFake()
	now := time.Date(2026, 6, 4, 10, 30, 0, 0, time.UTC)
	startedAt := now.Add(-10 * time.Minute)
	lastHeartbeatAt := now.Add(-9 * time.Minute)
	repo.bundles["bundle-1"] = domaindelivery.ReleaseBundle{
		ID:            "bundle-1",
		ApplicationID: "app-1",
		Version:       "v1",
		Status:        "releasing",
		Metadata:      map[string]any{},
		CreatedAt:     now.Add(-10 * time.Minute),
		UpdatedAt:     now.Add(-10 * time.Minute),
	}
	repo.tasks["task-1"] = domaindelivery.ExecutionTask{
		ID:              "task-1",
		ReleaseBundleID: "bundle-1",
		ApplicationID:   "app-1",
		TaskKind:        "release",
		ProviderKind:    "ci_agent_runner",
		Status:          "running",
		CallbackToken:   "callback-token",
		Result:          map[string]any{},
		StartedAt:       &startedAt,
		LastHeartbeatAt: &lastHeartbeatAt,
		TimeoutSeconds:  60,
		CreatedAt:       now.Add(-10 * time.Minute),
		UpdatedAt:       now.Add(-9 * time.Minute),
	}
	releases.records["task-1"] = domainrelease.Record{
		ID:            "release-1",
		ApplicationID: "app-1",
		Status:        "running",
		Metadata:      map[string]any{},
		CreatedAt:     now.Add(-time.Minute),
	}
	service := New(repo, nil, releases, nil, "", "", "", "", 0, "", nil)

	if err := service.recoverStaleTasks(context.Background(), now); err != nil {
		t.Fatalf("recoverStaleTasks() error = %v", err)
	}
	task := repo.tasks["task-1"]
	if task.Status != "callback_timeout" {
		t.Fatalf("task status = %q, want callback_timeout", task.Status)
	}
	if task.FinishedAt == nil {
		t.Fatalf("timeout did not set finished timestamp")
	}
	if task.OperationState != nil {
		t.Fatalf("stored repository task should not keep derived operation state: %#v", task.OperationState)
	}
	if repo.bundles["bundle-1"].Status != "failed" {
		t.Fatalf("bundle status = %q, want failed", repo.bundles["bundle-1"].Status)
	}
	release := releases.records["task-1"]
	if release.Status != "failed" {
		t.Fatalf("release record status = %q, want failed", release.Status)
	}
	if release.Metadata["executionTaskStatus"] != "callback_timeout" {
		t.Fatalf("release metadata executionTaskStatus = %#v", release.Metadata["executionTaskStatus"])
	}
	if !repo.hasLogContaining("timed out") {
		t.Fatalf("timeout log was not recorded")
	}
}

func TestListAndGetExecutionTasksAttachOperationState(t *testing.T) {
	repo := newExecutionRepoFake()
	now := time.Date(2026, 6, 4, 10, 30, 0, 0, time.UTC)
	repo.tasks["task-1"] = domaindelivery.ExecutionTask{
		ID:             "task-1",
		ApplicationID:  "app-1",
		TaskKind:       "build",
		ProviderKind:   "ci_agent_runner",
		Status:         "failed",
		TimeoutSeconds: 300,
		Result:         map[string]any{"error": "build failed"},
		CreatedAt:      now.Add(-time.Minute),
		UpdatedAt:      now,
	}
	permissions := appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {appaccess.PermDeliveryWorkflowsView},
		},
	})
	service := New(repo, nil, nil, nil, "", "", "", "", 0, "", permissions)
	principal := domainidentity.Principal{Roles: []string{"developer"}}

	listed, err := service.ListExecutionTasks(context.Background(), principal, domaindelivery.ExecutionTaskFilter{})
	if err != nil {
		t.Fatalf("ListExecutionTasks() error = %v", err)
	}
	if len(listed) != 1 || listed[0].OperationState == nil || listed[0].OperationState.Phase != "failed" || !listed[0].OperationState.Retryable {
		t.Fatalf("listed task missing operation state: %#v", listed)
	}

	got, err := service.GetExecutionTask(context.Background(), principal, "task-1")
	if err != nil {
		t.Fatalf("GetExecutionTask() error = %v", err)
	}
	if got.OperationState == nil || got.OperationState.FailureMessage != "build failed" {
		t.Fatalf("got task missing failure operation state: %#v", got.OperationState)
	}
}

func TestRetryExecutionTaskRotatesCallbackTokenAndQueuesRunnerTask(t *testing.T) {
	repo := newExecutionRepoFake()
	now := time.Date(2026, 6, 4, 10, 30, 0, 0, time.UTC)
	finishedAt := now.Add(-time.Minute)
	repo.tasks["task-1"] = domaindelivery.ExecutionTask{
		ID:             "task-1",
		ApplicationID:  "app-1",
		TaskKind:       "build",
		ProviderKind:   "ci_agent_runner",
		Status:         "failed",
		MaxRetries:     1,
		AttemptCount:   1,
		TimeoutSeconds: 300,
		CallbackToken:  "old-token",
		Result:         map[string]any{"error": "old failure"},
		FinishedAt:     &finishedAt,
		CreatedAt:      now.Add(-10 * time.Minute),
		UpdatedAt:      now.Add(-time.Minute),
	}
	service := New(repo, nil, nil, nil, "", "", "", "", 0, "", nil)

	updated, err := service.RetryExecutionTask(context.Background(), " task-1 ", domaindelivery.ExecutionTaskActionInput{Reason: "manual retry"})
	if err != nil {
		t.Fatalf("RetryExecutionTask() error = %v", err)
	}
	if updated.Status != "queued" {
		t.Fatalf("status = %q, want queued", updated.Status)
	}
	if updated.CallbackToken == "" || updated.CallbackToken == "old-token" {
		t.Fatalf("callback token was not rotated: %q", updated.CallbackToken)
	}
	if updated.StartedAt != nil || updated.LastHeartbeatAt != nil || updated.FinishedAt != nil {
		t.Fatalf("retry did not clear runtime timestamps: started=%v heartbeat=%v finished=%v", updated.StartedAt, updated.LastHeartbeatAt, updated.FinishedAt)
	}
	if updated.MaxRetries < 2 {
		t.Fatalf("max retries = %d, want at least 2", updated.MaxRetries)
	}
	if !repo.hasLogContaining("execution task re-queued") {
		t.Fatalf("retry log was not recorded")
	}
	if !repo.hasLogContaining("waiting for runner claim") {
		t.Fatalf("runner queue log was not recorded")
	}
}

func TestStartBuildExecutionMarksDisabledK8sJobRunnerFailed(t *testing.T) {
	repo := newExecutionRepoFake()
	service := New(repo, nil, nil, nil, "", "", "", "", 0, "", nil)

	bundle, task, err := service.StartBuildExecution(context.Background(), BuildPlan{
		ApplicationID:            "app-1",
		ApplicationEnvironmentID: "env-1",
		Version:                  "v1",
		ProviderKind:             "k8s_job_runner",
		Metadata: map[string]any{
			"commands": []string{"echo build"},
		},
	})
	if err != nil {
		t.Fatalf("StartBuildExecution() error = %v", err)
	}
	if task.Status != "failed" {
		t.Fatalf("task status = %q, want failed", task.Status)
	}
	if task.Result["failureReason"] != "provider_disabled" || task.Result["providerDisabled"] != true {
		t.Fatalf("task disabled metadata = %#v", task.Result)
	}
	if !strings.Contains(fmt.Sprint(task.Result["error"]), "Kubernetes cluster manager is not configured") {
		t.Fatalf("task error = %#v", task.Result["error"])
	}
	if repo.bundles[bundle.ID].Status != "failed" {
		t.Fatalf("bundle status = %q, want failed", repo.bundles[bundle.ID].Status)
	}
	if !repo.hasLogContaining("provider is disabled") {
		t.Fatalf("provider disabled log was not recorded: %#v", repo.logs)
	}
}

type executionRepoFake struct {
	tasks     map[string]domaindelivery.ExecutionTask
	bundles   map[string]domaindelivery.ReleaseBundle
	artifacts []domaindelivery.ExecutionArtifact
	logs      []domaindelivery.ExecutionLog
	callbacks []domaindelivery.ExecutionCallback
	updates   []domaindelivery.ExecutionTask
}

func newExecutionRepoFake() *executionRepoFake {
	return &executionRepoFake{
		tasks:   map[string]domaindelivery.ExecutionTask{},
		bundles: map[string]domaindelivery.ReleaseBundle{},
	}
}

func (r *executionRepoFake) ListReleaseBundles(context.Context, domaindelivery.ReleaseBundleFilter) ([]domaindelivery.ReleaseBundle, error) {
	items := make([]domaindelivery.ReleaseBundle, 0, len(r.bundles))
	for _, item := range r.bundles {
		items = append(items, item)
	}
	return items, nil
}

func (r *executionRepoFake) GetReleaseBundle(_ context.Context, id string) (domaindelivery.ReleaseBundle, error) {
	item, ok := r.bundles[strings.TrimSpace(id)]
	if !ok {
		return domaindelivery.ReleaseBundle{}, fmt.Errorf("release bundle not found")
	}
	return item, nil
}

func (r *executionRepoFake) CreateReleaseBundle(_ context.Context, item domaindelivery.ReleaseBundle) (domaindelivery.ReleaseBundle, error) {
	r.bundles[item.ID] = item
	return item, nil
}

func (r *executionRepoFake) UpdateReleaseBundle(_ context.Context, item domaindelivery.ReleaseBundle) (domaindelivery.ReleaseBundle, error) {
	r.bundles[item.ID] = item
	return item, nil
}

func (r *executionRepoFake) ListExecutionTasks(_ context.Context, filter domaindelivery.ExecutionTaskFilter) ([]domaindelivery.ExecutionTask, error) {
	items := make([]domaindelivery.ExecutionTask, 0, len(r.tasks))
	for _, item := range r.tasks {
		if filter.Status != "" && item.Status != filter.Status {
			continue
		}
		items = append(items, item)
	}
	return items, nil
}

func (r *executionRepoFake) GetExecutionTask(_ context.Context, id string) (domaindelivery.ExecutionTask, error) {
	item, ok := r.tasks[strings.TrimSpace(id)]
	if !ok {
		return domaindelivery.ExecutionTask{}, fmt.Errorf("execution task not found")
	}
	return item, nil
}

func (r *executionRepoFake) GetExecutionTaskByCallbackToken(_ context.Context, token string) (domaindelivery.ExecutionTask, error) {
	token = strings.TrimSpace(token)
	for _, item := range r.tasks {
		if item.CallbackToken == token {
			return item, nil
		}
	}
	return domaindelivery.ExecutionTask{}, fmt.Errorf("execution task callback token not found")
}

func (r *executionRepoFake) ClaimExecutionTask(context.Context, []string, string, string) (domaindelivery.ExecutionTask, error) {
	return domaindelivery.ExecutionTask{}, fmt.Errorf("not implemented")
}

func (r *executionRepoFake) CreateExecutionTask(_ context.Context, item domaindelivery.ExecutionTask) (domaindelivery.ExecutionTask, error) {
	r.tasks[item.ID] = item
	return item, nil
}

func (r *executionRepoFake) UpdateExecutionTask(_ context.Context, item domaindelivery.ExecutionTask) (domaindelivery.ExecutionTask, error) {
	r.tasks[item.ID] = item
	r.updates = append(r.updates, item)
	return item, nil
}

func (r *executionRepoFake) ListExecutionLogs(context.Context, string, int) ([]domaindelivery.ExecutionLog, error) {
	return append([]domaindelivery.ExecutionLog(nil), r.logs...), nil
}

func (r *executionRepoFake) CreateExecutionLog(_ context.Context, item domaindelivery.ExecutionLog) error {
	r.logs = append(r.logs, item)
	return nil
}

func (r *executionRepoFake) CreateExecutionCallback(_ context.Context, item domaindelivery.ExecutionCallback) error {
	r.callbacks = append(r.callbacks, item)
	return nil
}

func (r *executionRepoFake) ListExecutionArtifacts(context.Context, string) ([]domaindelivery.ExecutionArtifact, error) {
	return append([]domaindelivery.ExecutionArtifact(nil), r.artifacts...), nil
}

func (r *executionRepoFake) ListExecutionArtifactsByBundle(context.Context, string) ([]domaindelivery.ExecutionArtifact, error) {
	return append([]domaindelivery.ExecutionArtifact(nil), r.artifacts...), nil
}

func (r *executionRepoFake) UpsertExecutionArtifact(_ context.Context, item domaindelivery.ExecutionArtifact) (domaindelivery.ExecutionArtifact, error) {
	for index := range r.artifacts {
		if r.artifacts[index].ID == item.ID {
			r.artifacts[index] = item
			return item, nil
		}
	}
	r.artifacts = append(r.artifacts, item)
	return item, nil
}

func (r *executionRepoFake) ListApprovalPolicies(context.Context) ([]domaindelivery.ApprovalPolicy, error) {
	return nil, nil
}

func (r *executionRepoFake) GetApprovalPolicy(context.Context, string) (domaindelivery.ApprovalPolicy, error) {
	return domaindelivery.ApprovalPolicy{}, fmt.Errorf("approval policy not found")
}

func (r *executionRepoFake) CreateApprovalPolicy(_ context.Context, input domaindelivery.ApprovalPolicyInput) (domaindelivery.ApprovalPolicy, error) {
	return domaindelivery.ApprovalPolicy{ID: input.ID, Key: input.Key, Name: input.Name}, nil
}

func (r *executionRepoFake) UpdateApprovalPolicy(_ context.Context, id string, input domaindelivery.ApprovalPolicyInput) (domaindelivery.ApprovalPolicy, error) {
	return domaindelivery.ApprovalPolicy{ID: strings.TrimSpace(id), Key: input.Key, Name: input.Name}, nil
}

func (r *executionRepoFake) DeleteApprovalPolicy(context.Context, string) error {
	return nil
}

func (r *executionRepoFake) ListDeliveryBlueprints(context.Context) ([]domaindelivery.DeliveryBlueprint, error) {
	return nil, nil
}

func (r *executionRepoFake) GetDeliveryBlueprint(context.Context, string) (domaindelivery.DeliveryBlueprint, error) {
	return domaindelivery.DeliveryBlueprint{}, fmt.Errorf("delivery blueprint not found")
}

func (r *executionRepoFake) CreateDeliveryBlueprint(_ context.Context, input domaindelivery.DeliveryBlueprintInput) (domaindelivery.DeliveryBlueprint, error) {
	return domaindelivery.DeliveryBlueprint{ID: input.ID, Key: input.Key, Name: input.Name}, nil
}

func (r *executionRepoFake) UpdateDeliveryBlueprint(_ context.Context, id string, input domaindelivery.DeliveryBlueprintInput) (domaindelivery.DeliveryBlueprint, error) {
	return domaindelivery.DeliveryBlueprint{ID: strings.TrimSpace(id), Key: input.Key, Name: input.Name}, nil
}

func (r *executionRepoFake) hasLogContaining(fragment string) bool {
	for _, item := range r.logs {
		if strings.Contains(item.Message, fragment) {
			return true
		}
	}
	return false
}

type buildRecordRepoFake struct {
	records map[string]domainbuild.Record
	updates []domainbuild.Record
}

func newBuildRecordRepoFake() *buildRecordRepoFake {
	return &buildRecordRepoFake{records: map[string]domainbuild.Record{}}
}

func (r *buildRecordRepoFake) GetByExecutionTaskID(_ context.Context, taskID string) (domainbuild.Record, error) {
	item, ok := r.records[strings.TrimSpace(taskID)]
	if !ok {
		return domainbuild.Record{}, fmt.Errorf("build record not found")
	}
	return item, nil
}

func (r *buildRecordRepoFake) Update(_ context.Context, item domainbuild.Record) (domainbuild.Record, error) {
	for taskID, record := range r.records {
		if record.ID == item.ID {
			r.records[taskID] = item
			break
		}
	}
	r.updates = append(r.updates, item)
	return item, nil
}

type releaseRecordRepoFake struct {
	records map[string]domainrelease.Record
	updates []domainrelease.Record
}

func newReleaseRecordRepoFake() *releaseRecordRepoFake {
	return &releaseRecordRepoFake{records: map[string]domainrelease.Record{}}
}

func (r *releaseRecordRepoFake) GetByExecutionTaskID(_ context.Context, taskID string) (domainrelease.Record, error) {
	item, ok := r.records[strings.TrimSpace(taskID)]
	if !ok {
		return domainrelease.Record{}, fmt.Errorf("release record not found")
	}
	return item, nil
}

func (r *releaseRecordRepoFake) Update(_ context.Context, item domainrelease.Record) (domainrelease.Record, error) {
	for taskID, record := range r.records {
		if record.ID == item.ID {
			r.records[taskID] = item
			break
		}
	}
	r.updates = append(r.updates, item)
	return item, nil
}

type stubRolePermissionReader struct {
	matrix map[string][]string
}

func (s stubRolePermissionReader) ListRolePermissions(context.Context) (map[string][]string, error) {
	return s.matrix, nil
}
