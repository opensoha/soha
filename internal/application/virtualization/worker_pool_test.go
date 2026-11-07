package virtualization

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/virtualization"
	k8sinfra "github.com/opensoha/soha/internal/infrastructure/kubernetes"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type workerPoolStore struct {
	WorkerPoolRepository
	saved  domain.WorkerPool
	writes int
}

func (r *workerPoolStore) SaveWorkerPool(_ context.Context, pool domain.WorkerPool, expected int) (domain.WorkerPool, error) {
	r.writes++
	pool.Revision = expected + 1
	r.saved = pool
	return pool, nil
}

type workerPoolImageAdapter struct {
	fakeAdapter
	reads int
}

func (a *workerPoolImageAdapter) InspectWorkerTemplate(context.Context, domain.AdapterConnection, string, string, int) (string, error) {
	a.reads++
	return "frozen-template", nil
}

func TestWorkerPoolRegistrationPinsSourcesWithoutCreatingVM(t *testing.T) {
	repo := newMemoryRepo()
	connection := repo.addConnection(domain.Connection{ID: "pve", Provider: ProviderPVE, Enabled: true, VerifyTLS: true, Endpoint: "https://pve.example"})
	repo.images["image"] = domain.Image{ID: "image", Provider: ProviderPVE, ConnectionID: connection.ID, ExternalID: "101"}
	adapter := &workerPoolImageAdapter{}
	service := newTestService(repo, &captureOperations{}, adapter)
	service.permissions = appaccess.NewPermissionResolver(testRoleReader{matrix: map[string][]string{"admin": {appaccess.PermVirtualizationClustersManage, appaccess.PermVirtualizationImagesView, appaccess.PlatformActionPermission("", "Node", "create"), appaccess.PlatformActionPermission("", "Node", "view")}}})
	store := &workerPoolStore{}
	service.workerPools = store
	service.authorizeWorkerCluster = func(context.Context, domainidentity.Principal, string, bool) error { return nil }
	inspections := 0
	service.workerRuntime = &WorkerRuntime{
		ValidatePoolSpec: k8sinfra.ValidateWorkerPoolKubernetesSpec,
		InspectCluster: func(context.Context, sohaapi.VirtualizationWorkerPoolSpec) (domain.WorkerPoolIdentity, error) {
			inspections++
			return domain.WorkerPoolIdentity{ClusterUID: "cluster-uid", APIServerEndpoint: "cluster.example:6443", CAHash: "sha256:pinned"}, nil
		},
	}
	input := sohaapi.VirtualizationWorkerPoolInput{Spec: sohaapi.VirtualizationWorkerPoolSpec{Name: "workers", ConnectionID: "pve", ClusterID: "target", Owner: "soha-kubeadm", ImageID: "image", ProviderNode: "pve-a", Storage: "disk", Bridge: "vmbr0", SnippetStorage: "local", OsProfile: "ubuntu-24.04-amd64-containerd", KubernetesVersion: "v1.35.2", CPU: 4, MemoryMiB: 8192, DiskGiB: 80, MaxNodes: 2, Enabled: true, RequiredDaemonSets: []sohaapi.VirtualizationWorkerDaemonSet{{Namespace: "kube-system", Name: "network"}}}}
	id := uuid.NewString()
	saved, err := service.SaveWorkerPool(context.Background(), testPrincipal(), id, input)
	if err != nil || saved.Revision != 1 || adapter.reads != 1 || inspections != 1 || store.writes != 1 || len(repo.tasks) != 0 {
		t.Fatalf("registration did not remain configuration-only: %+v %v", saved, err)
	}
	if store.saved.Identity.ConnectionIdentity != vmCreateConnectionIdentity(connection) || store.saved.Identity.TemplateID != "101" || store.saved.Identity.TemplateFingerprint != "frozen-template" {
		t.Fatal("registration did not freeze provider identities")
	}
	paused := input
	paused.Spec.Enabled = false
	paused.ExpectedRevision = 1
	pausedPool, err := service.SaveWorkerPool(context.Background(), testPrincipal(), id, paused)
	if err != nil || pausedPool.Spec.Enabled || pausedPool.Revision != 2 || inspections != 1 || adapter.reads != 1 {
		t.Fatalf("disable required live provider access: %+v %v", pausedPool, err)
	}
	verifyInvalidWorkerPoolConfigurations(t, service, repo, connection, store, input, id)
	t.Run("registration and replay validation", func(t *testing.T) {
		for _, tt := range workerPoolValidationCases() {
			t.Run(tt.name, func(t *testing.T) {
				candidate := input
				candidate.Spec.RequiredDaemonSets = append([]sohaapi.VirtualizationWorkerDaemonSet(nil), input.Spec.RequiredDaemonSets...)
				tt.change(&candidate.Spec)
				validate := service.workerRuntime.ValidatePoolSpec
				if errors.Is(tt.want, apperrors.ErrUnsupportedOperation) {
					service.workerRuntime.ValidatePoolSpec = nil
				}
				t.Cleanup(func() { service.workerRuntime.ValidatePoolSpec = validate })
				frozen := store.saved
				frozen.Spec = candidate.Spec
				task := domain.Task{ConnectionID: frozen.Spec.ConnectionID, Payload: map[string]any{
					"workerPoolId": frozen.ID.String(), "workerPoolRevision": frozen.Revision,
					"workerPool": frozen.VirtualizationWorkerPool, "workerPoolIdentity": frozen.Identity,
				}}
				beforeWrites, beforeReads, beforeInspections := store.writes, adapter.reads, inspections
				_, registrationErr := service.SaveWorkerPool(context.Background(), testPrincipal(), uuid.NewString(), candidate)
				replayed, replayErr := service.workerPoolFromTask(task)
				if !errors.Is(registrationErr, tt.want) || !errors.Is(replayErr, tt.want) {
					t.Fatalf("registration error %v and replay error %v, want %v", registrationErr, replayErr, tt.want)
				}
				if scope := taskCapabilityScope(task); scope["workerPoolId"] != frozen.ID.String() || scope["clusterId"] != frozen.Spec.ClusterID {
					t.Fatalf("frozen identity scope lost: %v", scope)
				}
				if tt.want == nil {
					if replayed.Identity.ConnectionIdentity != frozen.Identity.ConnectionIdentity || store.writes != beforeWrites+1 {
						t.Fatal("valid registration or original replay identity was lost")
					}
				} else if store.writes != beforeWrites || adapter.reads != beforeReads || inspections != beforeInspections {
					t.Fatal("rejected spec reached a provider or pool write")
				}
				if len(repo.tasks) != 0 {
					t.Fatal("registration or replay created a VM task")
				}
			})
		}
	})
}

type workerPoolValidationCase struct {
	name   string
	change func(*sohaapi.VirtualizationWorkerPoolSpec)
	want   error
}

func workerPoolValidationCases() []workerPoolValidationCase {
	invalid := apperrors.ErrInvalidArgument
	return []workerPoolValidationCase{
		{"valid Kubernetes boundaries", func(spec *sohaapi.VirtualizationWorkerPoolSpec) {
			spec.RequiredDaemonSets[0].Namespace = strings.Repeat("n", 63)
			spec.RequiredDaemonSets[0].Name = strings.Repeat("d", 63) + ".net"
			spec.Labels = map[string]string{"example.com/" + strings.Repeat("k", 63): strings.Repeat("v", 63), "empty": ""}
		}, nil},
		{"invalid namespace", func(spec *sohaapi.VirtualizationWorkerPoolSpec) {
			spec.RequiredDaemonSets[0].Namespace = strings.Repeat("n", 64)
		}, invalid},
		{"invalid daemon name", func(spec *sohaapi.VirtualizationWorkerPoolSpec) { spec.RequiredDaemonSets[0].Name = "Bad.Name" }, invalid},
		{"invalid label key", func(spec *sohaapi.VirtualizationWorkerPoolSpec) {
			spec.Labels = map[string]string{"bad/key/extra": "ok"}
		}, invalid},
		{"invalid label value", func(spec *sohaapi.VirtualizationWorkerPoolSpec) {
			spec.Labels = map[string]string{"team": "invalid value"}
		}, invalid},
		{"reserved label prefix", func(spec *sohaapi.VirtualizationWorkerPoolSpec) {
			spec.Labels = map[string]string{"custom.k8s.io/group": "ok"}
		}, invalid},
		{"duplicate daemon", func(spec *sohaapi.VirtualizationWorkerPoolSpec) {
			spec.RequiredDaemonSets = append(spec.RequiredDaemonSets, spec.RequiredDaemonSets[0])
		}, invalid},
		{"invalid capacity", func(spec *sohaapi.VirtualizationWorkerPoolSpec) { spec.MaxNodes = 1001 }, invalid},
		{"invalid owner", func(spec *sohaapi.VirtualizationWorkerPoolSpec) { spec.Owner = "other" }, invalid},
		{"validator unavailable", func(*sohaapi.VirtualizationWorkerPoolSpec) {}, apperrors.ErrUnsupportedOperation},
	}
}

func verifyInvalidWorkerPoolConfigurations(t *testing.T, service *Service, repo *memoryRepo, connection domain.Connection, store *workerPoolStore, input sohaapi.VirtualizationWorkerPoolInput, id string) {
	t.Helper()
	for _, scenario := range []string{"cluster-permission", "scope", "cycle", "foreign-image", "invalid-version", "reserved-label"} {
		t.Run(scenario, func(t *testing.T) {
			current := input
			current.Spec.Labels = nil
			ctx := context.Background()
			savedPermissions := service.permissions
			defer func() {
				service.permissions = savedPermissions
				repo.connections[connection.ID] = connection
				repo.images["image"] = domain.Image{ID: "image", Provider: ProviderPVE, ConnectionID: connection.ID, ExternalID: "101"}
			}()
			switch scenario {
			case "cluster-permission":
				service.permissions = testPermissions()
			case "scope":
				ctx = domain.WithScopeCheck(ctx, func(map[string]string) error { return apperrors.ErrAccessDenied })
			case "cycle":
				changed := connection
				changed.Config = map[string]any{"supplyClusterIds": []string{"target"}}
				repo.connections[connection.ID] = changed
			case "foreign-image":
				repo.images["image"] = domain.Image{ID: "image", Provider: ProviderPVE, ConnectionID: "other", ExternalID: "101"}
			case "invalid-version":
				current.Spec.KubernetesVersion = "v1.35.2; unexpected"
			case "reserved-label":
				current.Spec.Labels = map[string]string{"node-role.kubernetes.io/control-plane": ""}
			}
			writes := store.writes
			if _, err := service.SaveWorkerPool(ctx, testPrincipal(), id, current); err == nil {
				t.Fatal("invalid worker pool saved")
			}
			if store.writes != writes || len(repo.tasks) != 0 {
				t.Fatal("rejected registration changed resources")
			}
		})
	}
}

func TestWorkerSupplyRefusesUnknownOrCyclicDependencies(t *testing.T) {
	for _, config := range []map[string]any{{"supplyClusterIds": "target"}, {"supplyClusterIds": nil}, {"supplyClusterIds": []any{12}}, {"nestedVirtualization": true}, {"supplyClusterIds": []string{"target"}}} {
		if err := checkWorkerSupply(domain.Connection{Provider: ProviderPVE, Enabled: true, VerifyTLS: true, Config: config}, "target"); err == nil {
			t.Fatalf("unverified supply accepted: %+v", config)
		}
	}
	if err := checkWorkerSupply(domain.Connection{Provider: ProviderKubeVirt, Enabled: true, VerifyTLS: true}, "target"); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("unsupported supply: %v", err)
	}
}
