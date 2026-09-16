package execution

import (
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domainbuild "github.com/opensoha/soha/internal/domain/build"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	"github.com/opensoha/soha/internal/infrastructure/config"
	dbstore "github.com/opensoha/soha/internal/infrastructure/db"
	repoapp "github.com/opensoha/soha/internal/repository/application"
	repodelivery "github.com/opensoha/soha/internal/repository/delivery"
	"go.uber.org/zap"
)

type externalPipelineFixture struct {
	created, release chan struct{}
	posts            atomic.Int32
	stopRequests     atomic.Int32
	inspection       domainbuild.PipelineInspection
	verifyErr        error
	resolveErr       error
	verifiedDigest   string
}

func (p *externalPipelineFixture) ExternalPipelineProvider(context.Context, sohaapi.ExternalPipelineExecutionSpec) (domainbuild.PipelineProvider, func(), error) {
	return p, func() {}, p.resolveErr
}
func (p *externalPipelineFixture) ResolvePipelineTag(context.Context, string, string) (string, error) {
	return strings.Repeat("b", 40), nil
}
func (p *externalPipelineFixture) StartPipeline(context.Context, domainbuild.PipelineRequest) (sohaapi.ExternalPipelineRun, error) {
	p.posts.Add(1)
	if p.created != nil {
		close(p.created)
		<-p.release
	}
	return sohaapi.ExternalPipelineRun{Status: "dispatch_unknown"}, context.DeadlineExceeded
}
func (p *externalPipelineFixture) FindPipeline(context.Context, domainbuild.PipelineRequest) (sohaapi.ExternalPipelineRun, error) {
	return sohaapi.ExternalPipelineRun{RunID: "101", Status: "running"}, nil
}
func (p *externalPipelineFixture) InspectPipeline(_ context.Context, _ domainbuild.PipelineRequest, id string, stop bool) (domainbuild.PipelineInspection, error) {
	if id != "101" {
		return domainbuild.PipelineInspection{}, errors.New("wrong recovered pipeline")
	}
	if stop {
		p.stopRequests.Add(1)
	}
	return p.inspection, nil
}
func (p *externalPipelineFixture) VerifyBuildImage(_ context.Context, id, image, digest string) error {
	if id != "registry" || image != "registry.example/team/app:v1" {
		return errors.New("unfrozen registry/image")
	}
	p.verifiedDigest = digest
	return p.verifyErr
}

func externalPipelinePostgres(t *testing.T) (*repodelivery.Repository, domaindelivery.ExecutionTask) {
	t.Helper()
	portText := os.Getenv("SOHA_CATALOG_TEST_POSTGRES_PORT")
	if portText == "" {
		t.Skip("requires an isolated PostgreSQL instance")
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	store, err := dbstore.New(config.DatabaseConfig{Driver: "postgres", Host: "127.0.0.1", Port: port, Name: "soha", User: "pgsql", Password: "test-only", SSLMode: "disable", MaxOpenConns: 6, MaxIdleConns: 4}, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.MigrateFromFile(t.Context(), filepath.Join("..", "..", "..", "migrations", "postgres")); err != nil {
		t.Fatal(err)
	}
	appID := uuid.NewString()
	if _, err := repoapp.New(store.DB()).Create(t.Context(), domainapp.UpsertInput{ID: appID, Key: appID, Name: "External CI check", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	task := domaindelivery.ExecutionTask{ID: uuid.NewString(), ApplicationID: appID, TaskKind: "build", ProviderKind: domainbuild.ExternalPipelineProvider, Status: "queued", CallbackToken: uuid.NewString(), TimeoutSeconds: 300, CreatedAt: now, UpdatedAt: now, Payload: map[string]any{"image": "registry.example/team/app:v1", "externalPipeline": sohaapi.ExternalPipelineExecutionSpec{SourceConnectionID: "gitlab", ProviderProjectID: "42", SourceCommit: strings.Repeat("a", 40), PipelineCommit: strings.Repeat("b", 40), Configuration: sohaapi.ExternalPipelineConfiguration{Provider: sohaapi.ExternalPipelineGitLab, PipelineTag: "soha-v1", ArtifactJob: "publish", RegistryID: "registry"}}}, Result: map[string]any{}}
	repo := repodelivery.New(store.DB())
	if _, err := repo.CreateExecutionTask(t.Context(), task); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = store.DB().Exec(`DELETE FROM execution_tasks WHERE id = ?`, task.ID).Error
		_ = store.DB().Exec(`DELETE FROM applications WHERE id = ?`, appID).Error
	})
	return repo, task
}

func pipelineFixtureService(repo *repodelivery.Repository, provider *externalPipelineFixture) *Service {
	s := New(repo, nil, nil, nil, "", "", "", "", 30, "", nil)
	s.SetExternalPipeline(provider, provider)
	return s
}

func TestExternalPipelineConcurrentDispatchCancelAndRecoveryWithPostgres(t *testing.T) {
	repo, task := externalPipelinePostgres(t)
	provider := &externalPipelineFixture{created: make(chan struct{}), release: make(chan struct{}), inspection: domainbuild.PipelineInspection{Run: sohaapi.ExternalPipelineRun{RunID: "101", Status: "canceling"}}}
	service := pipelineFixtureService(repo, provider)
	results := make(chan error, 12)
	for range 12 {
		go func() { _, err := service.dispatchExternalPipeline(t.Context(), task); results <- err }()
	}
	select {
	case <-provider.created:
	case <-time.After(10 * time.Second):
		t.Fatal("dispatch did not begin")
	}
	stored, err := repo.GetExecutionTask(t.Context(), task.ID)
	if err != nil || stored.Status != "dispatching" || stored.AttemptCount != 1 || stored.MaxRetries != 0 {
		t.Fatalf("external POST preceded durable intent: %+v %v", stored, err)
	}
	stopping, err := service.CancelExecutionTask(t.Context(), task.ID, domaindelivery.ExecutionTaskActionInput{Reason: "user"})
	close(provider.release)
	for range 12 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if err != nil || stopping.Status != "canceling" || stopping.FinishedAt != nil || provider.posts.Load() != 1 {
		t.Fatalf("cancel/dispatch race changed state: %+v %v posts=%d", stopping, err, provider.posts.Load())
	}
	assertExternalPipelineRecovery(t, repo, service, task, provider)
}

func assertExternalPipelineRecovery(t *testing.T, repo *repodelivery.Repository, service *Service, task domaindelivery.ExecutionTask, provider *externalPipelineFixture) {
	t.Helper()
	stored, err := repo.GetExecutionTask(t.Context(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	run, _ := externalPipelineRun(stored)
	if run.RunID != "101" || stored.Status != "canceling" {
		t.Fatal("late unknown response erased recovered identity", stored)
	}
	forged := stored
	forged.Result = mergeMaps(maps.Clone(stored.Result), map[string]any{"externalPipeline": sohaapi.ExternalPipelineRun{RunID: "another", Status: "running"}})
	if _, err := repo.UpdateExecutionTask(t.Context(), forged); err == nil {
		t.Fatal("replaced durable pipeline identity")
	}
	forged.Result = maps.Clone(stored.Result)
	forged.Status = "completed"
	if _, err := repo.UpdateExecutionTask(t.Context(), forged); err == nil {
		t.Fatal("accepted terminal state without stop confirmation")
	}
	provider.resolveErr = errors.New("connection revoked")
	stored, err = service.reconcileExternalPipeline(t.Context(), stored, time.Now())
	run, _ = externalPipelineRun(stored)
	if err != nil || stored.Status != "canceling" || stored.FinishedAt != nil || run.RunID != "101" || run.StopConfirmed || stored.Result["externalPipelineError"] == nil {
		t.Fatal("lost connection hid the reconciliation error or declared stop", stored, err)
	}
	provider.resolveErr = nil
	provider.inspection.Run.StopConfirmed = true
	provider.inspection.Run.Status = "canceled"
	restarted := pipelineFixtureService(repo, provider)
	stopped, err := restarted.reconcileExternalPipeline(t.Context(), stored, time.Now())
	if err != nil || stopped.Status != "canceled" || stopped.FinishedAt == nil || provider.posts.Load() != 1 || provider.stopRequests.Load() < 2 {
		t.Fatalf("restart did not confirm the original remote stop: %+v %v", stopped, err)
	}
	if _, won, err := repo.BeginExternalPipelineDispatch(t.Context(), task.ID); err != nil || won {
		t.Fatal("restarted terminal task redispatched", err)
	}
}

func TestExternalPipelineArtifactAndTimeoutWithPostgres(t *testing.T) {
	for _, mode := range []string{"verified", "invalid-image", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			repo, task := externalPipelinePostgres(t)
			provider := &externalPipelineFixture{inspection: domainbuild.PipelineInspection{Run: sohaapi.ExternalPipelineRun{RunID: "101", Status: "success", StopConfirmed: true, ArtifactJobID: "9", ArtifactDigest: "sha256:" + strings.Repeat("c", 64)}, ImageDigest: "sha256:" + strings.Repeat("d", 64)}}
			if mode == "invalid-image" {
				provider.verifyErr = errors.New("manifest bytes differ")
			}
			service := pipelineFixtureService(repo, provider)
			unknown, err := service.dispatchExternalPipeline(t.Context(), task)
			if err != nil || unknown.Status != "dispatching" {
				t.Fatal("ambiguous dispatch not retained", unknown, err)
			}
			if mode == "timeout" {
				started := time.Now().Add(-10 * time.Minute)
				unknown.StartedAt = &started
				unknown, err = repo.UpdateExecutionTask(t.Context(), unknown)
				if err != nil {
					t.Fatal(err)
				}
			}
			restarted := pipelineFixtureService(repo, provider)
			finished, err := restarted.reconcileExternalPipeline(t.Context(), unknown, time.Now())
			want := "failed"
			if mode == "verified" {
				want = "completed"
			}
			if err != nil || finished.Status != want || finished.FinishedAt == nil || provider.posts.Load() != 1 {
				t.Fatalf("unexpected final result %s: %+v %v", mode, finished, err)
			}
			if mode == "timeout" {
				if finished.Result["cancelReason"] != executionTimeoutReason || provider.verifiedDigest != "" {
					t.Fatal("timeout was accepted as artifact success")
				}
			} else if provider.verifiedDigest != provider.inspection.ImageDigest {
				t.Fatal("artifact was accepted without registry verification")
			}
		})
	}
}
