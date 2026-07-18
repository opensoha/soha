package cluster

import (
	"context"
	"errors"
	"reflect"
	"testing"

	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/appconfig"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

var errClusterRepository = errors.New("cluster repository failure")

type clusterContractContextKey struct{}

type contractRegistry struct {
	validated    []appconfig.Cluster
	registered   []appconfig.Cluster
	unregistered []string
	validateErr  error
}

func (r *contractRegistry) RegisterCluster(cluster appconfig.Cluster) {
	r.registered = append(r.registered, cluster)
}

func (r *contractRegistry) UnregisterCluster(clusterID string) {
	r.unregistered = append(r.unregistered, clusterID)
}

func (r *contractRegistry) ValidateCluster(cluster appconfig.Cluster) error {
	r.validated = append(r.validated, cluster)
	return r.validateErr
}

type contractRuntime struct {
	context context.Context
	summary domaincluster.Summary
	err     error
}

func (*contractRuntime) ClusterIDs() []string { return nil }

func (r *contractRuntime) ListClusters(ctx context.Context) ([]domaincluster.Summary, error) {
	r.context = ctx
	return []domaincluster.Summary{r.summary}, r.err
}

func (r *contractRuntime) GetCluster(ctx context.Context, _ string) (domaincluster.Summary, error) {
	r.context = ctx
	return r.summary, r.err
}

type contractCache struct {
	context      context.Context
	registered   []string
	unregistered []string
}

func (c *contractCache) RegisterCluster(ctx context.Context, clusterID string) error {
	c.context = ctx
	c.registered = append(c.registered, clusterID)
	return nil
}

func (c *contractCache) UnregisterCluster(clusterID string) {
	c.unregistered = append(c.unregistered, clusterID)
}

func (*contractCache) Status(string) domaincluster.CacheDiagnostic {
	return domaincluster.CacheDiagnostic{Status: "ready", Ready: true}
}

type contractRepository struct {
	context          context.Context
	connection       domaincluster.Connection
	listConnections  []domaincluster.Connection
	upserted         domaincluster.Connection
	updated          domaincluster.Connection
	snapshot         domaincluster.Summary
	deletedClusterID string
	upsertErr        error
	updateErr        error
	deleteErr        error
}

func (r *contractRepository) List(ctx context.Context) ([]domaincluster.Summary, error) {
	r.context = ctx
	return []domaincluster.Summary{r.connection.Summary}, nil
}

func (r *contractRepository) Get(ctx context.Context, _ string) (domaincluster.Summary, error) {
	r.context = ctx
	return r.connection.Summary, nil
}

func (r *contractRepository) ListConnections(ctx context.Context) ([]domaincluster.Connection, error) {
	r.context = ctx
	return append([]domaincluster.Connection(nil), r.listConnections...), nil
}

func (r *contractRepository) GetConnection(ctx context.Context, _ string) (domaincluster.Connection, error) {
	r.context = ctx
	return r.connection, nil
}

func (r *contractRepository) UpsertRegistration(ctx context.Context, connection domaincluster.Connection) error {
	r.context = ctx
	r.upserted = connection
	if r.upsertErr == nil {
		r.connection = connection
	}
	return r.upsertErr
}

func (r *contractRepository) UpdateRegistration(ctx context.Context, connection domaincluster.Connection) error {
	r.context = ctx
	r.updated = connection
	if r.updateErr == nil {
		r.connection = connection
	}
	return r.updateErr
}

func (r *contractRepository) UpsertSnapshot(ctx context.Context, summary domaincluster.Summary) error {
	r.context = ctx
	r.snapshot = summary
	r.connection.Summary = summary
	return nil
}

func (r *contractRepository) Delete(ctx context.Context, clusterID string) error {
	r.context = ctx
	r.deletedClusterID = clusterID
	return r.deleteErr
}

func TestNewRejectsMissingRuntimeDependencies(t *testing.T) {
	tests := []struct {
		name     string
		registry RuntimeRegistry
		runtime  RuntimeReader
	}{
		{name: "registry", runtime: &contractRuntime{}},
		{name: "runtime", registry: &contractRegistry{}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, err := New(test.registry, test.runtime, nil, nil, nil, nil, nil, nil)
			if service != nil {
				t.Fatalf("service = %#v, want nil", service)
			}
			if !errors.Is(err, apperrors.ErrInvalidArgument) {
				t.Fatalf("error = %v, want ErrInvalidArgument", err)
			}
		})
	}
}

func TestRestoreRuntimeRegistrationsPreservesContextAndDTO(t *testing.T) {
	ctx := context.WithValue(t.Context(), clusterContractContextKey{}, "restore")
	want := directConnection("cluster-restore", "restored")
	repo := &contractRepository{listConnections: []domaincluster.Connection{want}}
	registry := &contractRegistry{}
	cache := &contractCache{}
	service, err := New(registry, &contractRuntime{}, cache, nil, repo, nil, nil, nil)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	service.restoreRuntimeRegistrations(ctx)

	if repo.context != ctx || cache.context != ctx {
		t.Fatal("restore did not preserve context across repository and cache boundaries")
	}
	if len(registry.registered) != 1 {
		t.Fatalf("registered count = %d, want 1", len(registry.registered))
	}
	assertRuntimeCluster(t, registry.registered[0], want)
	if !reflect.DeepEqual(cache.registered, []string{"cluster-restore"}) {
		t.Fatalf("cache registrations = %v", cache.registered)
	}
}

func TestRegisterPreservesContextDTOAndRepositoryError(t *testing.T) {
	ctx := context.WithValue(t.Context(), clusterContractContextKey{}, "register")
	repo := &contractRepository{upsertErr: errClusterRepository}
	registry := &contractRegistry{}
	service, err := New(registry, &contractRuntime{}, nil, nil, repo, nil, nil, nil)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	_, err = service.Register(ctx, domainidentity.Principal{}, domaincluster.RegisterInput{
		ID:             "cluster-register",
		Name:           "registered",
		ConnectionMode: domaincluster.ConnectionModeDirectKubeconfig,
		Kubeconfig:     "register-config",
		Context:        "register-context",
	})
	if !errors.Is(err, errClusterRepository) {
		t.Fatalf("error = %v, want repository error", err)
	}
	if repo.context != ctx {
		t.Fatal("repository received a different context")
	}
	if repo.upserted.Summary.ID != "cluster-register" || repo.upserted.Summary.Name != "registered" {
		t.Fatalf("persisted DTO = %#v", repo.upserted)
	}
	if len(registry.validated) != 1 {
		t.Fatalf("validated count = %d, want 1", len(registry.validated))
	}
	if len(registry.registered) != 0 {
		t.Fatalf("runtime was mutated after persistence error: %#v", registry.registered)
	}
}

func TestUpdatePersistsBeforeReplacingRuntimeRegistration(t *testing.T) {
	ctx := context.WithValue(t.Context(), clusterContractContextKey{}, "update")
	existing := directConnection("cluster-update", "before")
	repo := &contractRepository{connection: existing, updateErr: errClusterRepository}
	registry := &contractRegistry{}
	service, err := New(registry, &contractRuntime{}, nil, nil, repo, nil, nil, nil)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	_, err = service.Update(ctx, domainidentity.Principal{}, "cluster-update", domaincluster.UpdateInput{Name: "after"})
	if !errors.Is(err, errClusterRepository) {
		t.Fatalf("error = %v, want repository error", err)
	}
	if repo.context != ctx {
		t.Fatal("repository received a different context")
	}
	if repo.updated.Summary.ID != "cluster-update" || repo.updated.Summary.Name != "after" {
		t.Fatalf("updated DTO = %#v", repo.updated)
	}
	if len(registry.unregistered) != 0 || len(registry.registered) != 0 {
		t.Fatalf("runtime changed before persistence succeeded: unregister=%v register=%v", registry.unregistered, registry.registered)
	}
}

func TestDeletePreservesContextDTOAndRepositoryError(t *testing.T) {
	ctx := context.WithValue(t.Context(), clusterContractContextKey{}, "delete")
	repo := &contractRepository{
		connection: directConnection("cluster-delete", "deleted"),
		deleteErr:  errClusterRepository,
	}
	registry := &contractRegistry{}
	service, err := New(registry, &contractRuntime{}, nil, nil, repo, nil, nil, nil)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	err = service.Delete(ctx, domainidentity.Principal{}, "cluster-delete")
	if !errors.Is(err, errClusterRepository) {
		t.Fatalf("error = %v, want repository error", err)
	}
	if repo.context != ctx || repo.deletedClusterID != "cluster-delete" {
		t.Fatalf("delete contract context/id = %v/%q", repo.context, repo.deletedClusterID)
	}
	if len(registry.unregistered) != 0 {
		t.Fatalf("runtime changed after persistence error: %v", registry.unregistered)
	}
}

func TestDeleteUnregistersRuntimeAfterPersistenceSucceeds(t *testing.T) {
	ctx := context.WithValue(t.Context(), clusterContractContextKey{}, "delete-success")
	repo := &contractRepository{connection: directConnection("cluster-delete", "deleted")}
	registry := &contractRegistry{}
	cache := &contractCache{}
	service, err := New(registry, &contractRuntime{}, cache, nil, repo, nil, nil, nil)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	if err := service.Delete(ctx, domainidentity.Principal{}, "cluster-delete"); err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}
	if repo.context != ctx || repo.deletedClusterID != "cluster-delete" {
		t.Fatalf("delete contract context/id = %v/%q", repo.context, repo.deletedClusterID)
	}
	if !reflect.DeepEqual(registry.unregistered, []string{"cluster-delete"}) {
		t.Fatalf("runtime unregister calls = %v", registry.unregistered)
	}
	if !reflect.DeepEqual(cache.unregistered, []string{"cluster-delete"}) {
		t.Fatalf("cache unregister calls = %v", cache.unregistered)
	}
}

func directConnection(clusterID, name string) domaincluster.Connection {
	return domaincluster.Connection{
		Summary: domaincluster.Summary{
			ID:             clusterID,
			Name:           name,
			ConnectionMode: domaincluster.ConnectionModeDirectKubeconfig,
		},
		SourceType: "api",
		Metadata: map[string]any{
			"kubeconfig": clusterID + "-config",
			"context":    clusterID + "-context",
		},
	}
}

func assertRuntimeCluster(t *testing.T, got appconfig.Cluster, want domaincluster.Connection) {
	t.Helper()
	if got.ID != want.Summary.ID || got.Name != want.Summary.Name || got.KubeconfigData != want.Metadata["kubeconfig"] || got.Context != want.Metadata["context"] {
		t.Fatalf("runtime DTO = %#v, want connection %#v", got, want)
	}
}
