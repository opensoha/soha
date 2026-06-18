package release

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainrelease "github.com/opensoha/soha/internal/domain/release"
	"github.com/opensoha/soha/internal/platform/apperrors"
	apprepo "github.com/opensoha/soha/internal/repository/application"
	clusterrepo "github.com/opensoha/soha/internal/repository/cluster"
)

type stubReleaseRepository struct {
	items       []domainrelease.Record
	deletedIDs  []string
	createCalls int
}

func (r *stubReleaseRepository) List(context.Context, domainrelease.Filter) ([]domainrelease.Record, error) {
	return append([]domainrelease.Record(nil), r.items...), nil
}

func (r *stubReleaseRepository) Get(_ context.Context, id string) (domainrelease.Record, error) {
	for _, item := range r.items {
		if item.ID == id {
			return item, nil
		}
	}
	return domainrelease.Record{}, apperrors.ErrNotFound
}

func (r *stubReleaseRepository) Create(context.Context, domainrelease.Record) (domainrelease.Record, error) {
	r.createCalls++
	return domainrelease.Record{}, nil
}

func (r *stubReleaseRepository) DeleteByIDs(_ context.Context, ids []string) error {
	r.deletedIDs = append(r.deletedIDs, ids...)
	return nil
}

type stubReleaseApps struct {
	missing map[string]bool
}

func (a *stubReleaseApps) Get(_ context.Context, applicationID string) (domainapp.App, error) {
	if a.missing[applicationID] {
		return domainapp.App{}, apprepo.ErrNotFound
	}
	return domainapp.App{ID: applicationID, Name: "ok"}, nil
}

type stubReleaseResolver struct {
	missing map[string]bool
}

type stubReleaseRolePermissionReader struct {
	matrix map[string][]string
}

func (s stubReleaseRolePermissionReader) ListRolePermissions(context.Context) (map[string][]string, error) {
	return s.matrix, nil
}

func releasePermissions(role string, keys ...string) *appaccess.PermissionResolver {
	return appaccess.NewPermissionResolver(stubReleaseRolePermissionReader{
		matrix: map[string][]string{
			role: keys,
		},
	})
}

func (r *stubReleaseResolver) GetConnection(_ context.Context, clusterID string) (domaincluster.Connection, error) {
	if r.missing[clusterID] {
		return domaincluster.Connection{}, clusterrepo.ErrNotFound
	}
	return domaincluster.Connection{
		Summary: domaincluster.Summary{
			ID:             clusterID,
			Name:           clusterID,
			ConnectionMode: domaincluster.ConnectionModeDirectKubeconfig,
			Health:         domaincluster.Health{Status: "healthy", LastChecked: time.Now().UTC()},
		},
	}, nil
}

func TestListPrunesStaleDependencies(t *testing.T) {
	repo := &stubReleaseRepository{
		items: []domainrelease.Record{
			{ID: "keep", ApplicationID: "app-ok", ClusterID: "cluster-ok", Namespace: "default", DeploymentName: "dep"},
			{ID: "stale-app", ApplicationID: "app-missing", ClusterID: "cluster-ok", Namespace: "default", DeploymentName: "dep"},
			{ID: "stale-cluster", ApplicationID: "app-ok", ClusterID: "cluster-missing", Namespace: "default", DeploymentName: "dep"},
			{ID: "stale-empty-cluster", ApplicationID: "app-ok", ClusterID: "", Namespace: "default", DeploymentName: "dep"},
		},
	}
	service := &Service{
		repo: repo,
		apps: &stubReleaseApps{missing: map[string]bool{"app-missing": true}},
		resolver: &stubReleaseResolver{
			missing: map[string]bool{"cluster-missing": true},
		},
		permissions: releasePermissions("developer", appaccess.PermDeliveryReleasesView),
	}

	items, err := service.List(context.Background(), domainidentity.Principal{Roles: []string{"developer"}}, domainrelease.Filter{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(items) != 1 || items[0].ID != "keep" {
		t.Fatalf("List() items = %+v, want only keep", items)
	}

	sort.Strings(repo.deletedIDs)
	expectedDeleted := []string{"stale-app", "stale-cluster", "stale-empty-cluster"}
	sort.Strings(expectedDeleted)
	if len(repo.deletedIDs) != len(expectedDeleted) {
		t.Fatalf("deletedIDs len = %d, want %d (%v)", len(repo.deletedIDs), len(expectedDeleted), repo.deletedIDs)
	}
	for i := range expectedDeleted {
		if repo.deletedIDs[i] != expectedDeleted[i] {
			t.Fatalf("deletedIDs = %v, want %v", repo.deletedIDs, expectedDeleted)
		}
	}
}

func TestTriggerReturnsNotFoundWhenApplicationMissing(t *testing.T) {
	repo := &stubReleaseRepository{}
	service := &Service{
		repo:        repo,
		apps:        &stubReleaseApps{missing: map[string]bool{"missing-app": true}},
		permissions: releasePermissions("developer", appaccess.PermDeliveryReleasesTrigger),
	}

	_, err := service.Trigger(context.Background(), domainidentity.Principal{Roles: []string{"developer"}}, domainrelease.TriggerInput{
		ApplicationID:  "missing-app",
		ClusterID:      "cluster-ok",
		Namespace:      "default",
		DeploymentName: "dep",
		Image:          "repo/image:tag",
	})
	if !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("Trigger() error = %v, want ErrNotFound", err)
	}
	if repo.createCalls != 0 {
		t.Fatalf("Create() called %d times, want 0", repo.createCalls)
	}
}

func TestTriggerRequiresReleaseTriggerPermission(t *testing.T) {
	repo := &stubReleaseRepository{}
	service := &Service{
		repo: repo,
		apps: &stubReleaseApps{},
	}

	_, err := service.Trigger(context.Background(), domainidentity.Principal{Roles: []string{"readonly"}}, domainrelease.TriggerInput{
		ApplicationID:  "app-ok",
		ClusterID:      "cluster-ok",
		Namespace:      "default",
		DeploymentName: "dep",
		Image:          "repo/image:tag",
	})
	if !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("Trigger() error = %v, want ErrAccessDenied", err)
	}
	if repo.createCalls != 0 {
		t.Fatalf("Create() called %d times, want 0", repo.createCalls)
	}
}

func TestTriggerAllowsDelegatedReleasePermission(t *testing.T) {
	repo := &stubReleaseRepository{}
	service := &Service{
		repo: repo,
		apps: &stubReleaseApps{missing: map[string]bool{"app-ok": true}},
		permissions: appaccess.NewPermissionResolver(stubReleaseRolePermissionReader{
			matrix: map[string][]string{
				"delegated": {appaccess.PermDeliveryReleasesTrigger},
			},
		}),
	}

	_, err := service.Trigger(context.Background(), domainidentity.Principal{Roles: []string{"delegated"}}, domainrelease.TriggerInput{
		ApplicationID:  "app-ok",
		ClusterID:      "cluster-ok",
		Namespace:      "default",
		DeploymentName: "dep",
		Image:          "repo/image:tag",
	})
	if !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("Trigger() error = %v, want ErrNotFound after passing permission gate", err)
	}
}

func TestTriggerFailsClosedWithoutRuntimeResolver(t *testing.T) {
	repo := &stubReleaseRepository{}
	service := &Service{
		repo: repo,
		apps: &stubReleaseApps{},
	}

	_, err := service.Trigger(context.Background(), domainidentity.Principal{Roles: []string{"developer"}}, domainrelease.TriggerInput{
		ApplicationID:  "app-ok",
		ClusterID:      "cluster-ok",
		Namespace:      "default",
		DeploymentName: "dep",
		Image:          "repo/image:tag",
	})
	if !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("Trigger() error = %v, want ErrAccessDenied when runtime resolver is missing", err)
	}
	if repo.createCalls != 0 {
		t.Fatalf("Create() called %d times, want 0", repo.createCalls)
	}
}
