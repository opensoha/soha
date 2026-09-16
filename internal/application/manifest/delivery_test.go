package manifest

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type snapshotRepository struct {
	DeclarativeRepository
	ManifestTaskRuntime
	base       *testRepository
	binding    domainmanifest.EnvironmentBinding
	deployment domainmanifest.Deployment
	tasks      map[string]domaindelivery.ExecutionTask
	keys       map[string]string
	setCount   int
	queueError error
}

func (r *snapshotRepository) GetBinding(context.Context, string) (domainmanifest.EnvironmentBinding, error) {
	return r.binding, nil
}
func (r *snapshotRepository) ListBindings(context.Context, string) ([]domainmanifest.EnvironmentBinding, error) {
	return []domainmanifest.EnvironmentBinding{r.binding}, nil
}
func (r *snapshotRepository) GetSource(context.Context, string) (domainmanifest.Source, error) {
	return r.base.source, nil
}
func (r *snapshotRepository) GetRevisionSourceCommit(context.Context, string, int) (string, error) {
	return "commit-a", nil
}
func (r *snapshotRepository) GetDeploymentByBinding(context.Context, string) (domainmanifest.Deployment, error) {
	if r.deployment.ID == "" {
		return domainmanifest.Deployment{}, apperrors.ErrNotFound
	}
	return r.deployment, nil
}
func (r *snapshotRepository) SetDesiredRevision(_ context.Context, next domainmanifest.Deployment, expected int64) (domainmanifest.Deployment, error) {
	if r.deployment.Generation != expected {
		return domainmanifest.Deployment{}, apperrors.ErrConflict
	}
	r.setCount++
	next.Generation = expected + 1
	r.deployment = next
	return next, nil
}
func (r *snapshotRepository) CreateOperationTask(_ context.Context, run domainmanifest.OperationRun, task domaindelivery.ExecutionTask) (string, bool, error) {
	if r.queueError != nil {
		return "", false, r.queueError
	}
	if id, ok := r.keys[run.IdempotencyKey]; ok {
		return id, false, nil
	}
	r.keys[run.IdempotencyKey] = task.ID
	r.tasks[task.ID] = task
	return task.ID, true, nil
}
func (r *snapshotRepository) GetExecutionTaskInternal(_ context.Context, id string) (domaindelivery.ExecutionTask, error) {
	return r.tasks[id], nil
}

type snapshotRenderer struct{ calls int }

func (r *snapshotRenderer) Render(_ context.Context, item domainmanifest.Package, binding domainmanifest.EnvironmentBinding, files []domainmanifest.File, revision int) (domainmanifest.RenderResult, error) {
	r.calls++
	digest, err := domainmanifest.RenderInputDigest(item.Renderer, binding, files)
	return domainmanifest.RenderResult{PackageID: item.ID, BindingID: binding.ID, Revision: revision, InputDigest: digest, RendererVersion: "test/v1", RenderedDigest: "rendered",
		Documents: []domainmanifest.RenderedDocument{{APIVersion: "v1", Kind: "ConfigMap", Namespace: binding.Namespace, Name: "settings", Content: `{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"settings","namespace":"payments"}}`, ContentDigest: "content"}}}, err
}

func newSnapshotTestService() (*DeclarativeService, *snapshotRepository, *snapshotRenderer, domaincatalog.ReleaseTarget) {
	base := &testRepository{item: domainmanifest.Package{ID: "package", ApplicationID: "payments", Renderer: domainmanifest.RendererRaw, CurrentRevision: 1, UpdatedAt: time.Now().UTC()}, revisions: []domainmanifest.Revision{{Version: 1, Digest: "revision", Files: []domainmanifest.File{{Path: "config.yaml", Content: "fixed"}}}}}
	repo := &snapshotRepository{base: base, binding: domainmanifest.EnvironmentBinding{ID: "manifest-binding", PackageID: "package", ApplicationEnvironmentID: "payments-dev", ClusterID: "dev-1", Namespace: "payments", Enabled: true, Version: 1}, tasks: map[string]domaindelivery.ExecutionTask{}, keys: map[string]string{}}
	renderer := &snapshotRenderer{}
	service := NewDeclarative(newTestService(base, testAuthorizer{}), repo, DeclarativeRuntimeDependencies{Renderer: renderer, Tasks: repo})
	return service, repo, renderer, domaincatalog.ReleaseTarget{ID: "target", ExecutorKind: "manifest_ssa", ConfigRef: repo.binding.ID, ClusterID: repo.binding.ClusterID, Namespace: repo.binding.Namespace, Enabled: true}
}

func approveSnapshotPreflight(repo *snapshotRepository, snapshot domainmanifest.DeliverySnapshot) {
	task := repo.tasks[snapshot.PreflightTaskID]
	task.Status = "completed"
	task.Result = structMap(domainmanifest.TaskResult{Preflight: &domainmanifest.PreflightResult{Ready: true, RenderedDigest: snapshot.RenderedDigest}})
	repo.tasks[task.ID] = task
}

func TestDeliverySnapshotPreflightApplyRetryAndObservationUseSameDocuments(t *testing.T) {
	service, repo, renderer, target := newSnapshotTestService()
	ctx := context.Background()
	snapshot, err := service.CreateDeliverySnapshot(ctx, testPrincipal(), "payments", "payments-dev", target, 0, "plan", domainmanifest.DeliveryArtifacts{})
	requireSnapshotTestNoError(t, err)
	if snapshot.SourceCommit != "commit-a" || snapshot.Revision != 1 || repo.setCount != 0 || len(repo.tasks) != 1 {
		t.Fatalf("unexpected snapshot: %#v", snapshot)
	}
	if err := service.ValidateDeliverySnapshot(ctx, testPrincipal(), snapshot); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("pending preflight accepted: %v", err)
	}
	approveSnapshotPreflight(repo, snapshot)
	repo.queueError = errors.New("queue unavailable")
	if _, _, err := service.ApplyDeliverySnapshot(ctx, testPrincipal(), snapshot); err == nil {
		t.Fatal("queue failure ignored")
	}
	repo.queueError = nil
	deployment, task, err := service.ApplyDeliverySnapshot(ctx, testPrincipal(), snapshot)
	requireSnapshotTestNoError(t, err)
	payload, _ := decodeTaskPayload(task.Payload)
	preflight, _ := decodeTaskPayload(repo.tasks[snapshot.PreflightTaskID].Payload)
	if repo.setCount != 1 || renderer.calls != 1 || !reflect.DeepEqual(payload.Documents, preflight.Documents) || payload.RenderedDigest != preflight.RenderedDigest || payload.FieldManager != preflight.FieldManager {
		t.Fatalf("snapshot changed or selected twice: %#v", payload)
	}
	_, repeated, err := service.ApplyDeliverySnapshot(ctx, testPrincipal(), snapshot)
	requireSnapshotTestNoError(t, err)
	if repeated.ID != task.ID || len(repo.tasks) != 2 {
		t.Fatalf("retry duplicated task: %s %v", repeated.ID, err)
	}
	repo.binding.Overlay = map[string]string{"changed": "after approval"}
	rendered, err := service.renderDeployment(ctx, repo.base.item, repo.binding, deployment)
	if err != nil || renderer.calls != 1 || !reflect.DeepEqual(rendered.Documents, snapshot.Documents) {
		t.Fatalf("observation re-rendered: %v", err)
	}
	if err := service.promoteRevision(ctx, repo.base.item, 2, "sync"); err != nil || repo.setCount != 1 {
		t.Fatalf("Git promotion overwrote planned deployment: %v", err)
	}
	if _, err := service.SetDesiredRevision(ctx, testPrincipal(), repo.binding.ID, domainmanifest.DesiredRevisionInput{DesiredRevision: 1, ExpectedGeneration: 1}); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("legacy writer accepted: %v", err)
	}
}

func TestDeliverySnapshotRejectsChangedInputAndFailedPreflight(t *testing.T) {
	for name, change := range map[string]func(*snapshotRepository){
		"binding-version": func(r *snapshotRepository) { r.binding.Version++ },
		"namespace":       func(r *snapshotRepository) { r.binding.Namespace = "outside" },
		"package":         func(r *snapshotRepository) { r.base.item.UpdatedAt = r.base.item.UpdatedAt.Add(time.Second) },
		"files":           func(r *snapshotRepository) { r.base.revisions[0].Files[0].Content = "changed" },
		"generation":      func(r *snapshotRepository) { r.deployment = domainmanifest.Deployment{ID: "other", Generation: 3} },
		"preflight": func(r *snapshotRepository) {
			for id, task := range r.tasks {
				task.Status = "failed"
				r.tasks[id] = task
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			service, repo, _, target := newSnapshotTestService()
			snapshot, err := service.CreateDeliverySnapshot(context.Background(), testPrincipal(), "payments", "payments-dev", target, 1, "plan", domainmanifest.DeliveryArtifacts{})
			requireSnapshotTestNoError(t, err)
			approveSnapshotPreflight(repo, snapshot)
			change(repo)
			if _, _, err := service.ApplyDeliverySnapshot(context.Background(), testPrincipal(), snapshot); !errors.Is(err, apperrors.ErrConflict) || repo.setCount != 0 {
				t.Fatalf("changed plan applied: %v", err)
			}
		})
	}
}

func requireSnapshotTestNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
