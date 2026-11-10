package docker

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domaindocker "github.com/opensoha/soha/internal/domain/docker"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func identityTestService(keys []string) (*Service, *memoryDockerRepo, *captureDockerAudit, *captureDockerOperations) {
	repo := newMemoryDockerRepo()
	repo.hosts["host-1"] = domaindocker.Host{ID: "host-1", ResourceUID: uuid.NewString(), ConfigurationRevision: uuid.NewString(), Config: map[string]any{"private": "identity-private-fixture"}}
	repo.projects["project-1"] = domaindocker.Project{ID: "project-1", HostID: "host-1", ResourceUID: uuid.NewString(), ConfigurationRevision: uuid.NewString(), ComposeContent: "identity-private-fixture", EnvContent: "identity-private-fixture"}
	audit, operations := &captureDockerAudit{}, &captureDockerOperations{}
	resolver := appaccess.NewPermissionResolver(dockerTestRoleReader{matrix: map[string][]string{"reader": keys}})
	return New(repo, resolver, operations, WithAudit(audit)), repo, audit, operations
}

func TestDockerResourceIdentityAuthorizationAndProjection(t *testing.T) {
	for _, resource := range []string{"host", "project"} {
		for _, tc := range []struct {
			name          string
			keys, ceiling []string
			allowed       bool
			missing       bool
		}{
			{name: "matching view", keys: []string{"docker." + resource + "s.view"}, allowed: true},
			{name: "unrelated view", keys: []string{"docker.services.view"}},
			{name: "no grants"},
			{name: "token ceiling denies", keys: []string{"docker.hosts.view", "docker.projects.view"}, ceiling: []string{"docker.services.view"}},
			{name: "token and role intersection", keys: []string{"docker.hosts.view", "docker.projects.view"}, ceiling: []string{"docker." + resource + "s.view"}, allowed: true},
			{name: "missing resolver", missing: true},
		} {
			t.Run(resource+"/"+tc.name, func(t *testing.T) { dockerResourceIdentityAuthorizationAndProjectionPhase1(t, &resource, &tc) })
		}
	}
}

func TestDockerResourceIdentityScopeAndMissingIdentity(t *testing.T) {
	for _, resource := range []string{"host", "project"} {
		for _, scenario := range []string{"scope denied", "scope allowed", "missing identity", "missing row"} {
			t.Run(resource+"/"+scenario, func(t *testing.T) { dockerResourceIdentityScopeAndMissingIdentityPhase1(t, &resource, &scenario) })
		}
	}
}

func dockerResourceIdentityAuthorizationAndProjectionPhase1(t *testing.T, sharedResource *string, sharedTc *struct {
	name    string
	keys    []string
	ceiling []string
	allowed bool
	missing bool
}) {

	s, repo, audit, operations := identityTestService((*sharedTc).keys)
	if (*sharedTc).missing {
		s.permissions = nil
	}
	p := domainidentity.Principal{UserID: "reader", Roles: []string{"reader"}, PermissionKeys: (*sharedTc).ceiling}
	var result sohaapi.DockerResourceIdentity
	var err error
	if (*sharedResource) == "host" {
		result, err = s.GetHostIdentity(t.Context(), p, "host-1")
	} else {
		result, err = s.GetProjectIdentity(t.Context(), p, "project-1")
	}
	if !(*sharedTc).allowed {
		if !errors.Is(err, apperrors.ErrAccessDenied) {
			t.Fatalf("unauthorized identity read: %v", err)
		}
	} else {
		assertDockerIdentityFromRow(t, *sharedResource, result, err, repo)
	}
	if len(audit.entries) != 1 || len(operations.entries) != 0 {
		t.Fatal("read audit missing or identity read enqueued an operation")
	}
	if (*sharedTc).allowed && audit.entries[0].Result != "success" || !(*sharedTc).allowed && audit.entries[0].Result != "failure" {
		t.Fatal("read audit result is incorrect")
	}
	raw, _ := json.Marshal(struct {
		Result sohaapi.DockerResourceIdentity
		Audit  any
	}{result, audit.entries})
	if strings.Contains(string(raw), "identity-private-fixture") || strings.Contains(string(raw), "composeContent") || strings.Contains(string(raw), "envContent") {
		t.Fatal("identity response/audit exposed configuration")
	}
}

func dockerResourceIdentityScopeAndMissingIdentityPhase1(t *testing.T, sharedResource *string, sharedScenario *string) {

	s, repo, _, _ := identityTestService([]string{"docker.hosts.view", "docker.projects.view"})
	id := (*sharedResource) + "-1"
	if (*sharedScenario) == "missing identity" {
		if (*sharedResource) == "host" {
			item := repo.hosts[id]
			item.ResourceUID = ""
			repo.hosts[id] = item
		} else {
			item := repo.projects[id]
			item.ConfigurationRevision = ""
			repo.projects[id] = item
		}
	}
	if (*sharedScenario) == "missing row" {
		id = "missing"
	}
	checks := 0
	ctx := domaindocker.WithScopeCheck(t.Context(), func(scope map[string]string) error {
		checks++
		if scope["hostId"] != "host-1" || (*sharedResource) == "project" && scope["projectId"] != "project-1" {
			t.Fatalf("scope used untrusted facts: %#v", scope)
		}
		if (*sharedScenario) == "scope denied" {
			return apperrors.ErrAccessDenied
		}
		return nil
	})
	principal := domainidentity.Principal{UserID: "reader", Roles: []string{"reader"}}
	var err error
	if (*sharedResource) == "host" {
		_, err = s.GetHostIdentity(ctx, principal, id)
	} else {
		_, err = s.GetProjectIdentity(ctx, principal, id)
	}
	switch *sharedScenario {
	case "scope denied":
		if !errors.Is(err, apperrors.ErrAccessDenied) || checks != 1 {
			t.Fatalf("err=%v checks=%d", err, checks)
		}
	case "scope allowed":
		if err != nil || checks != 1 {
			t.Fatalf("err=%v checks=%d", err, checks)
		}
	case "missing identity":
		if !errors.Is(err, apperrors.ErrUnsupportedOperation) {
			t.Fatalf("fabricated identity: %v", err)
		}
	case "missing row":
		if err == nil || checks != 0 {
			t.Fatalf("missing row: err=%v checks=%d", err, checks)
		}
	}
}

func assertDockerIdentityFromRow(t *testing.T, resource string, result sohaapi.DockerResourceIdentity, err error, repo *memoryDockerRepo) {
	t.Helper()
	if err != nil || string(result.ResourceKind) != "docker."+resource || result.ID != resource+"-1" || result.UID == uuid.Nil || result.ConfigurationRevision == uuid.Nil {
		t.Fatalf("identity=%+v err=%v", result, err)
	}
	if resource == "host" && result.UID.String() != repo.hosts["host-1"].ResourceUID || resource == "project" && (result.UID.String() != repo.projects["project-1"].ResourceUID || result.HostID != "host-1") {
		t.Fatal("identity did not come from the selected database row")
	}
}
