package virtualization

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/virtualization"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type WorkerPoolRepository interface {
	GetWorkerPool(context.Context, string) (domain.WorkerPool, error)
	ListWorkerPools(context.Context, string) ([]domain.WorkerPool, error)
	SaveWorkerPool(context.Context, domain.WorkerPool, int) (domain.WorkerPool, error)
	DeleteWorkerPool(context.Context, string, int) error
	WithWorkerPoolAdmission(context.Context, string, int, string, func(context.Context) error) error
}

// Domain-owned hooks use the already configured direct Kubernetes client. No
// bootstrap credentials, kubeconfigs or shell are accepted from capability input.
type WorkerRuntime struct {
	InspectCluster   func(context.Context, sohaapi.VirtualizationWorkerPoolSpec) (domain.WorkerPoolIdentity, error)
	PrepareBootstrap func(context.Context, domain.WorkerPool, string, string, time.Time) (domain.WorkerBootstrap, error)
	ClaimNode        func(context.Context, domain.WorkerPool, string) error
	Observe          func(context.Context, domain.WorkerPool, string) (domain.WorkerObservation, error)
	RevokeBootstrap  func(context.Context, domain.WorkerPool, string, string) error
}

func workerPoolScope(pool domain.WorkerPool) map[string]string {
	scope := map[string]string{"virtualizationConnectionId": pool.Spec.ConnectionID, "clusterId": pool.Spec.ClusterID, "workerPoolId": pool.ID.String(), "workerPoolRevision": strconv.Itoa(pool.Revision)}
	if pool.Identity.ConnectionIdentity != "" {
		scope["connectionRevision"] = pool.Identity.ConnectionIdentity
	}
	return scope
}

func (s *Service) GetWorkerPool(ctx context.Context, principal domainidentity.Principal, id string) (sohaapi.VirtualizationWorkerPool, error) {
	pool, err := s.readWorkerPool(ctx, principal, id)
	return pool.VirtualizationWorkerPool, err
}

func (s *Service) readWorkerPool(ctx context.Context, principal domainidentity.Principal, id string) (domain.WorkerPool, error) {
	if err := s.authorizeWorkerPoolView(ctx, principal); err != nil {
		return domain.WorkerPool{}, err
	}
	if s.workerPools == nil {
		return domain.WorkerPool{}, apperrors.ErrUnsupportedOperation
	}
	if _, err := uuid.Parse(id); err != nil {
		return domain.WorkerPool{}, apperrors.ErrInvalidArgument
	}
	pool, err := s.workerPools.GetWorkerPool(ctx, id)
	if err != nil {
		return pool, err
	}
	if err := s.checkWorkerClusterAccess(ctx, principal, pool.Spec.ClusterID, false); err != nil {
		return domain.WorkerPool{}, err
	}
	return pool, domain.CheckScope(ctx, workerPoolScope(pool))
}

func (s *Service) checkWorkerClusterAccess(ctx context.Context, principal domainidentity.Principal, clusterID string, mutate bool) error {
	if s.authorizeWorkerCluster == nil {
		return fmt.Errorf("%w: worker cluster authorization is unavailable", apperrors.ErrAccessDenied)
	}
	return s.authorizeWorkerCluster(ctx, principal, clusterID, mutate)
}

func (s *Service) authorizeWorkerPoolView(ctx context.Context, principal domainidentity.Principal) error {
	for _, permission := range []string{appaccess.PermVirtualizationClustersView, appaccess.PermPlatformClustersView, appaccess.PlatformActionPermission("", "Node", "view")} {
		if err := s.authorize(ctx, principal, permission); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) ListWorkerPools(ctx context.Context, principal domainidentity.Principal, connectionID string) ([]sohaapi.VirtualizationWorkerPool, error) {
	if err := s.authorizeWorkerPoolView(ctx, principal); err != nil {
		return nil, err
	}
	if s.workerPools == nil {
		return nil, apperrors.ErrUnsupportedOperation
	}
	connection, err := s.connections.GetConnection(ctx, strings.TrimSpace(connectionID))
	if err != nil {
		return nil, err
	}
	if err := domain.CheckScope(ctx, connectionCapabilityScope(connection, "")); err != nil {
		return nil, err
	}
	pools, err := s.workerPools.ListWorkerPools(ctx, connection.ID)
	if err != nil {
		return nil, err
	}
	items := make([]sohaapi.VirtualizationWorkerPool, 0, len(pools))
	for _, pool := range pools {
		if err := s.checkWorkerClusterAccess(ctx, principal, pool.Spec.ClusterID, false); err != nil {
			return nil, err
		}
		if err := domain.CheckScope(ctx, workerPoolScope(pool)); err != nil {
			return nil, err
		}
		items = append(items, pool.VirtualizationWorkerPool)
	}
	return items, nil
}

func (s *Service) SaveWorkerPool(ctx context.Context, principal domainidentity.Principal, id string, input sohaapi.VirtualizationWorkerPoolInput) (_ sohaapi.VirtualizationWorkerPool, retErr error) {
	defer func() {
		s.recordMutationFailure(ctx, principal, "virtualization.worker_pool.save", id, input.Spec.Name, retErr, nil)
	}()
	action := "update"
	if input.ExpectedRevision == 0 {
		action = "create"
	}
	for _, permission := range []string{appaccess.ManagedActionPermission(appaccess.PermVirtualizationClustersManage, action), appaccess.PlatformActionPermission("", "Node", "create"), appaccess.PermVirtualizationImagesView} {
		if err := s.authorize(ctx, principal, permission); err != nil {
			return sohaapi.VirtualizationWorkerPool{}, err
		}
	}
	poolID, err := uuid.Parse(id)
	if err != nil || poolID == uuid.Nil || input.ExpectedRevision < 0 {
		return sohaapi.VirtualizationWorkerPool{}, apperrors.ErrInvalidArgument
	}
	if err := domain.ValidateWorkerPoolSpec(input.Spec); err != nil {
		return sohaapi.VirtualizationWorkerPool{}, err
	}
	if s.workerPools == nil || s.workerRuntime == nil || s.workerRuntime.InspectCluster == nil {
		return sohaapi.VirtualizationWorkerPool{}, apperrors.ErrUnsupportedOperation
	}
	if err := s.checkWorkerClusterAccess(ctx, principal, input.Spec.ClusterID, true); err != nil {
		return sohaapi.VirtualizationWorkerPool{}, err
	}
	pool := domain.WorkerPool{VirtualizationWorkerPool: sohaapi.VirtualizationWorkerPool{ID: poolID, Revision: input.ExpectedRevision, Spec: input.Spec}}
	if err := domain.CheckScope(ctx, workerPoolScope(pool)); err != nil {
		return pool.VirtualizationWorkerPool, err
	}
	if !input.Spec.Enabled && input.ExpectedRevision > 0 {
		// Stopping supply must remain possible while the provider or cluster is down.
		// Re-enabling always re-inspects and freezes the current target identities.
		current, err := s.workerPools.GetWorkerPool(ctx, id)
		if err != nil {
			return pool.VirtualizationWorkerPool, err
		}
		pool.Identity = current.Identity
	} else {
		ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
		defer cancel()
		connection, templateID, fingerprint, err := s.inspectWorkerImage(ctx, input.Spec)
		if err != nil {
			return pool.VirtualizationWorkerPool, err
		}
		pool.Identity, err = s.workerRuntime.InspectCluster(ctx, input.Spec)
		if err != nil {
			return pool.VirtualizationWorkerPool, err
		}
		pool.Identity.ConnectionIdentity = vmCreateConnectionIdentity(connection)
		pool.Identity.TemplateID, pool.Identity.TemplateFingerprint = templateID, fingerprint
	}
	if err := domain.CheckScope(ctx, workerPoolScope(pool)); err != nil {
		return pool.VirtualizationWorkerPool, err
	}
	saved, err := s.workerPools.SaveWorkerPool(ctx, pool, input.ExpectedRevision)
	if err == nil {
		s.recordOperation(ctx, principal, "virtualization.worker_pool."+action, saved.ID.String(), saved.Spec.Name, "success", "saved worker supply owner and bounded configuration", map[string]any{"clusterId": saved.Spec.ClusterID, "connectionId": saved.Spec.ConnectionID, "revision": saved.Revision})
	}
	return saved.VirtualizationWorkerPool, err
}

func (s *Service) DeleteWorkerPool(ctx context.Context, principal domainidentity.Principal, id string, revision int) (retErr error) {
	defer func() {
		s.recordMutationFailure(ctx, principal, "virtualization.worker_pool.delete", id, "", retErr, nil)
	}()
	for _, permission := range []string{appaccess.ManagedActionPermission(appaccess.PermVirtualizationClustersManage, "delete"), appaccess.PlatformActionPermission("", "Node", "create")} {
		if err := s.authorize(ctx, principal, permission); err != nil {
			return err
		}
	}
	if revision < 1 {
		return apperrors.ErrInvalidArgument
	}
	pool, err := s.readWorkerPool(ctx, principal, id)
	if err != nil {
		return err
	}
	if err := s.checkWorkerClusterAccess(ctx, principal, pool.Spec.ClusterID, true); err != nil {
		return err
	}
	if err := s.workerPools.DeleteWorkerPool(ctx, pool.ID.String(), revision); err != nil {
		return err
	}
	s.recordOperation(ctx, principal, "virtualization.worker_pool.delete", id, pool.Spec.Name, "success", "deleted unused worker pool configuration", nil)
	return nil
}

func (s *Service) inspectWorkerImage(ctx context.Context, spec sohaapi.VirtualizationWorkerPoolSpec) (domain.Connection, string, string, error) {
	connection, err := s.connections.GetConnection(ctx, spec.ConnectionID)
	if err != nil {
		return connection, "", "", err
	}
	if err := checkWorkerSupply(connection, spec.ClusterID); err != nil {
		return connection, "", "", err
	}
	image, err := s.images.GetImage(ctx, spec.ImageID)
	if err != nil {
		return connection, "", "", err
	}
	if image.ConnectionID != connection.ID || image.Provider != ProviderPVE {
		return connection, "", "", fmt.Errorf("%w: worker image does not belong to the pool provider", apperrors.ErrInvalidArgument)
	}
	templateID := firstNonEmpty(stringValue(image.Config, "sourceRef"), image.ExternalID)
	if number, err := strconv.Atoi(templateID); err != nil || number < 100 {
		return connection, "", "", fmt.Errorf("%w: worker image must refer to an existing PVE template VMID", apperrors.ErrInvalidArgument)
	}
	adapter, providerConnection, err := s.adapterForConnection(ctx, connection)
	if err != nil {
		return connection, "", "", err
	}
	inspector, ok := adapter.(interface {
		InspectWorkerTemplate(context.Context, domain.AdapterConnection, string, string, int) (string, error)
	})
	if !ok {
		return connection, "", "", apperrors.ErrUnsupportedOperation
	}
	fingerprint, err := inspector.InspectWorkerTemplate(ctx, providerConnection, spec.ProviderNode, templateID, spec.DiskGiB)
	return connection, templateID, fingerprint, err
}

func checkWorkerSupply(connection domain.Connection, clusterID string) error {
	if connection.Provider != ProviderPVE || !connection.Enabled || !connection.VerifyTLS {
		return fmt.Errorf("%w: worker supply requires an enabled independent PVE connection with verified TLS", apperrors.ErrInvalidArgument)
	}
	if connection.KubernetesClusterID != "" || boolValue(connection.Config, "nestedVirtualization") {
		return fmt.Errorf("%w: nested Kubernetes-backed supply needs a supported independent provider path", apperrors.ErrConflict)
	}
	if dependencies, ok := connection.Config["supplyClusterIds"]; ok {
		raw, err := json.Marshal(dependencies)
		var clusterIDs []string
		if err != nil || json.Unmarshal(raw, &clusterIDs) != nil || dependencies == nil {
			return fmt.Errorf("%w: provider supply dependencies must be explicit cluster IDs", apperrors.ErrInvalidArgument)
		}
		if slices.Contains(clusterIDs, clusterID) {
			return fmt.Errorf("%w: worker pool cannot supply the cluster hosting its own provider", apperrors.ErrConflict)
		}
	}
	return nil
}

func (s *Service) authorizeWorkerTaskRead(ctx context.Context, principal domainidentity.Principal, task domain.Task) error {
	if !isWorkerCreation(task) {
		return nil
	}
	if err := s.authorizeWorkerPoolView(ctx, principal); err != nil {
		return err
	}
	pool, err := workerPoolFromTask(task)
	if err != nil {
		return err
	}
	return s.checkWorkerClusterAccess(ctx, principal, pool.Spec.ClusterID, false)
}
