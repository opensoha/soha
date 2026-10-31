package docker_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	appdocker "github.com/opensoha/soha/internal/application/docker"
	domaindocker "github.com/opensoha/soha/internal/domain/docker"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	db "github.com/opensoha/soha/internal/infrastructure/db"
	"github.com/opensoha/soha/internal/platform/apperrors"
	dockerrepo "github.com/opensoha/soha/internal/repository/docker"
)

type conditionalConfigurationRoles struct{ denied atomic.Bool }

func (r *conditionalConfigurationRoles) ListRolePermissions(context.Context) (map[string][]string, error) {
	keys := []string{"docker.hosts.create", "docker.hosts.update", "docker.hosts.delete", "docker.projects.update", "docker.projects.delete"}
	if r.denied.Load() {
		keys = nil
	}
	return map[string][]string{"conditional": keys}, nil
}

func TestDockerConditionalConfigurationWithPostgres(t *testing.T) {
	store := projectConfigurationStore(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	repo := dockerrepo.New(store.DB())
	roles := &conditionalConfigurationRoles{}
	newService := func() *appdocker.Service { return appdocker.New(repo, appaccess.NewPermissionResolver(roles), nil) }
	service := newService()
	actor := domainidentity.Principal{UserID: "conditional-fixture", Roles: []string{"conditional"}}
	must := func(tb *testing.T, err error) { dockerConditionalConfigurationWithPostgresPhase1(tb, err) }
	host := func(tb *testing.T) domaindocker.Host {
		return dockerConditionalConfigurationWithPostgresPhase2(tb, &ctx, &repo, &must)
	}
	project := func(tb *testing.T) domaindocker.Project {
		return dockerConditionalConfigurationWithPostgresPhase3(tb, &ctx, &repo, &must, &host)
	}
	conditions := func(uid, revision string) sohaapi.DockerConfigurationPreconditions {
		return sohaapi.DockerConfigurationPreconditions{ExpectedUID: uuid.MustParse(uid), ExpectedConfigurationRevision: uuid.MustParse(revision)}
	}
	patch := func(uid, revision string) sohaapi.DockerConfigurationPatchInput {
		return dockerConditionalConfigurationWithPostgresPhase4(uid, revision, &conditions)
	}
	noReceipt := func(tb *testing.T, key string) {
		dockerConditionalConfigurationWithPostgresPhase5(tb, key, &ctx, &repo, &actor)
	}
	t.Run("concurrent_cas_one_winner", func(t *testing.T) {
		dockerConditionalConfigurationWithPostgresPhase6(t, &ctx, &service, &actor, &host, &patch)
	})
	t.Run("restart_replay_and_changed_input", func(t *testing.T) {
		dockerConditionalConfigurationWithPostgresPhase7(t, &ctx, &newService, &service, &actor, &must, &host, &patch)
	})
	t.Run("legacy_writer_and_aba_conflict", func(t *testing.T) {
		dockerConditionalConfigurationWithPostgresPhase8(t, &store, &ctx, &repo, &service, &actor, &must, &host, &patch, &noReceipt)
	})
	t.Run("partial_preserves_runtime_precision_and_expiry", func(t *testing.T) {
		dockerConditionalConfigurationWithPostgresPhase9(t, &store, &ctx, &repo, &service, &actor, &must, &project, &patch)
	})
	t.Run("explicit_zero_empty_and_large_object", func(t *testing.T) {
		dockerConditionalConfigurationWithPostgresPhase10(t, &store, &ctx, &repo, &service, &actor, &must, &project, &patch)
	})
	t.Run("host_move_uid_and_scope_fences", func(t *testing.T) {
		dockerConditionalConfigurationWithPostgresPhase11(t, &ctx, &repo, &service, &actor, &must, &host, &project, &patch, &noReceipt)
	})
	t.Run("definition_failure_rolls_back_receipt_and_revision", func(t *testing.T) {
		dockerConditionalConfigurationWithPostgresPhase12(t, &store, &ctx, &repo, &service, &actor, &must, &project, &patch, &noReceipt)
	})
	t.Run("delete_tombstone_survives_recreation", func(t *testing.T) {
		dockerConditionalConfigurationWithPostgresPhase13(t, &ctx, &repo, &roles, &newService, &service, &actor, &must, &host, &conditions)
	})
	t.Run("children_and_runtime_history_block_deregistration", func(t *testing.T) {
		dockerConditionalConfigurationWithPostgresPhase14(t, &ctx, &repo, &service, &actor, &must, &project, &conditions, &noReceipt)
	})
	t.Run("registration_is_metadata_only_and_replayable", func(t *testing.T) {
		dockerConditionalConfigurationWithPostgresPhase15(t, &store, &ctx, &newService, &service, &actor, &must)
	})
	t.Run("key_ceiling_denies_without_mutation", func(t *testing.T) {
		dockerConditionalConfigurationWithPostgresPhase16(t, &ctx, &repo, &service, &actor, &must, &host, &patch, &noReceipt)
	})
	t.Run("receipt_failure_rolls_back_deletion", func(t *testing.T) {
		dockerConditionalConfigurationWithPostgresPhase17(t, &store, &ctx, &repo, &service, &actor, &must, &host, &conditions, &noReceipt)
	})
	t.Run("reviewed_deploy_configuration_fence", func(t *testing.T) {
		reviewedDeployWithPostgres(t, store)
	})
}

func dockerConditionalConfigurationWithPostgresPhase1(tb *testing.T, err error) {

	tb.Helper()
	if err != nil {
		tb.Fatal(err)
	}
}

func dockerConditionalConfigurationWithPostgresPhase2(tb *testing.T, sharedCtx *context.Context, sharedRepo **dockerrepo.Repository, sharedMust *func(tb *testing.T, err error)) domaindocker.Host {

	tb.Helper()
	h, err := (*sharedRepo).CreateHost((*sharedCtx), domaindocker.HostInput{ID: uuid.NewString(), Name: "conditional-host"})
	(*sharedMust)(tb, err)
	return h
}

func dockerConditionalConfigurationWithPostgresPhase3(tb *testing.T, sharedCtx *context.Context, sharedRepo **dockerrepo.Repository, sharedMust *func(tb *testing.T, err error), sharedHost *func(tb *testing.T) domaindocker.Host) domaindocker.Project {

	tb.Helper()
	h := (*sharedHost)(tb)
	p, err := (*sharedRepo).CreateProjectConfiguration((*sharedCtx), domaindocker.ProjectInput{ID: uuid.NewString(), HostID: h.ID, Name: "conditional-project", Slug: "fixture", TTLSeconds: 1200, ComposeContent: "services:\n  api: {}\n"}, []string{"api"})
	(*sharedMust)(tb, err)
	return p
}

func dockerConditionalConfigurationWithPostgresPhase4(uid, revision string, sharedConditions *func(uid string, revision string) sohaapi.DockerConfigurationPreconditions) sohaapi.DockerConfigurationPatchInput {

	value := "renamed"
	return sohaapi.DockerConfigurationPatchInput{IdempotencyKey: uuid.NewString(), Preconditions: (*sharedConditions)(uid, revision), Changes: []sohaapi.DockerConfigurationChange{{Field: "name", StringValue: &value}}}
}

func dockerConditionalConfigurationWithPostgresPhase5(tb *testing.T, key string, sharedCtx *context.Context, sharedRepo **dockerrepo.Repository, sharedActor *domainidentity.Principal) {

	tb.Helper()
	if _, err := (*sharedRepo).GetConfigurationRequest((*sharedCtx), (*sharedActor).UserID, key); !errors.Is(err, apperrors.ErrNotFound) {
		tb.Fatalf("failed request persisted: %v", err)
	}
}

func dockerConditionalConfigurationWithPostgresPhase6(t *testing.T, sharedCtx *context.Context, sharedService **appdocker.Service, sharedActor *domainidentity.Principal, sharedHost *func(tb *testing.T) domaindocker.Host, sharedPatch *func(uid string, revision string) sohaapi.DockerConfigurationPatchInput) {

	h := (*sharedHost)(t)
	var success, conflict atomic.Int32
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			_, err := (*sharedService).PatchConfiguration((*sharedCtx), (*sharedActor), "docker.host", h.ID, (*sharedPatch)(h.ResourceUID, h.ConfigurationRevision))
			switch {
			case err == nil:
				success.Add(1)
			case errors.Is(err, apperrors.ErrConflict):
				conflict.Add(1)
			default:
				t.Errorf("unexpected concurrent result: %v", err)
			}
		})
	}
	wg.Wait()
	if success.Load() != 1 || conflict.Load() != 7 {
		t.Fatalf("CAS outcomes %d/%d", success.Load(), conflict.Load())
	}
}

func dockerConditionalConfigurationWithPostgresPhase7(t *testing.T, sharedCtx *context.Context, sharedNewService *func() *appdocker.Service, sharedService **appdocker.Service, sharedActor *domainidentity.Principal, sharedMust *func(tb *testing.T, err error), sharedHost *func(tb *testing.T) domaindocker.Host, sharedPatch *func(uid string, revision string) sohaapi.DockerConfigurationPatchInput) {

	h := (*sharedHost)(t)
	in := (*sharedPatch)(h.ResourceUID, h.ConfigurationRevision)
	first, err := (*sharedService).PatchConfiguration((*sharedCtx), (*sharedActor), "docker.host", h.ID, in)
	(*sharedMust)(t, err)
	again, err := (*sharedNewService)().PatchConfiguration((*sharedCtx), (*sharedActor), "docker.host", h.ID, in)
	(*sharedMust)(t, err)
	if !reflect.DeepEqual(first, again) {
		t.Fatal("restart replay changed receipt")
	}
	changed := "different"
	in.Changes[0].StringValue = &changed
	if _, err := (*sharedService).PatchConfiguration((*sharedCtx), (*sharedActor), "docker.host", h.ID, in); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("same key accepted different input: %v", err)
	}
}

func dockerConditionalConfigurationWithPostgresPhase8(t *testing.T, sharedStore **db.Store, sharedCtx *context.Context, sharedRepo **dockerrepo.Repository, sharedService **appdocker.Service, sharedActor *domainidentity.Principal, sharedMust *func(tb *testing.T, err error), sharedHost *func(tb *testing.T) domaindocker.Host, sharedPatch *func(uid string, revision string) sohaapi.DockerConfigurationPatchInput, sharedNoReceipt *func(tb *testing.T, key string)) {

	h := (*sharedHost)(t)
	in := (*sharedPatch)(h.ResourceUID, h.ConfigurationRevision)
	(*sharedMust)(t, (*sharedStore).DB().WithContext((*sharedCtx)).Exec("UPDATE docker_hosts SET name='legacy-writer' WHERE id=?", h.ID).Error)
	if _, err := (*sharedService).PatchConfiguration((*sharedCtx), (*sharedActor), "docker.host", h.ID, in); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("legacy writer overwritten: %v", err)
	}
	(*sharedNoReceipt)(t, in.IdempotencyKey)
	(*sharedMust)(t, (*sharedRepo).DeleteHost((*sharedCtx), h.ID))
	_, err := (*sharedRepo).CreateHost((*sharedCtx), domaindocker.HostInput{ID: h.ID, Name: "recreated"})
	(*sharedMust)(t, err)
	if _, err := (*sharedService).PatchConfiguration((*sharedCtx), (*sharedActor), "docker.host", h.ID, in); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("ABA identity reused: %v", err)
	}
	(*sharedNoReceipt)(t, in.IdempotencyKey)
}

func dockerConditionalConfigurationWithPostgresPhase9(t *testing.T, sharedStore **db.Store, sharedCtx *context.Context, sharedRepo **dockerrepo.Repository, sharedService **appdocker.Service, sharedActor *domainidentity.Principal, sharedMust *func(tb *testing.T, err error), sharedProject *func(tb *testing.T) domaindocker.Project, sharedPatch *func(uid string, revision string) sohaapi.DockerConfigurationPatchInput) {

	p := (*sharedProject)(t)
	(*sharedMust)(t, (*sharedStore).DB().WithContext((*sharedCtx)).Exec(`UPDATE docker_projects SET status='running',desired_state='running',config='{"large":9007199254740993}'::jsonb WHERE id=?`, p.ID).Error)
	before, err := (*sharedRepo).GetProject((*sharedCtx), p.ID)
	(*sharedMust)(t, err)
	in := (*sharedPatch)(before.ResourceUID, before.ConfigurationRevision)
	_, err = (*sharedService).PatchConfiguration((*sharedCtx), (*sharedActor), "docker.project", p.ID, in)
	(*sharedMust)(t, err)
	after, err := (*sharedRepo).GetProject((*sharedCtx), p.ID)
	(*sharedMust)(t, err)
	var number string
	(*sharedMust)(t, (*sharedStore).DB().WithContext((*sharedCtx)).Raw("SELECT config->>'large' FROM docker_projects WHERE id=?", p.ID).Scan(&number).Error)
	if number != "9007199254740993" || after.Status != before.Status || after.DesiredState != before.DesiredState || !reflect.DeepEqual(after.ExpiresAt, before.ExpiresAt) {
		t.Fatal("partial write changed unrelated state")
	}
}

func dockerConditionalConfigurationWithPostgresPhase10(t *testing.T, sharedStore **db.Store, sharedCtx *context.Context, sharedRepo **dockerrepo.Repository, sharedService **appdocker.Service, sharedActor *domainidentity.Principal, sharedMust *func(tb *testing.T, err error), sharedProject *func(tb *testing.T) domaindocker.Project, sharedPatch *func(uid string, revision string) sohaapi.DockerConfigurationPatchInput) {

	p := (*sharedProject)(t)
	empty := ""
	zero := 0
	var object map[string]sohaapi.DockerConfigurationMapValue
	(*sharedMust)(t, json.Unmarshal([]byte(`{"large":9007199254740993}`), &object))
	in := (*sharedPatch)(p.ResourceUID, p.ConfigurationRevision)
	in.Changes = []sohaapi.DockerConfigurationChange{{Field: "ttlSeconds", IntegerValue: &zero}, {Field: "description", StringValue: &empty}, {Field: "config", ObjectValue: &object}}
	_, err := (*sharedService).PatchConfiguration((*sharedCtx), (*sharedActor), "docker.project", p.ID, in)
	(*sharedMust)(t, err)
	after, err := (*sharedRepo).GetProject((*sharedCtx), p.ID)
	(*sharedMust)(t, err)
	var number string
	(*sharedMust)(t, (*sharedStore).DB().WithContext((*sharedCtx)).Raw("SELECT config->>'large' FROM docker_projects WHERE id=?", p.ID).Scan(&number).Error)
	if after.TTLSeconds != 0 || after.ExpiresAt != nil || after.Description != "" || number != "9007199254740993" {
		t.Fatal("explicit clear or JSON precision lost")
	}
}

func dockerConditionalConfigurationWithPostgresPhase11(t *testing.T, sharedCtx *context.Context, sharedRepo **dockerrepo.Repository, sharedService **appdocker.Service, sharedActor *domainidentity.Principal, sharedMust *func(tb *testing.T, err error), sharedHost *func(tb *testing.T) domaindocker.Host, sharedProject *func(tb *testing.T) domaindocker.Project, sharedPatch *func(uid string, revision string) sohaapi.DockerConfigurationPatchInput, sharedNoReceipt *func(tb *testing.T, key string)) {

	p := (*sharedProject)(t)
	target := (*sharedHost)(t)
	targetID := target.ID
	wrong := uuid.New()
	in := (*sharedPatch)(p.ResourceUID, p.ConfigurationRevision)
	in.Changes = []sohaapi.DockerConfigurationChange{{Field: "hostId", StringValue: &targetID}}
	in.TargetHostUID = &wrong
	if _, err := (*sharedService).PatchConfiguration((*sharedCtx), (*sharedActor), "docker.project", p.ID, in); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("wrong target host UID accepted: %v", err)
	}
	(*sharedNoReceipt)(t, in.IdempotencyKey)
	correct := uuid.MustParse(target.ResourceUID)
	in.TargetHostUID = &correct
	denied := domaindocker.WithScopeCheck((*sharedCtx), func(scope map[string]string) error {
		if scope["hostId"] == target.ID {
			return apperrors.ErrAccessDenied
		}
		return nil
	})
	if _, err := (*sharedService).PatchConfiguration(denied, (*sharedActor), "docker.project", p.ID, in); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("target scope accepted: %v", err)
	}
	(*sharedNoReceipt)(t, in.IdempotencyKey)
	_, err := (*sharedService).PatchConfiguration((*sharedCtx), (*sharedActor), "docker.project", p.ID, in)
	(*sharedMust)(t, err)
	items, err := (*sharedRepo).ListServices((*sharedCtx), domaindocker.ServiceFilter{ProjectID: p.ID, Limit: 100})
	(*sharedMust)(t, err)
	if len(items) != 1 || items[0].HostID != target.ID || items[0].LastSeenAt != nil {
		t.Fatal("cold move lost or observed definition")
	}
}

func dockerConditionalConfigurationWithPostgresPhase12(t *testing.T, sharedStore **db.Store, sharedCtx *context.Context, sharedRepo **dockerrepo.Repository, sharedService **appdocker.Service, sharedActor *domainidentity.Principal, sharedMust *func(tb *testing.T, err error), sharedProject *func(tb *testing.T) domaindocker.Project, sharedPatch *func(uid string, revision string) sohaapi.DockerConfigurationPatchInput, sharedNoReceipt *func(tb *testing.T, key string)) {

	p := (*sharedProject)(t)
	(*sharedMust)(t, (*sharedStore).DB().WithContext((*sharedCtx)).Exec(`CREATE FUNCTION conditional_reject_definition() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.name='zz_reject' THEN RAISE EXCEPTION 'fixture rollback'; END IF; RETURN NEW; END $$; CREATE TRIGGER conditional_failure BEFORE INSERT ON docker_services FOR EACH ROW EXECUTE FUNCTION conditional_reject_definition();`).Error)
	defer (*sharedStore).DB().WithContext((*sharedCtx)).Exec(`DROP TRIGGER conditional_failure ON docker_services; DROP FUNCTION conditional_reject_definition();`)
	compose := "services:\n  aa_first: {}\n  zz_reject: {}\n"
	in := (*sharedPatch)(p.ResourceUID, p.ConfigurationRevision)
	in.Changes = []sohaapi.DockerConfigurationChange{{Field: "composeContent", StringValue: &compose}}
	if _, err := (*sharedService).PatchConfiguration((*sharedCtx), (*sharedActor), "docker.project", p.ID, in); err == nil {
		t.Fatal("injected failure accepted")
	}
	(*sharedNoReceipt)(t, in.IdempotencyKey)
	after, err := (*sharedRepo).GetProject((*sharedCtx), p.ID)
	(*sharedMust)(t, err)
	items, err := (*sharedRepo).ListServices((*sharedCtx), domaindocker.ServiceFilter{ProjectID: p.ID, Limit: 100})
	(*sharedMust)(t, err)
	if !reflect.DeepEqual(p, after) || len(items) != 1 {
		t.Fatal("partial configuration transaction committed")
	}
}

func dockerConditionalConfigurationWithPostgresPhase13(t *testing.T, sharedCtx *context.Context, sharedRepo **dockerrepo.Repository, sharedRoles **conditionalConfigurationRoles, sharedNewService *func() *appdocker.Service, sharedService **appdocker.Service, sharedActor *domainidentity.Principal, sharedMust *func(tb *testing.T, err error), sharedHost *func(tb *testing.T) domaindocker.Host, sharedConditions *func(uid string, revision string) sohaapi.DockerConfigurationPreconditions) {

	h := (*sharedHost)(t)
	in := sohaapi.DockerConfigurationDeleteInput{IdempotencyKey: uuid.NewString(), Preconditions: (*sharedConditions)(h.ResourceUID, h.ConfigurationRevision)}
	first, err := (*sharedService).DeleteConfiguration((*sharedCtx), (*sharedActor), "docker.host", h.ID, in)
	(*sharedMust)(t, err)
	_, err = (*sharedRepo).CreateHost((*sharedCtx), domaindocker.HostInput{ID: h.ID, Name: "replacement"})
	(*sharedMust)(t, err)
	again, err := (*sharedNewService)().DeleteConfiguration((*sharedCtx), (*sharedActor), "docker.host", h.ID, in)
	(*sharedMust)(t, err)
	if !first.Deleted || !reflect.DeepEqual(first, again) {
		t.Fatal("original deletion replay changed")
	}
	_, err = (*sharedRepo).GetHost((*sharedCtx), h.ID)
	(*sharedMust)(t, err)
	other := (*sharedActor)
	other.UserID = "other-actor"
	if _, err := (*sharedService).GetConfigurationRequest((*sharedCtx), other, in.IdempotencyKey); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("cross actor receipt visible: %v", err)
	}
	(*sharedRoles).denied.Store(true)
	_, err = (*sharedNewService)().GetConfigurationRequest((*sharedCtx), (*sharedActor), in.IdempotencyKey)
	(*sharedRoles).denied.Store(false)
	if !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("revoked receipt readable: %v", err)
	}
}

func dockerConditionalConfigurationWithPostgresPhase14(t *testing.T, sharedCtx *context.Context, sharedRepo **dockerrepo.Repository, sharedService **appdocker.Service, sharedActor *domainidentity.Principal, sharedMust *func(tb *testing.T, err error), sharedProject *func(tb *testing.T) domaindocker.Project, sharedConditions *func(uid string, revision string) sohaapi.DockerConfigurationPreconditions, sharedNoReceipt *func(tb *testing.T, key string)) {

	p := (*sharedProject)(t)
	h, err := (*sharedRepo).GetHost((*sharedCtx), p.HostID)
	(*sharedMust)(t, err)
	in := sohaapi.DockerConfigurationDeleteInput{IdempotencyKey: uuid.NewString(), Preconditions: (*sharedConditions)(h.ResourceUID, h.ConfigurationRevision)}
	if _, err := (*sharedService).DeleteConfiguration((*sharedCtx), (*sharedActor), "docker.host", h.ID, in); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("host children silently removed: %v", err)
	}
	(*sharedNoReceipt)(t, in.IdempotencyKey)
	_, err = (*sharedRepo).CreateOperation((*sharedCtx), domaindocker.OperationInput{HostID: h.ID, ProjectID: p.ID, OperationKind: "fixture", Status: "completed"})
	(*sharedMust)(t, err)
	pin := sohaapi.DockerConfigurationDeleteInput{IdempotencyKey: uuid.NewString(), Preconditions: (*sharedConditions)(p.ResourceUID, p.ConfigurationRevision)}
	if _, err := (*sharedService).DeleteConfiguration((*sharedCtx), (*sharedActor), "docker.project", p.ID, pin); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("runtime history silently removed: %v", err)
	}
	(*sharedNoReceipt)(t, pin.IdempotencyKey)
}

func dockerConditionalConfigurationWithPostgresPhase15(t *testing.T, sharedStore **db.Store, sharedCtx *context.Context, sharedNewService *func() *appdocker.Service, sharedService **appdocker.Service, sharedActor *domainidentity.Principal, sharedMust *func(tb *testing.T, err error)) {

	in := sohaapi.DockerHostRegistrationInput{IdempotencyKey: uuid.NewString(), Host: sohaapi.DockerHostRegistration{Name: "registered-only"}}
	first, err := (*sharedService).RegisterHostConfiguration((*sharedCtx), (*sharedActor), in)
	(*sharedMust)(t, err)
	again, err := (*sharedNewService)().RegisterHostConfiguration((*sharedCtx), (*sharedActor), in)
	(*sharedMust)(t, err)
	if !reflect.DeepEqual(first, again) || first.Action != "register" {
		t.Fatal("host registration was not idempotent")
	}
	var count int64
	(*sharedMust)(t, (*sharedStore).DB().WithContext((*sharedCtx)).Raw("SELECT count(*) FROM docker_operations WHERE host_id=?", first.Resource.ID).Scan(&count).Error)
	if count != 0 {
		t.Fatal("metadata registration created runtime work")
	}
}

func dockerConditionalConfigurationWithPostgresPhase16(t *testing.T, sharedCtx *context.Context, sharedRepo **dockerrepo.Repository, sharedService **appdocker.Service, sharedActor *domainidentity.Principal, sharedMust *func(tb *testing.T, err error), sharedHost *func(tb *testing.T) domaindocker.Host, sharedPatch *func(uid string, revision string) sohaapi.DockerConfigurationPatchInput, sharedNoReceipt *func(tb *testing.T, key string)) {

	h := (*sharedHost)(t)
	in := (*sharedPatch)(h.ResourceUID, h.ConfigurationRevision)
	limited := (*sharedActor)
	limited.PermissionKeys = []string{"docker.hosts.create"}
	if _, err := (*sharedService).PatchConfiguration((*sharedCtx), limited, "docker.host", h.ID, in); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("credential ceiling bypassed: %v", err)
	}
	(*sharedNoReceipt)(t, in.IdempotencyKey)
	after, err := (*sharedRepo).GetHost((*sharedCtx), h.ID)
	(*sharedMust)(t, err)
	if !reflect.DeepEqual(h, after) {
		t.Fatal("denied key mutated host")
	}
}

func dockerConditionalConfigurationWithPostgresPhase17(t *testing.T, sharedStore **db.Store, sharedCtx *context.Context, sharedRepo **dockerrepo.Repository, sharedService **appdocker.Service, sharedActor *domainidentity.Principal, sharedMust *func(tb *testing.T, err error), sharedHost *func(tb *testing.T) domaindocker.Host, sharedConditions *func(uid string, revision string) sohaapi.DockerConfigurationPreconditions, sharedNoReceipt *func(tb *testing.T, key string)) {

	h := (*sharedHost)(t)
	(*sharedMust)(t, (*sharedStore).DB().WithContext((*sharedCtx)).Exec(`CREATE FUNCTION conditional_reject_receipt() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture receipt rollback'; END $$; CREATE TRIGGER conditional_receipt_failure BEFORE INSERT ON docker_configuration_requests FOR EACH ROW EXECUTE FUNCTION conditional_reject_receipt();`).Error)
	defer (*sharedStore).DB().WithContext((*sharedCtx)).Exec(`DROP TRIGGER conditional_receipt_failure ON docker_configuration_requests; DROP FUNCTION conditional_reject_receipt();`)
	in := sohaapi.DockerConfigurationDeleteInput{IdempotencyKey: uuid.NewString(), Preconditions: (*sharedConditions)(h.ResourceUID, h.ConfigurationRevision)}
	if _, err := (*sharedService).DeleteConfiguration((*sharedCtx), (*sharedActor), "docker.host", h.ID, in); err == nil {
		t.Fatal("receipt insertion failure accepted")
	}
	(*sharedNoReceipt)(t, in.IdempotencyKey)
	after, err := (*sharedRepo).GetHost((*sharedCtx), h.ID)
	(*sharedMust)(t, err)
	if !reflect.DeepEqual(h, after) {
		t.Fatal("failed receipt left host deleted or changed")
	}
}
