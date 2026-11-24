package access

import (
	"context"
	"errors"
	"testing"
	"time"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainscopegrant "github.com/opensoha/soha/internal/domain/scopegrant"
	"github.com/opensoha/soha/internal/policy"
)

type snapshotTestRepository struct {
	roleReads, policyReads, grantReads              int
	permissionReads, environmentReads, bindingReads int
	permissions                                     map[string][]string
	grants                                          []domainscopegrant.Record
	err                                             error
}

func (r *snapshotTestRepository) ListRolePermissions(context.Context) (map[string][]string, error) {
	r.permissionReads++
	return r.permissions, r.err
}

func (r *snapshotTestRepository) ListEnvironments(context.Context) ([]domaincatalog.Environment, error) {
	r.environmentReads++
	return nil, r.err
}

func (r *snapshotTestRepository) ListApplicationEnvironments(context.Context) ([]domaincatalog.ApplicationEnvironment, error) {
	r.bindingReads++
	return nil, r.err
}

func (r *snapshotTestRepository) ListRoleCapabilities(context.Context) (map[string][]domainaccess.Action, error) {
	r.roleReads++
	return RoleMatrix(), r.err
}

func (r *snapshotTestRepository) ListPolicies(context.Context) ([]domainaccess.Policy, error) {
	r.policyReads++
	return DefaultPolicies(), r.err
}

func (r *snapshotTestRepository) List(context.Context) ([]domainscopegrant.Record, error) {
	r.grantReads++
	return r.grants, r.err
}

func snapshotApplicationRequest(id string) domainaccess.Request {
	return domainaccess.Request{
		Principal: domainidentity.Principal{UserID: "user-1", Roles: []string{"admin"}},
		Subject:   domainaccess.SubjectAttributes{UserID: "user-1", Roles: []string{"admin"}},
		Action:    domainaccess.ActionList,
		Resource:  domainaccess.ResourceAttributes{Kind: "Application", Name: id},
		Delivery:  domainaccess.DeliveryAttributes{BusinessLineID: "business-1", ApplicationID: id},
	}
}

func TestReadSnapshotEvaluatesEachResourceAndRefreshesNextRead(t *testing.T) {
	repo := &snapshotTestRepository{grants: []domainscopegrant.Record{
		{ID: "allow", SubjectType: "user", SubjectID: "user-1", BusinessLineID: "business-1", ApplicationIDs: []string{"allowed"}, Role: "readonly", Effect: "allow", Enabled: true},
		{ID: "deny", SubjectType: "user", SubjectID: "user-1", BusinessLineID: "business-1", ApplicationIDs: []string{"denied"}, Role: "readonly", Effect: "deny", Enabled: true},
	}}
	service := New(policy.NewEngine(), repo, repo, nil)
	ctx := context.Background()
	reader := SnapshotForRead(ctx, service)
	for range 3 {
		for _, id := range []string{"allowed", "denied", "ungranted"} {
			decision, err := reader.Authorize(ctx, snapshotApplicationRequest(id))
			if err != nil || decision.Allowed != (id == "allowed") {
				t.Fatalf("%s: decision=%#v err=%v", id, decision, err)
			}
		}
	}
	if repo.roleReads != 1 || repo.policyReads != 1 || repo.grantReads != 1 {
		t.Fatalf("policy data reloaded within one read: %#v", repo)
	}
	repo.grants = []domainscopegrant.Record{{ID: "revoked", SubjectType: "user", SubjectID: "user-1", BusinessLineID: "business-1", ApplicationIDs: []string{"allowed"}, Role: "readonly", Effect: "deny", Enabled: true}}
	decision, err := SnapshotForRead(ctx, service).Authorize(ctx, snapshotApplicationRequest("allowed"))
	if err != nil || decision.Allowed || repo.grantReads != 2 {
		t.Fatalf("next read ignored revocation: decision=%#v err=%v reads=%d", decision, err, repo.grantReads)
	}
}

func TestReadSnapshotFailsClosedOnLoadErrorAndCancellation(t *testing.T) {
	want := errors.New("policy store unavailable")
	repo := &snapshotTestRepository{err: want}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader := SnapshotForRead(ctx, New(policy.NewEngine(), repo, repo, nil))
	for range 2 {
		decision, err := reader.Authorize(ctx, snapshotApplicationRequest("app-1"))
		if !errors.Is(err, want) || decision.Allowed {
			t.Fatalf("failed open: decision=%#v err=%v", decision, err)
		}
	}
	if repo.roleReads != 1 {
		t.Fatalf("failed load retried within same read: %d", repo.roleReads)
	}
	cancel()
	if _, err := reader.Authorize(ctx, snapshotApplicationRequest("app-1")); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation ignored: %v", err)
	}
}

func TestSnapshotLoaderDoesNotHoldCanceledObserver(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	read := snapshotLoader(t.Context(), func(ctx context.Context) (string, error) {
		close(started)
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-release:
			return "active", nil
		}
	})
	child, cancelChild := context.WithCancel(t.Context())
	defer cancelChild()
	finished := make(chan error, 1)
	go func() {
		_, err := read(child)
		finished <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("snapshot load did not start")
	}
	cancelChild()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("waiter returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled observer waited for the shared load")
	}
	close(release)
	if got, err := read(t.Context()); err != nil || got != "active" {
		t.Fatalf("remaining observer received %q, %v", got, err)
	}
}

func TestReadSnapshotSharesScopedPermissionInputsAndRefreshesNextRequest(t *testing.T) {
	repo := &snapshotTestRepository{
		permissions: map[string][]string{"admin": {PermPlatformPodsView}},
		grants: []domainscopegrant.Record{
			{ID: "allow", SubjectType: "user", SubjectID: "user-1", ScopeType: domainscopegrant.ScopeTypePlatform, Role: "admin", Enabled: true, ClusterIDs: []string{"cluster-a"}, Namespaces: []string{"allowed"}},
			{ID: "deny", SubjectType: "user", SubjectID: "user-1", ScopeType: domainscopegrant.ScopeTypePlatform, Role: "admin", Enabled: true, ClusterIDs: []string{"cluster-a"}, Namespaces: []string{"denied"}, Effect: "deny"},
		},
	}
	service := New(policy.NewEngine(), repo, repo, repo)
	service.SetPermissionResolver(NewPermissionResolver(repo))
	principal := domainidentity.Principal{UserID: "user-1", Roles: []string{"admin"}, PermissionKeys: []string{PermPlatformPodsView}}
	ctx := t.Context()
	read := func(reader domainaccess.Authorizer, namespace string, want bool) {
		t.Helper()
		request := platformRequest(principal, domainaccess.ActionView, namespace, "workloads", "Pod")
		request.PermissionKey = PermPlatformPodsView
		decision, err := reader.Authorize(ctx, request)
		if err != nil || decision.Allowed != want {
			t.Fatalf("%s: decision=%+v err=%v", namespace, decision, err)
		}
	}
	reader := SnapshotForRead(ctx, service)
	for range 15 {
		read(reader, "allowed", true)
		read(reader, "denied", false)
	}
	if repo.permissionReads != 1 || repo.policyReads != 1 || repo.grantReads != 1 || repo.environmentReads != 1 || repo.bindingReads != 1 {
		t.Fatalf("authorization inputs reloaded for 30 distinct decisions: %+v", repo)
	}
	repo.permissions = map[string][]string{"admin": {PermPlatformDeploymentView}}
	read(SnapshotForRead(ctx, service), "allowed", false)
	if repo.permissionReads != 2 {
		t.Fatalf("next request reused role permissions, reads=%d", repo.permissionReads)
	}
}
