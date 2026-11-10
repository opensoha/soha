package docker_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	dockerruntime "github.com/opensoha/soha-contracts/docker/runtime"
	appaccess "github.com/opensoha/soha/internal/application/access"
	appdocker "github.com/opensoha/soha/internal/application/docker"
	domain "github.com/opensoha/soha/internal/domain/docker"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	db "github.com/opensoha/soha/internal/infrastructure/db"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
	dockerrepo "github.com/opensoha/soha/internal/repository/docker"
)

func TestDockerRuntimeIdentityWithPostgres(t *testing.T) {
	store := projectConfigurationStore(t)
	repo := dockerrepo.New(store.DB())
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	must := func(t *testing.T, err error) { dockerRuntimeIdentityWithPostgresPhase1(t, err) }
	newProject := func(t *testing.T, version int) (domain.Host, domain.Project) {
		return dockerRuntimeIdentityWithPostgresPhase2(t, version, &repo, &ctx, &must)
	}
	queued := func(t *testing.T, h domain.Host, p domain.Project, binding any) domain.Operation {
		return dockerRuntimeIdentityWithPostgresPhase3(t, h, p, binding, &repo, &ctx, &must)
	}
	t.Run("legacy_default_and_explicit_opt_in", func(t *testing.T) { dockerRuntimeIdentityWithPostgresPhase4(t, &newProject) })
	t.Run("service_opt_in_and_disable_preserves_existing", func(t *testing.T) { dockerRuntimeIdentityWithPostgresPhase5(t, &repo, &ctx, &must, &newProject) })

	t.Run("version_update_rejected", func(t *testing.T) {
		dockerRuntimeIdentityWithPostgresPhase6(t, &store, &repo, &ctx, &must, &newProject)
	})
	t.Run("queued_project_freezes_registered_container_set", func(t *testing.T) {
		dockerRuntimeIdentityWithPostgresPhase7(t, &repo, &ctx, &must, &newProject, &queued)
	})
	t.Run("legacy_runner_cannot_claim_bound", func(t *testing.T) {
		assertBoundRuntimeClaimDenied(t, ctx, repo.ClaimOperation, "legacy", must, newProject, queued, "legacy runner received bound operation")
	})
	t.Run("capable_claim_preserves_original_binding", func(t *testing.T) {
		dockerRuntimeIdentityWithPostgresPhase9(t, &store, &ctx, &must, &newProject, &queued)
	})
	t.Run("unknown_protocol_is_not_claimed", func(t *testing.T) {
		dockerRuntimeIdentityWithPostgresPhase10(t, &store, &repo, &ctx, &must, &newProject, &queued)
	})
	t.Run("callback_fence_required", func(t *testing.T) {
		assertBoundRuntimeClaimDenied(t, ctx, repo.ClaimIdentityOperation, "new", must, newProject, queued, "unfenced runner claimed bound operation")
	})
	t.Run("atomic_container_start_binds_database_uids", func(t *testing.T) { dockerRuntimeIdentityWithPostgresPhase12(t, &repo, &ctx, &must) })
	t.Run("callback_transaction_rollback_and_recreation_fence", func(t *testing.T) {
		dockerRuntimeIdentityWithPostgresPhase13(t, &store, &repo, &ctx, &must, &newProject)
	})
	t.Run("confirmed_inventory_removal_and_atomic_callback", func(t *testing.T) {
		dockerRuntimeIdentityWithPostgresPhase14(t, &repo, &ctx, &must, &newProject, &queued)
	})

}

func dockerRuntimeIdentityWithPostgresPhase1(t *testing.T, err error) {

	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func dockerRuntimeIdentityWithPostgresPhase2(t *testing.T, version int, sharedRepo **dockerrepo.Repository, sharedCtx *context.Context, sharedMust *func(t *testing.T, err error)) (domain.Host, domain.Project) {

	t.Helper()
	id := uuid.NewString()
	h, err := (*sharedRepo).CreateHost((*sharedCtx), domain.HostInput{ID: uuid.NewString(), Name: "runtime-owned"})
	(*sharedMust)(t, err)
	p, err := (*sharedRepo).CreateProject((*sharedCtx), domain.ProjectInput{ID: id, HostID: h.ID, Name: "runtime-owned", Slug: "same-slug", RuntimeIdentityVersion: version, ComposeContent: "services:\n  web:\n    image: alpine:3.21\n"})
	(*sharedMust)(t, err)
	return h, p
}

func dockerRuntimeIdentityWithPostgresPhase3(t *testing.T, h domain.Host, p domain.Project, binding any, sharedRepo **dockerrepo.Repository, sharedCtx *context.Context, sharedMust *func(t *testing.T, err error)) domain.Operation {

	t.Helper()
	o, err := (*sharedRepo).CreateOperation((*sharedCtx), domain.OperationInput{ID: uuid.NewString(), HostID: h.ID, ProjectID: p.ID, OperationKind: "project_deploy", Status: "queued", Payload: map[string]any{"action": "deploy", "runtimeBinding": binding}})
	(*sharedMust)(t, err)
	return o
}

func dockerRuntimeIdentityWithPostgresPhase4(t *testing.T, sharedNewProject *func(t *testing.T, version int) (domain.Host, domain.Project)) {

	_, legacy := (*sharedNewProject)(t, 0)
	_, bound := (*sharedNewProject)(t, 1)
	if legacy.RuntimeIdentityVersion != 0 || bound.RuntimeIdentityVersion != 1 || legacy.ResourceUID == bound.ResourceUID {
		t.Fatal("runtime version or UID not preserved")
	}
}

func dockerRuntimeIdentityWithPostgresPhase5(t *testing.T, sharedRepo **dockerrepo.Repository, sharedCtx *context.Context, sharedMust *func(t *testing.T, err error), sharedNewProject *func(t *testing.T, version int) (domain.Host, domain.Project)) {

	h, _ := (*sharedNewProject)(t, 0)
	actor := domainidentity.Principal{UserID: "owned-runtime-identity", Roles: []string{"fixture"}}
	input := domain.ProjectInput{HostID: h.ID, ID: uuid.NewString(), Name: "bound", Slug: "bound", ComposeContent: "services:\n  web:\n    image: alpine:3.21\n"}
	enabled := appdocker.New((*sharedRepo), appaccess.NewPermissionResolver(projectConfigurationRoles{}), nil, appdocker.WithRuntimeIdentityV1(true))
	bound, err := enabled.CreateProject((*sharedCtx), actor, input)
	(*sharedMust)(t, err)
	if bound.RuntimeIdentityVersion != 1 {
		t.Fatal("explicit server policy did not select protocol")
	}
	disabled := appdocker.New((*sharedRepo), appaccess.NewPermissionResolver(projectConfigurationRoles{}), nil)
	input.ID = uuid.NewString()
	input.Slug = "legacy"
	input.RuntimeIdentityVersion = 1
	legacy, err := disabled.CreateProject((*sharedCtx), actor, input)
	(*sharedMust)(t, err)
	if legacy.RuntimeIdentityVersion != 0 {
		t.Fatal("caller input bypassed server policy")
	}
	current, err := (*sharedRepo).GetProject((*sharedCtx), bound.ID)
	(*sharedMust)(t, err)
	if current.RuntimeIdentityVersion != 1 {
		t.Fatal("disabling policy downgraded existing project")
	}
}

func dockerRuntimeIdentityWithPostgresPhase6(t *testing.T, sharedStore **db.Store, sharedRepo **dockerrepo.Repository, sharedCtx *context.Context, sharedMust *func(t *testing.T, err error), sharedNewProject *func(t *testing.T, version int) (domain.Host, domain.Project)) {

	_, p := (*sharedNewProject)(t, 0)
	if (*sharedStore).DB().WithContext((*sharedCtx)).Exec("UPDATE docker_projects SET runtime_identity_version=1 WHERE id=?", p.ID).Error == nil {
		t.Fatal("implicit adoption accepted")
	}
	p, err := (*sharedRepo).GetProject((*sharedCtx), p.ID)
	(*sharedMust)(t, err)
	if p.RuntimeIdentityVersion != 0 {
		t.Fatal("failed update persisted")
	}
}

func dockerRuntimeIdentityWithPostgresPhase7(t *testing.T, sharedRepo **dockerrepo.Repository, sharedCtx *context.Context, sharedMust *func(t *testing.T, err error), sharedNewProject *func(t *testing.T, version int) (domain.Host, domain.Project), sharedQueued *func(t *testing.T, h domain.Host, p domain.Project, binding any) domain.Operation) {

	h, p := (*sharedNewProject)(t, 1)
	binding, err := dockerruntime.NewProjectBinding(h.ResourceUID, p.ResourceUID)
	(*sharedMust)(t, err)
	current, err := (*sharedRepo).UpsertService((*sharedCtx), domain.ServiceInput{ProjectID: p.ID, HostID: h.ID, Name: "web", ContainerID: strings.Repeat("a", 64), Status: "running"})
	(*sharedMust)(t, err)
	original := (*sharedQueued)(t, h, p, binding)
	frozen, err := dockerruntime.ContainerSetFromValue(original.Payload["runtimeContainers"], binding)
	(*sharedMust)(t, err)
	if len(frozen) != 1 || frozen[0].ContainerID != current.ContainerID {
		t.Fatal("original full IDs not frozen")
	}
	_, err = (*sharedRepo).UpsertService((*sharedCtx), domain.ServiceInput{ProjectID: p.ID, HostID: h.ID, Name: current.Name, ContainerID: strings.Repeat("b", 64), Status: "running"})
	(*sharedMust)(t, err)
	_, err = (*sharedRepo).CreateOperation((*sharedCtx), domain.OperationInput{HostID: h.ID, ProjectID: p.ID, OperationKind: "project_deploy", Payload: map[string]any{"runtimeBinding": binding, "runtimeContainers": frozen}})
	if !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("original snapshot rebound at enqueue", err)
	}
	_, err = (*sharedRepo).CreateOperation((*sharedCtx), domain.OperationInput{HostID: h.ID, ProjectID: p.ID, OperationKind: "project_deploy", Payload: map[string]any{"action": "down"}})
	if !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("bound project accepted a legacy runtime operation", err)
	}
}

func dockerRuntimeIdentityWithPostgresPhase9(t *testing.T, sharedStore **db.Store, sharedCtx *context.Context, sharedMust *func(t *testing.T, err error), sharedNewProject *func(t *testing.T, version int) (domain.Host, domain.Project), sharedQueued *func(t *testing.T, h domain.Host, p domain.Project, binding any) domain.Operation) {

	h, p := (*sharedNewProject)(t, 1)
	b, err := dockerruntime.NewProjectBinding(h.ResourceUID, p.ResourceUID)
	(*sharedMust)(t, err)
	original := (*sharedQueued)(t, h, p, b)
	item, err := dockerrepo.New((*sharedStore).DB()).ClaimIdentityOperation((*sharedCtx), "new", "agent", []string{h.ID}, nil, "test-owned-callback-token", time.Now())
	(*sharedMust)(t, err)
	got, err := dockerruntime.ProjectBindingFromValue(item.Payload["runtimeBinding"])
	(*sharedMust)(t, err)
	if item.ID != original.ID || got != b || item.CallbackToken == "" {
		t.Fatal("claim changed original runtime identity")
	}
}

func dockerRuntimeIdentityWithPostgresPhase10(t *testing.T, sharedStore **db.Store, sharedRepo **dockerrepo.Repository, sharedCtx *context.Context, sharedMust *func(t *testing.T, err error), sharedNewProject *func(t *testing.T, version int) (domain.Host, domain.Project), sharedQueued *func(t *testing.T, h domain.Host, p domain.Project, binding any) domain.Operation) {

	h, p := (*sharedNewProject)(t, 1)
	b, err := dockerruntime.NewProjectBinding(h.ResourceUID, p.ResourceUID)
	(*sharedMust)(t, err)
	item := (*sharedQueued)(t, h, p, b)
	// Simulate a future writer without weakening current enqueue validation.
	(*sharedMust)(t, (*sharedStore).DB().WithContext((*sharedCtx)).Exec(`UPDATE docker_operations SET payload=jsonb_set(payload,'{runtimeBinding,protocol}','"future"'::jsonb) WHERE id=?`, item.ID).Error)
	_, err = (*sharedRepo).ClaimIdentityOperation((*sharedCtx), "new", "agent", []string{h.ID}, nil, "test-owned-callback-token", time.Now())
	if !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatal("future protocol downgraded", err)
	}
}

func dockerRuntimeIdentityWithPostgresPhase12(t *testing.T, sharedRepo **dockerrepo.Repository, sharedCtx *context.Context, sharedMust *func(t *testing.T, err error)) {

	h, err := (*sharedRepo).CreateHost((*sharedCtx), domain.HostInput{ID: uuid.NewString(), Name: "runtime-container-owned"})
	(*sharedMust)(t, err)
	compose := "services:\n  web:\n    image: alpine:3.21\n"
	result, err := (*sharedRepo).CreateContainerStart((*sharedCtx), domain.ContainerStartCreateInput{Project: domain.ProjectInput{ID: uuid.NewString(), HostID: h.ID, Name: "runtime-container-owned", Slug: "same-slug", ComposeContent: compose, RuntimeIdentityVersion: 1}, Service: domain.ServiceInput{Name: "web", HostID: h.ID, Status: "defined"}, PortMappings: []domain.PortMappingInput{{HostID: h.ID, Name: "http", HostPort: 32123, ContainerPort: 80, Protocol: "tcp"}}, Operation: domain.OperationInput{HostID: h.ID, OperationKind: "container_start", Status: "queued", Payload: map[string]any{"composeContent": compose, "action": "deploy"}}})
	(*sharedMust)(t, err)
	binding, err := dockerruntime.ProjectBindingFromValue(result.Operation.Payload["runtimeBinding"])
	(*sharedMust)(t, err)
	if binding.ProjectUID != result.Project.ResourceUID || binding.HostUID != h.ResourceUID || result.Operation.Payload["composeContent"] != compose {
		t.Fatal("atomic creation missed database runtime identity")
	}
}

func dockerRuntimeIdentityWithPostgresPhase13(t *testing.T, sharedStore **db.Store, sharedRepo **dockerrepo.Repository, sharedCtx *context.Context, sharedMust *func(t *testing.T, err error), sharedNewProject *func(t *testing.T, version int) (domain.Host, domain.Project)) {

	h, p := (*sharedNewProject)(t, 1)
	rollback := errors.New("owned callback rollback")
	err := (*sharedRepo).WithinRuntimeIdentity((*sharedCtx), h.ID, p.ID, h.ResourceUID, p.ResourceUID, func(txCtx context.Context) error {
		(*sharedMust)(t, dbtx.DB(txCtx, (*sharedStore).DB()).Exec("UPDATE docker_projects SET status='running' WHERE id=?", p.ID).Error)
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	current, err := (*sharedRepo).GetProject((*sharedCtx), p.ID)
	(*sharedMust)(t, err)
	if current.Status != p.Status {
		t.Fatal("callback transaction partially committed")
	}
	(*sharedMust)(t, (*sharedRepo).DeleteProject((*sharedCtx), p.ID))
	_, err = (*sharedRepo).CreateProject((*sharedCtx), domain.ProjectInput{ID: p.ID, HostID: h.ID, Name: p.Name, Slug: p.Slug, RuntimeIdentityVersion: 1, ComposeContent: p.ComposeContent})
	(*sharedMust)(t, err)
	called := false
	err = (*sharedRepo).WithinRuntimeIdentity((*sharedCtx), h.ID, p.ID, h.ResourceUID, p.ResourceUID, func(context.Context) error { called = true; return nil })
	if !errors.Is(err, apperrors.ErrConflict) || called {
		t.Fatal("old callback crossed immutable UID fence", err)
	}
}

func dockerRuntimeIdentityWithPostgresPhase14(t *testing.T, sharedRepo **dockerrepo.Repository, sharedCtx *context.Context, sharedMust *func(t *testing.T, err error), sharedNewProject *func(t *testing.T, version int) (domain.Host, domain.Project), sharedQueued *func(t *testing.T, h domain.Host, p domain.Project, binding any) domain.Operation) {

	h, p := (*sharedNewProject)(t, 1)
	binding, err := dockerruntime.NewProjectBinding(h.ResourceUID, p.ResourceUID)
	(*sharedMust)(t, err)
	original, err := (*sharedRepo).UpsertService((*sharedCtx), domain.ServiceInput{ProjectID: p.ID, HostID: h.ID, Name: "web", ContainerID: strings.Repeat("a", 64), Status: "running", Image: "fixture", Config: map[string]any{"definition": "retain"}})
	(*sharedMust)(t, err)
	otherHost, otherProject := (*sharedNewProject)(t, 1)
	other, err := (*sharedRepo).UpsertService((*sharedCtx), domain.ServiceInput{ProjectID: otherProject.ID, HostID: otherHost.ID, Name: "web", ContainerID: strings.Repeat("b", 64), Status: "running"})
	(*sharedMust)(t, err)
	operation := (*sharedQueued)(t, h, p, binding)
	operation.Payload["action"] = "down"
	_, err = (*sharedRepo).UpdateOperation((*sharedCtx), operation)
	(*sharedMust)(t, err)
	claimed, err := (*sharedRepo).ClaimIdentityOperation((*sharedCtx), "owned-inventory", "agent", []string{h.ID}, nil, "owned-inventory-token", time.Now())
	(*sharedMust)(t, err)
	service := appdocker.New((*sharedRepo), appaccess.NewPermissionResolver(projectConfigurationRoles{}), nil)
	input := domain.OperationCallbackInput{OperationID: claimed.ID, WorkerID: "owned-inventory", CallbackToken: claimed.CallbackToken, Status: "completed", Payload: map[string]any{"runtimeInventoryConfirmed": true}}
	_, err = service.RecordOperationCallback((*sharedCtx), input)
	if !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatal("missing complete inventory accepted", err)
	}
	currentOperation, err := (*sharedRepo).GetOperation((*sharedCtx), operation.ID)
	(*sharedMust)(t, err)
	currentHost, err := (*sharedRepo).GetHost((*sharedCtx), h.ID)
	(*sharedMust)(t, err)
	currentProject, err := (*sharedRepo).GetProject((*sharedCtx), p.ID)
	(*sharedMust)(t, err)
	if currentOperation.Status != "running" || currentHost.Status != h.Status || currentProject.Status != p.Status {
		t.Fatal("invalid callback partially committed operation/host/project state")
	}
	input.Payload["services"] = []any{}
	_, err = service.RecordOperationCallback((*sharedCtx), input)
	(*sharedMust)(t, err)
	removed, err := (*sharedRepo).GetService((*sharedCtx), original.ID)
	(*sharedMust)(t, err)
	if removed.Status != "removed" || removed.ContainerID != "" || removed.Config["definition"] != "retain" || removed.Image != original.Image {
		t.Fatal("confirmed empty inventory lost definition or retained stale runtime", removed)
	}
	unrelated, err := (*sharedRepo).GetService((*sharedCtx), other.ID)
	(*sharedMust)(t, err)
	if unrelated.Status != "running" || unrelated.ContainerID != other.ContainerID {
		t.Fatal("inventory reconciliation touched another project")
	}
	input.Payload["services"] = []any{map[string]any{"name": "web", "containerId": strings.Repeat("c", 64), "status": "running"}}
	_, err = service.RecordOperationCallback((*sharedCtx), input)
	(*sharedMust)(t, err)
	duplicate, err := (*sharedRepo).GetService((*sharedCtx), original.ID)
	(*sharedMust)(t, err)
	if duplicate.ContainerID != "" {
		t.Fatal("terminal duplicate callback re-adopted inventory")
	}
	(*sharedMust)(t, (*sharedRepo).WithinRuntimeIdentity((*sharedCtx), h.ID, p.ID, h.ResourceUID, p.ResourceUID, func(txCtx context.Context) error {
		return (*sharedRepo).ReconcileRuntimeServices(txCtx, h.ID, p.ID, []domain.ServiceInput{{HostID: h.ID, ProjectID: p.ID, Name: original.Name, ContainerID: strings.Repeat("d", 64), Status: "running"}})
	}))
	observed, err := (*sharedRepo).GetService((*sharedCtx), original.ID)
	(*sharedMust)(t, err)
	if observed.Config["definition"] != "retain" || observed.ContainerID != strings.Repeat("d", 64) {
		t.Fatal("confirmed present inventory erased configuration", observed)
	}
}

func assertBoundRuntimeClaimDenied(t *testing.T, ctx context.Context, claim func(context.Context, string, string, []string, []string, string, time.Time) (domain.Operation, error), worker string, must func(*testing.T, error), newProject func(*testing.T, int) (domain.Host, domain.Project), queued func(*testing.T, domain.Host, domain.Project, any) domain.Operation, message string) {
	t.Helper()
	h, p := newProject(t, 1)
	binding, err := dockerruntime.NewProjectBinding(h.ResourceUID, p.ResourceUID)
	must(t, err)
	queued(t, h, p, binding)
	_, err = claim(ctx, worker, "agent", []string{h.ID}, nil, "", time.Now())
	if !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatal(message, err)
	}
}
