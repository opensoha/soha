package resource

import (
	"context"
	"fmt"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type workloadListSpec[T any] struct {
	kind        string
	auditText   string
	agent       func(WorkloadAgent) ([]T, error)
	direct      func() ([]T, string, error)
	namespaceOf func(T) string
	populate    func([]T, domainaccess.Decision)
}

func listWorkloadResources[T any](ctx context.Context, w *Workloads, principal domainidentity.Principal, clusterID, namespace string, spec workloadListSpec[T]) ([]T, error) {
	connection, decision, err := w.authorize(ctx, principal, clusterID, namespace, spec.kind, domainaccess.ActionList)
	if err != nil {
		return nil, err
	}
	items, source, err := routeWorkload(ctx, w, connection, spec.agent, spec.direct)
	if err != nil {
		return nil, err
	}
	items = filterScopedNamespaceItems(items, decision, spec.namespaceOf)
	spec.populate(items, decision)
	_ = w.recordAudit(ctx, principal, connection.Summary.ID, namespace, spec.kind, "", string(domainaccess.ActionList), "success",
		fmt.Sprintf("%s via %s in namespace %s", spec.auditText, source, displayNamespace(namespace)))
	return items, nil
}

type workloadGetSpec[T any] struct {
	kind      string
	auditText string
	agent     func(WorkloadAgent) (T, error)
	direct    func() (T, string, error)
	finalize  func(*T, domainaccess.Decision)
}

func getWorkloadResource[T any](ctx context.Context, w *Workloads, principal domainidentity.Principal, clusterID, namespace, name string, spec workloadGetSpec[T]) (T, error) {
	var zero T
	connection, decision, err := w.authorize(ctx, principal, clusterID, namespace, spec.kind, domainaccess.ActionView)
	if err != nil {
		return zero, err
	}
	item, source, err := routeWorkload(ctx, w, connection, spec.agent, spec.direct)
	if err != nil {
		return zero, err
	}
	filterWorkloadDetailRelations(ctx, w.resourceAccess, principal, clusterID, namespace, &item)
	if spec.finalize != nil {
		spec.finalize(&item, decision)
	}
	_ = w.recordAudit(ctx, principal, connection.Summary.ID, namespace, spec.kind, name, string(domainaccess.ActionView), "success",
		fmt.Sprintf("%s via %s in namespace %s", spec.auditText, source, displayNamespace(namespace)))
	return item, nil
}

func filterWorkloadDetailRelations[T any](ctx context.Context, access *resourceAccess, principal domainidentity.Principal, clusterID, namespace string, item *T) {
	var pods *[]domainresource.PodView
	var jobs *[]domainresource.JobView
	var related *[]domainresource.WorkloadRelationView
	switch detail := any(item).(type) {
	case *domainresource.DeploymentDetailView:
		pods, related = &detail.Pods, &detail.RelatedResources
	case *domainresource.StatefulSetDetailView:
		pods, related = &detail.Pods, &detail.RelatedResources
	case *domainresource.DaemonSetDetailView:
		pods, related = &detail.Pods, &detail.RelatedResources
	case *domainresource.JobDetailView:
		pods, related = &detail.Pods, &detail.RelatedResources
	case *domainresource.CronJobDetailView:
		jobs, related = &detail.Jobs, &detail.RelatedResources
	case *domainresource.ReplicaSetDetailView:
		pods, related = &detail.Pods, &detail.RelatedResources
	case *domainresource.ReplicationControllerDetailView:
		pods, related = &detail.Pods, &detail.RelatedResources
	default:
		return
	}

	decisions := make(map[string]bool)
	canView := func(itemNamespace, kind string) bool {
		if itemNamespace == "" {
			itemNamespace = namespace
		}
		key := itemNamespace + "\x00" + kind
		allowed, ok := decisions[key]
		if !ok {
			allowed = canViewRelatedResource(ctx, access, principal, clusterID, itemNamespace, kind)
			decisions[key] = allowed
		}
		return allowed
	}
	if pods != nil {
		*pods = filterRelatedItems(*pods, func(item domainresource.PodView) bool {
			return canView(item.Namespace, "Pod")
		})
	}
	if jobs != nil {
		*jobs = filterRelatedItems(*jobs, func(item domainresource.JobView) bool {
			return canView(item.Namespace, "Job")
		})
	}
	*related = filterRelatedItems(*related, func(item domainresource.WorkloadRelationView) bool {
		return canView(item.Namespace, item.Kind)
	})
}

func filterRelatedItems[T any](items []T, keep func(T) bool) []T {
	filtered := make([]T, 0, len(items))
	for _, item := range items {
		if keep(item) {
			filtered = append(filtered, item)
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	return filtered
}

func routeWorkload[T any](ctx context.Context, w *Workloads, connection domaincluster.Connection, agent func(WorkloadAgent) (T, error), direct func() (T, string, error)) (T, string, error) {
	var zero T
	if connection.Summary.ConnectionMode != domaincluster.ConnectionModeAgent {
		return direct()
	}
	client, err := w.workloadAgentClient(connection)
	if err != nil {
		return zero, "", err
	}
	item, err := agent(client)
	if err != nil {
		return zero, "agent", fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	return item, "agent", nil
}

func liveWorkload[T any](load func() (T, error)) (T, string, error) {
	item, err := load()
	return item, "live", err
}

type workloadMutationSpec struct {
	permission       string
	kind             string
	action           domainaccess.Action
	agent            func(WorkloadAgent) error
	direct           func() error
	successMessage   func(string) string
	auditErrorPrefix string
	operation        string
	attributes       map[string]any
}

func performWorkloadMutation(ctx context.Context, w *Workloads, principal domainidentity.Principal, clusterID, namespace, name string, spec workloadMutationSpec) error {
	if err := w.authorizeDeploymentPermission(ctx, principal, spec.permission); err != nil {
		return err
	}
	connection, _, err := w.authorize(ctx, principal, clusterID, namespace, spec.kind, spec.action)
	if err != nil {
		return err
	}
	source := "direct"
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		client, err := w.workloadAgentClient(connection)
		if err != nil {
			return err
		}
		err = spec.agent(client)
		source = "agent"
		if err != nil {
			_ = w.recordAudit(ctx, principal, clusterID, namespace, spec.kind, name, string(spec.action), "failure", err.Error())
			return fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
	} else if err := spec.direct(); err != nil {
		_ = w.recordAudit(ctx, principal, clusterID, namespace, spec.kind, name, string(spec.action), "failure", err.Error())
		return err
	}
	message := spec.successMessage(source)
	if err := w.recordAudit(ctx, principal, clusterID, namespace, spec.kind, name, string(spec.action), "success", message); err != nil {
		return fmt.Errorf("%s: %w", spec.auditErrorPrefix, err)
	}
	w.recordOperation(ctx, principal, spec.operation, clusterID, namespace, spec.kind, name, message, spec.attributes)
	return nil
}
