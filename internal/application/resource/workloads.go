package resource

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/yaml"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	informerinfra "github.com/opensoha/soha/internal/infrastructure/informer"
	k8sinfra "github.com/opensoha/soha/internal/infrastructure/kubernetes"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) ListDeployments(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.DeploymentView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "Deployment", domainaccess.ActionList)
	if err != nil {
		return nil, err
	}

	var (
		items  []domainresource.DeploymentView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return nil, err
		}
		items, err = client.ListDeployments(ctx, namespace)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItems, rawSource, err := s.listDirectDeployments(ctx, clusterID, namespace)
		if err != nil {
			return nil, err
		}
		items = make([]domainresource.DeploymentView, 0, len(rawItems))
		for _, item := range rawItems {
			items = append(items, mapDeployment(item, decision))
		}
		source = rawSource
	}
	items = filterScopedNamespaceItems(items, decision, func(item domainresource.DeploymentView) string { return item.Namespace })
	populateAllowedActionsDeployments(items, decision)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Deployment", "", string(domainaccess.ActionList), "success", fmt.Sprintf("listed deployments via %s in namespace %s", source, displayNamespace(namespace)))
	return items, nil
}
func (s *Service) GetDeploymentDetail(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.DeploymentDetailView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "Deployment", domainaccess.ActionView)
	if err != nil {
		return domainresource.DeploymentDetailView{}, err
	}

	var (
		item   domainresource.DeploymentDetailView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return domainresource.DeploymentDetailView{}, err
		}
		item, err = client.GetDeploymentDetail(ctx, namespace, name)
		if err != nil {
			return domainresource.DeploymentDetailView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItem, err := s.getDirectDeployment(ctx, clusterID, namespace, name)
		if err != nil {
			return domainresource.DeploymentDetailView{}, err
		}
		item = mapDeploymentDetail(*rawItem, decision)
		source = "live"
	}
	item.AllowedActions = stringifyActions(decision.AllowedActions)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Deployment", name, string(domainaccess.ActionView), "success", fmt.Sprintf("viewed deployment detail via %s in namespace %s", source, displayNamespace(namespace)))
	return item, nil
}
func (s *Service) GetDeploymentYAML(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.ResourceYAMLView, error) {
	connection, _, err := s.authorize(ctx, principal, clusterID, namespace, "Deployment", domainaccess.ActionView)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	var (
		item   domainresource.ResourceYAMLView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return domainresource.ResourceYAMLView{}, err
		}
		item, err = client.GetDeploymentYAML(ctx, namespace, name)
		if err != nil {
			return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		item, err = s.getDirectDeploymentYAML(ctx, clusterID, namespace, name)
		if err != nil {
			return domainresource.ResourceYAMLView{}, err
		}
		source = "live"
	}
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Deployment", name, string(domainaccess.ActionView), "success", fmt.Sprintf("viewed deployment yaml via %s in namespace %s", source, displayNamespace(namespace)))
	return item, nil
}
func (s *Service) ApplyDeploymentYAML(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name, content string) (domainresource.ResourceYAMLView, error) {
	return s.applyResourceYAML(ctx, principal, clusterID, namespace, "Deployment", name, content)
}
func (s *Service) GetStatefulSetDetail(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.StatefulSetDetailView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "StatefulSet", domainaccess.ActionView)
	if err != nil {
		return domainresource.StatefulSetDetailView{}, err
	}
	var (
		item   domainresource.StatefulSetDetailView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return domainresource.StatefulSetDetailView{}, err
		}
		item, err = client.GetStatefulSetDetail(ctx, namespace, name)
		if err != nil {
			return domainresource.StatefulSetDetailView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItem, err := s.getDirectStatefulSet(ctx, clusterID, namespace, name)
		if err != nil {
			return domainresource.StatefulSetDetailView{}, err
		}
		item = mapStatefulSetDetail(*rawItem, decision)
		source = "live"
	}
	item.AllowedActions = stringifyActions(decision.AllowedActions)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "StatefulSet", name, string(domainaccess.ActionView), "success", fmt.Sprintf("viewed statefulset detail via %s in namespace %s", source, displayNamespace(namespace)))
	return item, nil
}
func (s *Service) GetStatefulSetYAML(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.ResourceYAMLView, error) {
	connection, _, err := s.authorize(ctx, principal, clusterID, namespace, "StatefulSet", domainaccess.ActionView)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	var (
		item   domainresource.ResourceYAMLView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return domainresource.ResourceYAMLView{}, err
		}
		item, err = client.GetStatefulSetYAML(ctx, namespace, name)
		if err != nil {
			return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		item, err = s.getDirectStatefulSetYAML(ctx, clusterID, namespace, name)
		if err != nil {
			return domainresource.ResourceYAMLView{}, err
		}
		source = "live"
	}
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "StatefulSet", name, string(domainaccess.ActionView), "success", fmt.Sprintf("viewed statefulset yaml via %s in namespace %s", source, displayNamespace(namespace)))
	return item, nil
}
func (s *Service) ApplyStatefulSetYAML(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name, content string) (domainresource.ResourceYAMLView, error) {
	return s.applyResourceYAML(ctx, principal, clusterID, namespace, "StatefulSet", name, content)
}
func (s *Service) GetDaemonSetDetail(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.DaemonSetDetailView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "DaemonSet", domainaccess.ActionView)
	if err != nil {
		return domainresource.DaemonSetDetailView{}, err
	}
	var (
		item   domainresource.DaemonSetDetailView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return domainresource.DaemonSetDetailView{}, err
		}
		item, err = client.GetDaemonSetDetail(ctx, namespace, name)
		if err != nil {
			return domainresource.DaemonSetDetailView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItem, err := s.getDirectDaemonSet(ctx, clusterID, namespace, name)
		if err != nil {
			return domainresource.DaemonSetDetailView{}, err
		}
		item = mapDaemonSetDetail(*rawItem, decision)
		source = "live"
	}
	item.AllowedActions = stringifyActions(decision.AllowedActions)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "DaemonSet", name, string(domainaccess.ActionView), "success", fmt.Sprintf("viewed daemonset detail via %s in namespace %s", source, displayNamespace(namespace)))
	return item, nil
}
func (s *Service) GetDaemonSetYAML(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.ResourceYAMLView, error) {
	connection, _, err := s.authorize(ctx, principal, clusterID, namespace, "DaemonSet", domainaccess.ActionView)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	var (
		item   domainresource.ResourceYAMLView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return domainresource.ResourceYAMLView{}, err
		}
		item, err = client.GetDaemonSetYAML(ctx, namespace, name)
		if err != nil {
			return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		item, err = s.getDirectDaemonSetYAML(ctx, clusterID, namespace, name)
		if err != nil {
			return domainresource.ResourceYAMLView{}, err
		}
		source = "live"
	}
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "DaemonSet", name, string(domainaccess.ActionView), "success", fmt.Sprintf("viewed daemonset yaml via %s in namespace %s", source, displayNamespace(namespace)))
	return item, nil
}
func (s *Service) ApplyDaemonSetYAML(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name, content string) (domainresource.ResourceYAMLView, error) {
	return s.applyResourceYAML(ctx, principal, clusterID, namespace, "DaemonSet", name, content)
}
func (s *Service) GetJobDetail(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.JobDetailView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "Job", domainaccess.ActionView)
	if err != nil {
		return domainresource.JobDetailView{}, err
	}
	var (
		item   domainresource.JobDetailView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return domainresource.JobDetailView{}, err
		}
		item, err = client.GetJobDetail(ctx, namespace, name)
		if err != nil {
			return domainresource.JobDetailView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItem, err := s.getDirectJob(ctx, clusterID, namespace, name)
		if err != nil {
			return domainresource.JobDetailView{}, err
		}
		item = mapJobDetail(*rawItem, decision)
		source = "live"
	}
	item.AllowedActions = stringifyActions(decision.AllowedActions)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Job", name, string(domainaccess.ActionView), "success", fmt.Sprintf("viewed job detail via %s in namespace %s", source, displayNamespace(namespace)))
	return item, nil
}
func (s *Service) GetJobYAML(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.ResourceYAMLView, error) {
	connection, _, err := s.authorize(ctx, principal, clusterID, namespace, "Job", domainaccess.ActionView)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	var (
		item   domainresource.ResourceYAMLView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return domainresource.ResourceYAMLView{}, err
		}
		item, err = client.GetJobYAML(ctx, namespace, name)
		if err != nil {
			return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		item, err = s.getDirectJobYAML(ctx, clusterID, namespace, name)
		if err != nil {
			return domainresource.ResourceYAMLView{}, err
		}
		source = "live"
	}
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Job", name, string(domainaccess.ActionView), "success", fmt.Sprintf("viewed job yaml via %s in namespace %s", source, displayNamespace(namespace)))
	return item, nil
}
func (s *Service) ApplyJobYAML(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name, content string) (domainresource.ResourceYAMLView, error) {
	return s.applyResourceYAML(ctx, principal, clusterID, namespace, "Job", name, content)
}
func (s *Service) GetCronJobDetail(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.CronJobDetailView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "CronJob", domainaccess.ActionView)
	if err != nil {
		return domainresource.CronJobDetailView{}, err
	}
	var (
		item   domainresource.CronJobDetailView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return domainresource.CronJobDetailView{}, err
		}
		item, err = client.GetCronJobDetail(ctx, namespace, name)
		if err != nil {
			return domainresource.CronJobDetailView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItem, err := s.getDirectCronJob(ctx, clusterID, namespace, name)
		if err != nil {
			return domainresource.CronJobDetailView{}, err
		}
		item = mapCronJobDetail(*rawItem, decision)
		source = "live"
	}
	item.AllowedActions = stringifyActions(decision.AllowedActions)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "CronJob", name, string(domainaccess.ActionView), "success", fmt.Sprintf("viewed cronjob detail via %s in namespace %s", source, displayNamespace(namespace)))
	return item, nil
}
func (s *Service) GetCronJobYAML(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.ResourceYAMLView, error) {
	connection, _, err := s.authorize(ctx, principal, clusterID, namespace, "CronJob", domainaccess.ActionView)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	var (
		item   domainresource.ResourceYAMLView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return domainresource.ResourceYAMLView{}, err
		}
		item, err = client.GetCronJobYAML(ctx, namespace, name)
		if err != nil {
			return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		item, err = s.getDirectCronJobYAML(ctx, clusterID, namespace, name)
		if err != nil {
			return domainresource.ResourceYAMLView{}, err
		}
		source = "live"
	}
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "CronJob", name, string(domainaccess.ActionView), "success", fmt.Sprintf("viewed cronjob yaml via %s in namespace %s", source, displayNamespace(namespace)))
	return item, nil
}
func (s *Service) ApplyCronJobYAML(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name, content string) (domainresource.ResourceYAMLView, error) {
	return s.applyResourceYAML(ctx, principal, clusterID, namespace, "CronJob", name, content)
}
func (s *Service) GetDeploymentRolloutStatus(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.DeploymentRolloutStatusView, error) {
	connection, _, err := s.authorize(ctx, principal, clusterID, namespace, "Deployment", domainaccess.ActionView)
	if err != nil {
		return domainresource.DeploymentRolloutStatusView{}, err
	}

	var (
		item   domainresource.DeploymentRolloutStatusView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return domainresource.DeploymentRolloutStatusView{}, err
		}
		item, err = client.GetDeploymentRolloutStatus(ctx, namespace, name)
		if err != nil {
			return domainresource.DeploymentRolloutStatusView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		item, err = s.getDirectDeploymentRolloutStatus(ctx, clusterID, namespace, name)
		if err != nil {
			return domainresource.DeploymentRolloutStatusView{}, err
		}
		source = "live"
	}
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Deployment", name, string(domainaccess.ActionView), "success", fmt.Sprintf("viewed deployment rollout status via %s in namespace %s", source, displayNamespace(namespace)))
	return item, nil
}
func (s *Service) ListDeploymentRolloutHistory(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) ([]domainresource.RolloutHistoryView, error) {
	connection, _, err := s.authorize(ctx, principal, clusterID, namespace, "Deployment", domainaccess.ActionView)
	if err != nil {
		return nil, err
	}

	var (
		items  []domainresource.RolloutHistoryView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return nil, err
		}
		items, err = client.ListDeploymentRolloutHistory(ctx, namespace, name)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		items, err = s.listDirectDeploymentRolloutHistory(ctx, clusterID, namespace, name)
		if err != nil {
			return nil, err
		}
		source = "live"
	}
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Deployment", name, string(domainaccess.ActionView), "success", fmt.Sprintf("listed deployment rollout history via %s in namespace %s", source, displayNamespace(namespace)))
	return items, nil
}
func (s *Service) RollbackDeployment(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name, revision string) (domainresource.DeploymentRollbackView, error) {
	if err := s.authorizeDeploymentPermission(ctx, principal, appaccess.PermPlatformDeploymentRollback); err != nil {
		return domainresource.DeploymentRollbackView{}, err
	}
	connection, _, err := s.authorize(ctx, principal, clusterID, namespace, "Deployment", domainaccess.ActionRollback)
	if err != nil {
		return domainresource.DeploymentRollbackView{}, err
	}
	revision = strings.TrimSpace(revision)
	if revision == "" {
		return domainresource.DeploymentRollbackView{}, fmt.Errorf("%w: revision is required", apperrors.ErrInvalidArgument)
	}

	var source string
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return domainresource.DeploymentRollbackView{}, err
		}
		if err := client.RollbackDeployment(ctx, namespace, name, revision); err != nil {
			_ = s.recordAudit(ctx, principal, clusterID, namespace, "Deployment", name, string(domainaccess.ActionRollback), "failure", err.Error())
			return domainresource.DeploymentRollbackView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		if err := s.rollbackDirectDeployment(ctx, clusterID, namespace, name, revision); err != nil {
			_ = s.recordAudit(ctx, principal, clusterID, namespace, "Deployment", name, string(domainaccess.ActionRollback), "failure", err.Error())
			return domainresource.DeploymentRollbackView{}, err
		}
		source = "live"
	}

	now := time.Now().UTC().Format(time.RFC3339)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Deployment", name, string(domainaccess.ActionRollback), "success", fmt.Sprintf("rolled back deployment to revision %s via %s", revision, source))
	s.recordOperation(ctx, principal, "platform.deployment.rollback", connection.Summary.ID, namespace, "Deployment", name, fmt.Sprintf("rolled back deployment to revision %s via %s", revision, source), map[string]any{"revision": revision})
	return domainresource.DeploymentRollbackView{
		Name:           name,
		Namespace:      namespace,
		TargetRevision: revision,
		Message:        fmt.Sprintf("Rollback to revision %s requested.", revision),
		RequestedAt:    now,
	}, nil
}
func (s *Service) ListStatefulSets(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.StatefulSetView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "StatefulSet", domainaccess.ActionList)
	if err != nil {
		return nil, err
	}
	var (
		items  []domainresource.StatefulSetView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return nil, err
		}
		items, err = client.ListStatefulSets(ctx, namespace)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItems, rawSource, err := s.listDirectStatefulSets(ctx, clusterID, namespace)
		if err != nil {
			return nil, err
		}
		items = make([]domainresource.StatefulSetView, 0, len(rawItems))
		for _, item := range rawItems {
			items = append(items, mapStatefulSet(item, decision))
		}
		source = rawSource
	}
	items = filterScopedNamespaceItems(items, decision, func(item domainresource.StatefulSetView) string { return item.Namespace })
	populateAllowedActionsStatefulSets(items, decision)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "StatefulSet", "", string(domainaccess.ActionList), "success", fmt.Sprintf("listed statefulsets via %s in namespace %s", source, displayNamespace(namespace)))
	return items, nil
}
func (s *Service) ListDaemonSets(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.DaemonSetView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "DaemonSet", domainaccess.ActionList)
	if err != nil {
		return nil, err
	}
	var (
		items  []domainresource.DaemonSetView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return nil, err
		}
		items, err = client.ListDaemonSets(ctx, namespace)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItems, err := s.listDirectDaemonSets(ctx, clusterID, namespace)
		if err != nil {
			return nil, err
		}
		items = make([]domainresource.DaemonSetView, 0, len(rawItems))
		for _, item := range rawItems {
			items = append(items, mapDaemonSet(item, decision))
		}
		source = "live"
	}
	items = filterScopedNamespaceItems(items, decision, func(item domainresource.DaemonSetView) string { return item.Namespace })
	populateAllowedActionsDaemonSets(items, decision)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "DaemonSet", "", string(domainaccess.ActionList), "success", fmt.Sprintf("listed daemonsets via %s in namespace %s", source, displayNamespace(namespace)))
	return items, nil
}
func (s *Service) ListJobs(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.JobView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "Job", domainaccess.ActionList)
	if err != nil {
		return nil, err
	}
	var (
		items  []domainresource.JobView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return nil, err
		}
		items, err = client.ListJobs(ctx, namespace)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItems, err := s.listDirectJobs(ctx, clusterID, namespace)
		if err != nil {
			return nil, err
		}
		items = make([]domainresource.JobView, 0, len(rawItems))
		for _, item := range rawItems {
			items = append(items, mapJob(item, decision))
		}
		source = "live"
	}
	items = filterScopedNamespaceItems(items, decision, func(item domainresource.JobView) string { return item.Namespace })
	populateAllowedActionsJobs(items, decision)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Job", "", string(domainaccess.ActionList), "success", fmt.Sprintf("listed jobs via %s in namespace %s", source, displayNamespace(namespace)))
	return items, nil
}
func (s *Service) ListCronJobs(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.CronJobView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "CronJob", domainaccess.ActionList)
	if err != nil {
		return nil, err
	}
	var (
		items  []domainresource.CronJobView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return nil, err
		}
		items, err = client.ListCronJobs(ctx, namespace)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItems, err := s.listDirectCronJobs(ctx, clusterID, namespace)
		if err != nil {
			return nil, err
		}
		items = make([]domainresource.CronJobView, 0, len(rawItems))
		for _, item := range rawItems {
			items = append(items, mapCronJob(item, decision))
		}
		source = "live"
	}
	items = filterScopedNamespaceItems(items, decision, func(item domainresource.CronJobView) string { return item.Namespace })
	populateAllowedActionsCronJobs(items, decision)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "CronJob", "", string(domainaccess.ActionList), "success", fmt.Sprintf("listed cronjobs via %s in namespace %s", source, displayNamespace(namespace)))
	return items, nil
}
func (s *Service) ListReplicaSets(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.ReplicaSetView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "ReplicaSet", domainaccess.ActionList)
	if err != nil {
		return nil, err
	}
	var (
		items  []domainresource.ReplicaSetView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return nil, err
		}
		items, err = client.ListReplicaSets(ctx, namespace)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItems, err := s.listDirectReplicaSets(ctx, clusterID, namespace)
		if err != nil {
			return nil, err
		}
		items = make([]domainresource.ReplicaSetView, 0, len(rawItems))
		for _, item := range rawItems {
			items = append(items, mapReplicaSet(item, decision))
		}
		source = "live"
	}
	items = filterScopedNamespaceItems(items, decision, func(item domainresource.ReplicaSetView) string { return item.Namespace })
	populateAllowedActionsReplicaSets(items, decision)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "ReplicaSet", "", string(domainaccess.ActionList), "success", fmt.Sprintf("listed replicasets via %s in namespace %s", source, displayNamespace(namespace)))
	return items, nil
}
func (s *Service) ListHorizontalPodAutoscalers(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.HorizontalPodAutoscalerView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "HorizontalPodAutoscaler", domainaccess.ActionList)
	if err != nil {
		return nil, err
	}
	var (
		items  []domainresource.HorizontalPodAutoscalerView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return nil, err
		}
		items, err = client.ListHorizontalPodAutoscalers(ctx, namespace)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItems, err := s.listDirectHorizontalPodAutoscalers(ctx, clusterID, namespace)
		if err != nil {
			return nil, err
		}
		items = make([]domainresource.HorizontalPodAutoscalerView, 0, len(rawItems))
		for _, item := range rawItems {
			items = append(items, mapHorizontalPodAutoscaler(item, decision))
		}
		source = "live"
	}
	items = filterScopedNamespaceItems(items, decision, func(item domainresource.HorizontalPodAutoscalerView) string { return item.Namespace })
	populateAllowedActionsHorizontalPodAutoscalers(items, decision)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "HorizontalPodAutoscaler", "", string(domainaccess.ActionList), "success", fmt.Sprintf("listed hpas via %s in namespace %s", source, displayNamespace(namespace)))
	return items, nil
}
func (s *Service) ListPodDisruptionBudgets(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.PodDisruptionBudgetView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "PodDisruptionBudget", domainaccess.ActionList)
	if err != nil {
		return nil, err
	}
	var (
		items  []domainresource.PodDisruptionBudgetView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return nil, err
		}
		items, err = client.ListPodDisruptionBudgets(ctx, namespace)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItems, err := s.listDirectPodDisruptionBudgets(ctx, clusterID, namespace)
		if err != nil {
			return nil, err
		}
		items = make([]domainresource.PodDisruptionBudgetView, 0, len(rawItems))
		for _, item := range rawItems {
			items = append(items, mapPodDisruptionBudget(item, decision))
		}
		source = "live"
	}
	items = filterScopedNamespaceItems(items, decision, func(item domainresource.PodDisruptionBudgetView) string { return item.Namespace })
	populateAllowedActionsPodDisruptionBudgets(items, decision)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "PodDisruptionBudget", "", string(domainaccess.ActionList), "success", fmt.Sprintf("listed pod disruption budgets via %s in namespace %s", source, displayNamespace(namespace)))
	return items, nil
}
func (s *Service) RestartDeployment(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) error {
	if err := s.authorizeDeploymentPermission(ctx, principal, appaccess.PermPlatformDeploymentRestart); err != nil {
		return err
	}
	connection, _, err := s.authorize(ctx, principal, clusterID, namespace, "Deployment", domainaccess.ActionRestart)
	if err != nil {
		return err
	}

	source := "direct"
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return err
		}
		if err := client.RestartDeployment(ctx, namespace, name); err != nil {
			_ = s.recordAudit(ctx, principal, clusterID, namespace, "Deployment", name, string(domainaccess.ActionRestart), "failure", err.Error())
			return fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		if err := s.restartDirectDeployment(ctx, clusterID, namespace, name); err != nil {
			_ = s.recordAudit(ctx, principal, clusterID, namespace, "Deployment", name, string(domainaccess.ActionRestart), "failure", err.Error())
			return err
		}
	}
	if err := s.recordAudit(ctx, principal, clusterID, namespace, "Deployment", name, string(domainaccess.ActionRestart), "success", fmt.Sprintf("restarted deployment via %s", source)); err != nil {
		return fmt.Errorf("record restart deployment audit: %w", err)
	}
	s.recordOperation(ctx, principal, "platform.deployment.restart", clusterID, namespace, "Deployment", name, fmt.Sprintf("restarted deployment via %s", source), nil)
	return nil
}
func (s *Service) ScaleDeployment(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string, replicas int32) error {
	if err := s.authorizeDeploymentPermission(ctx, principal, appaccess.PermPlatformDeploymentScale); err != nil {
		return err
	}
	connection, _, err := s.authorize(ctx, principal, clusterID, namespace, "Deployment", domainaccess.ActionScale)
	if err != nil {
		return err
	}

	source := "direct"
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return err
		}
		if err := client.ScaleDeployment(ctx, namespace, name, replicas); err != nil {
			_ = s.recordAudit(ctx, principal, clusterID, namespace, "Deployment", name, string(domainaccess.ActionScale), "failure", err.Error())
			return fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		if err := s.scaleDirectDeployment(ctx, clusterID, namespace, name, replicas); err != nil {
			_ = s.recordAudit(ctx, principal, clusterID, namespace, "Deployment", name, string(domainaccess.ActionScale), "failure", err.Error())
			return err
		}
	}
	if err := s.recordAudit(ctx, principal, clusterID, namespace, "Deployment", name, string(domainaccess.ActionScale), "success", fmt.Sprintf("scaled deployment to %d via %s", replicas, source)); err != nil {
		return fmt.Errorf("record scale deployment audit: %w", err)
	}
	s.recordOperation(ctx, principal, "platform.deployment.scale", clusterID, namespace, "Deployment", name, fmt.Sprintf("scaled deployment to %d via %s", replicas, source), map[string]any{"replicas": replicas})
	return nil
}
func (s *Service) listDirectDeployments(ctx context.Context, clusterID, namespace string) ([]appsv1.Deployment, string, error) {
	if strings.TrimSpace(namespace) == "" {
		items, err := s.listDeploymentsAcrossNamespaces(ctx, clusterID)
		if err != nil {
			return nil, "live", err
		}
		return items, "live", nil
	}
	if shouldUseInformerCache(namespace) {
		if items, err := s.cache.ListDeployments(clusterID, namespace); err == nil {
			return items, "cache", nil
		} else if !errors.Is(err, informerinfra.ErrCacheNotReady) {
			return nil, "cache", err
		}
	}
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 4*time.Second)
	if err != nil {
		return nil, "live", err
	}
	defer cancel()
	items, err := listDeploymentsLive(queryCtx, bundle, namespace)
	if err != nil {
		return nil, "live", err
	}
	return items, "live", nil
}
func (s *Service) getDirectDeployment(ctx context.Context, clusterID, namespace, name string) (*appsv1.Deployment, error) {
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 4*time.Second)
	if err != nil {
		return nil, err
	}
	defer cancel()
	item, err := bundle.Typed.AppsV1().Deployments(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return item, nil
}
func (s *Service) getDirectDeploymentYAML(ctx context.Context, clusterID, namespace, name string) (domainresource.ResourceYAMLView, error) {
	item, err := s.getDirectDeployment(ctx, clusterID, namespace, name)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	copyItem := item.DeepCopy()
	copyItem.ManagedFields = nil
	content, err := yaml.Marshal(copyItem)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	return domainresource.ResourceYAMLView{
		Kind:      "Deployment",
		Name:      name,
		Namespace: namespace,
		Content:   string(content),
	}, nil
}
func (s *Service) getDirectDeploymentRolloutStatus(ctx context.Context, clusterID, namespace, name string) (domainresource.DeploymentRolloutStatusView, error) {
	item, err := s.getDirectDeployment(ctx, clusterID, namespace, name)
	if err != nil {
		return domainresource.DeploymentRolloutStatusView{}, err
	}
	return mapDeploymentRolloutStatus(*item), nil
}
func (s *Service) getDirectStatefulSet(ctx context.Context, clusterID, namespace, name string) (*appsv1.StatefulSet, error) {
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 4*time.Second)
	if err != nil {
		return nil, err
	}
	defer cancel()
	item, err := bundle.Typed.AppsV1().StatefulSets(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return item, nil
}
func (s *Service) getDirectStatefulSetYAML(ctx context.Context, clusterID, namespace, name string) (domainresource.ResourceYAMLView, error) {
	item, err := s.getDirectStatefulSet(ctx, clusterID, namespace, name)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	copyItem := item.DeepCopy()
	copyItem.ManagedFields = nil
	content, err := yaml.Marshal(copyItem)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	return domainresource.ResourceYAMLView{Kind: "StatefulSet", Name: name, Namespace: namespace, Content: string(content)}, nil
}
func (s *Service) getDirectDaemonSet(ctx context.Context, clusterID, namespace, name string) (*appsv1.DaemonSet, error) {
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 4*time.Second)
	if err != nil {
		return nil, err
	}
	defer cancel()
	item, err := bundle.Typed.AppsV1().DaemonSets(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return item, nil
}
func (s *Service) getDirectDaemonSetYAML(ctx context.Context, clusterID, namespace, name string) (domainresource.ResourceYAMLView, error) {
	item, err := s.getDirectDaemonSet(ctx, clusterID, namespace, name)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	copyItem := item.DeepCopy()
	copyItem.ManagedFields = nil
	content, err := yaml.Marshal(copyItem)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	return domainresource.ResourceYAMLView{Kind: "DaemonSet", Name: name, Namespace: namespace, Content: string(content)}, nil
}
func (s *Service) getDirectJob(ctx context.Context, clusterID, namespace, name string) (*batchv1.Job, error) {
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 4*time.Second)
	if err != nil {
		return nil, err
	}
	defer cancel()
	item, err := bundle.Typed.BatchV1().Jobs(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return item, nil
}
func (s *Service) getDirectJobYAML(ctx context.Context, clusterID, namespace, name string) (domainresource.ResourceYAMLView, error) {
	item, err := s.getDirectJob(ctx, clusterID, namespace, name)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	copyItem := item.DeepCopy()
	copyItem.ManagedFields = nil
	content, err := yaml.Marshal(copyItem)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	return domainresource.ResourceYAMLView{Kind: "Job", Name: name, Namespace: namespace, Content: string(content)}, nil
}
func (s *Service) getDirectCronJob(ctx context.Context, clusterID, namespace, name string) (*batchv1.CronJob, error) {
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 4*time.Second)
	if err != nil {
		return nil, err
	}
	defer cancel()
	item, err := bundle.Typed.BatchV1().CronJobs(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return item, nil
}
func (s *Service) getDirectCronJobYAML(ctx context.Context, clusterID, namespace, name string) (domainresource.ResourceYAMLView, error) {
	item, err := s.getDirectCronJob(ctx, clusterID, namespace, name)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	copyItem := item.DeepCopy()
	copyItem.ManagedFields = nil
	content, err := yaml.Marshal(copyItem)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	return domainresource.ResourceYAMLView{Kind: "CronJob", Name: name, Namespace: namespace, Content: string(content)}, nil
}
func (s *Service) listDirectDeploymentRolloutHistory(ctx context.Context, clusterID, namespace, name string) ([]domainresource.RolloutHistoryView, error) {
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 4*time.Second)
	if err != nil {
		return nil, err
	}
	defer cancel()
	deployment, err := bundle.Typed.AppsV1().Deployments(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	replicaSets, err := bundle.Typed.AppsV1().ReplicaSets(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	items := make([]domainresource.RolloutHistoryView, 0)
	for _, item := range replicaSets.Items {
		if !ownedByDeployment(item.OwnerReferences, deployment.UID) {
			continue
		}
		images := make([]string, 0, len(item.Spec.Template.Spec.Containers))
		for _, container := range item.Spec.Template.Spec.Containers {
			images = append(images, fmt.Sprintf("%s=%s", container.Name, container.Image))
		}
		replicas := int32(0)
		if item.Spec.Replicas != nil {
			replicas = *item.Spec.Replicas
		}
		items = append(items, domainresource.RolloutHistoryView{
			Name:          item.Name,
			Namespace:     item.Namespace,
			Revision:      item.Annotations["deployment.kubernetes.io/revision"],
			Images:        images,
			Replicas:      replicas,
			ReadyReplicas: item.Status.ReadyReplicas,
			CreatedAt:     item.CreationTimestamp.Time.Format(time.RFC3339),
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].CreatedAt > items[j].CreatedAt
	})
	return items, nil
}
func (s *Service) rollbackDirectDeployment(ctx context.Context, clusterID, namespace, name, revision string) error {
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 5*time.Second)
	if err != nil {
		return err
	}
	defer cancel()
	deployment, err := bundle.Typed.AppsV1().Deployments(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	replicaSets, err := bundle.Typed.AppsV1().ReplicaSets(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	var target *appsv1.ReplicaSet
	for index := range replicaSets.Items {
		item := &replicaSets.Items[index]
		if !ownedByDeployment(item.OwnerReferences, deployment.UID) {
			continue
		}
		if item.Annotations["deployment.kubernetes.io/revision"] == revision {
			target = item
			break
		}
	}
	if target == nil {
		return fmt.Errorf("%w: target revision %s not found", apperrors.ErrNotFound, revision)
	}
	deployment.Spec.Template = *target.Spec.Template.DeepCopy()
	if deployment.Spec.Template.Labels != nil {
		delete(deployment.Spec.Template.Labels, "pod-template-hash")
	}
	queryCtxUpdate, cancelUpdate := context.WithTimeout(ctx, 5*time.Second)
	defer cancelUpdate()
	_, err = bundle.Typed.AppsV1().Deployments(namespace).Update(queryCtxUpdate, deployment, metav1.UpdateOptions{})
	return err
}
func (s *Service) listDirectStatefulSets(ctx context.Context, clusterID, namespace string) ([]appsv1.StatefulSet, string, error) {
	if strings.TrimSpace(namespace) == "" {
		items, err := s.listStatefulSetsAcrossNamespaces(ctx, clusterID)
		if err != nil {
			return nil, "live", err
		}
		return items, "live", nil
	}
	if shouldUseInformerCache(namespace) {
		if items, err := s.cache.ListStatefulSets(clusterID, namespace); err == nil {
			return items, "cache", nil
		} else if !errors.Is(err, informerinfra.ErrCacheNotReady) {
			return nil, "cache", err
		}
	}
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 4*time.Second)
	if err != nil {
		return nil, "live", err
	}
	defer cancel()
	items, err := listStatefulSetsLive(queryCtx, bundle, namespace)
	if err != nil {
		return nil, "live", err
	}
	return items, "live", nil
}
func (s *Service) listDirectDaemonSets(ctx context.Context, clusterID, namespace string) ([]appsv1.DaemonSet, error) {
	if strings.TrimSpace(namespace) == "" {
		return s.listDaemonSetsAcrossNamespaces(ctx, clusterID)
	}
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 4*time.Second)
	if err != nil {
		return nil, err
	}
	defer cancel()
	items, err := listDaemonSetsLive(queryCtx, bundle, namespace)
	if err != nil {
		return nil, err
	}
	return items, nil
}
func (s *Service) listDirectJobs(ctx context.Context, clusterID, namespace string) ([]batchv1.Job, error) {
	if strings.TrimSpace(namespace) == "" {
		return s.listJobsAcrossNamespaces(ctx, clusterID)
	}
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 4*time.Second)
	if err != nil {
		return nil, err
	}
	defer cancel()
	items, err := listJobsLive(queryCtx, bundle, namespace)
	if err != nil {
		return nil, err
	}
	return items, nil
}
func (s *Service) listDirectCronJobs(ctx context.Context, clusterID, namespace string) ([]batchv1.CronJob, error) {
	if strings.TrimSpace(namespace) == "" {
		return s.listCronJobsAcrossNamespaces(ctx, clusterID)
	}
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 4*time.Second)
	if err != nil {
		return nil, err
	}
	defer cancel()
	items, err := listCronJobsLive(queryCtx, bundle, namespace)
	if err != nil {
		return nil, err
	}
	return items, nil
}
func (s *Service) listDirectReplicaSets(ctx context.Context, clusterID, namespace string) ([]appsv1.ReplicaSet, error) {
	if strings.TrimSpace(namespace) == "" {
		return listAcrossNamespaces(ctx, s, clusterID, func(queryCtx context.Context, bundle *k8sinfra.Bundle, namespace string) ([]appsv1.ReplicaSet, error) {
			items, err := bundle.Typed.AppsV1().ReplicaSets(namespace).List(queryCtx, metav1.ListOptions{})
			if err != nil {
				return nil, err
			}
			return items.Items, nil
		})
	}
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 4*time.Second)
	if err != nil {
		return nil, err
	}
	defer cancel()
	items, err := bundle.Typed.AppsV1().ReplicaSets(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return items.Items, nil
}
func (s *Service) listDirectHorizontalPodAutoscalers(ctx context.Context, clusterID, namespace string) ([]autoscalingv2.HorizontalPodAutoscaler, error) {
	if strings.TrimSpace(namespace) == "" {
		return listAcrossNamespaces(ctx, s, clusterID, func(queryCtx context.Context, bundle *k8sinfra.Bundle, namespace string) ([]autoscalingv2.HorizontalPodAutoscaler, error) {
			items, err := bundle.Typed.AutoscalingV2().HorizontalPodAutoscalers(namespace).List(queryCtx, metav1.ListOptions{})
			if err != nil {
				return nil, err
			}
			return items.Items, nil
		})
	}
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 4*time.Second)
	if err != nil {
		return nil, err
	}
	defer cancel()
	items, err := bundle.Typed.AutoscalingV2().HorizontalPodAutoscalers(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return items.Items, nil
}
func (s *Service) listDirectPodDisruptionBudgets(ctx context.Context, clusterID, namespace string) ([]policyv1.PodDisruptionBudget, error) {
	if strings.TrimSpace(namespace) == "" {
		return listAcrossNamespaces(ctx, s, clusterID, func(queryCtx context.Context, bundle *k8sinfra.Bundle, namespace string) ([]policyv1.PodDisruptionBudget, error) {
			items, err := bundle.Typed.PolicyV1().PodDisruptionBudgets(namespace).List(queryCtx, metav1.ListOptions{})
			if err != nil {
				return nil, err
			}
			return items.Items, nil
		})
	}
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 4*time.Second)
	if err != nil {
		return nil, err
	}
	defer cancel()
	items, err := bundle.Typed.PolicyV1().PodDisruptionBudgets(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return items.Items, nil
}
func (s *Service) listDeploymentsAcrossNamespaces(ctx context.Context, clusterID string) ([]appsv1.Deployment, error) {
	return listAcrossNamespaces(ctx, s, clusterID, func(queryCtx context.Context, bundle *k8sinfra.Bundle, namespace string) ([]appsv1.Deployment, error) {
		return listDeploymentsLive(queryCtx, bundle, namespace)
	})
}
func (s *Service) listStatefulSetsAcrossNamespaces(ctx context.Context, clusterID string) ([]appsv1.StatefulSet, error) {
	return listAcrossNamespaces(ctx, s, clusterID, func(queryCtx context.Context, bundle *k8sinfra.Bundle, namespace string) ([]appsv1.StatefulSet, error) {
		return listStatefulSetsLive(queryCtx, bundle, namespace)
	})
}
func (s *Service) listDaemonSetsAcrossNamespaces(ctx context.Context, clusterID string) ([]appsv1.DaemonSet, error) {
	return listAcrossNamespaces(ctx, s, clusterID, func(queryCtx context.Context, bundle *k8sinfra.Bundle, namespace string) ([]appsv1.DaemonSet, error) {
		return listDaemonSetsLive(queryCtx, bundle, namespace)
	})
}
func (s *Service) listJobsAcrossNamespaces(ctx context.Context, clusterID string) ([]batchv1.Job, error) {
	return listAcrossNamespaces(ctx, s, clusterID, func(queryCtx context.Context, bundle *k8sinfra.Bundle, namespace string) ([]batchv1.Job, error) {
		return listJobsLive(queryCtx, bundle, namespace)
	})
}
func (s *Service) listCronJobsAcrossNamespaces(ctx context.Context, clusterID string) ([]batchv1.CronJob, error) {
	return listAcrossNamespaces(ctx, s, clusterID, func(queryCtx context.Context, bundle *k8sinfra.Bundle, namespace string) ([]batchv1.CronJob, error) {
		return listCronJobsLive(queryCtx, bundle, namespace)
	})
}

func listDeploymentsLive(ctx context.Context, bundle *k8sinfra.Bundle, namespace string) ([]appsv1.Deployment, error) {
	var list appsv1.DeploymentList
	if err := bundle.Typed.AppsV1().RESTClient().Get().
		Namespace(namespace).
		Resource("deployments").
		VersionedParams(&metav1.ListOptions{}, scheme.ParameterCodec).
		Do(ctx).
		Into(&list); err != nil {
		return nil, err
	}
	return list.Items, nil
}
func listStatefulSetsLive(ctx context.Context, bundle *k8sinfra.Bundle, namespace string) ([]appsv1.StatefulSet, error) {
	var list appsv1.StatefulSetList
	if err := bundle.Typed.AppsV1().RESTClient().Get().
		Namespace(namespace).
		Resource("statefulsets").
		VersionedParams(&metav1.ListOptions{}, scheme.ParameterCodec).
		Do(ctx).
		Into(&list); err != nil {
		return nil, err
	}
	return list.Items, nil
}
func listDaemonSetsLive(ctx context.Context, bundle *k8sinfra.Bundle, namespace string) ([]appsv1.DaemonSet, error) {
	var list appsv1.DaemonSetList
	if err := bundle.Typed.AppsV1().RESTClient().Get().
		Namespace(namespace).
		Resource("daemonsets").
		VersionedParams(&metav1.ListOptions{}, scheme.ParameterCodec).
		Do(ctx).
		Into(&list); err != nil {
		return nil, err
	}
	return list.Items, nil
}
func listJobsLive(ctx context.Context, bundle *k8sinfra.Bundle, namespace string) ([]batchv1.Job, error) {
	var list batchv1.JobList
	if err := bundle.Typed.BatchV1().RESTClient().Get().
		Namespace(namespace).
		Resource("jobs").
		VersionedParams(&metav1.ListOptions{}, scheme.ParameterCodec).
		Do(ctx).
		Into(&list); err != nil {
		return nil, err
	}
	return list.Items, nil
}
func listCronJobsLive(ctx context.Context, bundle *k8sinfra.Bundle, namespace string) ([]batchv1.CronJob, error) {
	var list batchv1.CronJobList
	if err := bundle.Typed.BatchV1().RESTClient().Get().
		Namespace(namespace).
		Resource("cronjobs").
		VersionedParams(&metav1.ListOptions{}, scheme.ParameterCodec).
		Do(ctx).
		Into(&list); err != nil {
		return nil, err
	}
	return list.Items, nil
}
func (s *Service) restartDirectDeployment(ctx context.Context, clusterID, namespace, name string) error {
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 4*time.Second)
	if err != nil {
		return err
	}
	defer cancel()
	deployment, err := bundle.Typed.AppsV1().Deployments(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if deployment.Spec.Template.Annotations == nil {
		deployment.Spec.Template.Annotations = map[string]string{}
	}
	deployment.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] = time.Now().UTC().Format(time.RFC3339)
	_, err = bundle.Typed.AppsV1().Deployments(namespace).Update(queryCtx, deployment, metav1.UpdateOptions{})
	return err
}
func (s *Service) scaleDirectDeployment(ctx context.Context, clusterID, namespace, name string, replicas int32) error {
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 4*time.Second)
	if err != nil {
		return err
	}
	defer cancel()
	deployment, err := bundle.Typed.AppsV1().Deployments(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	deployment.Spec.Replicas = &replicas
	_, err = bundle.Typed.AppsV1().Deployments(namespace).Update(queryCtx, deployment, metav1.UpdateOptions{})
	return err
}
func mapDeployment(item appsv1.Deployment, decision domainaccess.Decision) domainresource.DeploymentView {
	desired := int32(1)
	if item.Spec.Replicas != nil {
		desired = *item.Spec.Replicas
	}
	return domainresource.DeploymentView{Name: item.Name, Namespace: item.Namespace, Labels: item.Labels, DesiredReplicas: desired, ReadyReplicas: item.Status.ReadyReplicas, UpdatedReplicas: item.Status.UpdatedReplicas, Available: item.Status.AvailableReplicas, AgeSeconds: secondsSince(item.CreationTimestamp.Time), AllowedActions: stringifyActions(decision.AllowedActions)}
}
func mapDeploymentDetail(item appsv1.Deployment, decision domainaccess.Decision) domainresource.DeploymentDetailView {
	desired := int32(1)
	if item.Spec.Replicas != nil {
		desired = *item.Spec.Replicas
	}
	containers := make([]domainresource.WorkloadContainerView, 0, len(item.Spec.Template.Spec.Containers))
	for _, container := range item.Spec.Template.Spec.Containers {
		containers = append(containers, domainresource.WorkloadContainerView{
			Name:  container.Name,
			Image: container.Image,
		})
	}
	conditions := make([]domainresource.WorkloadConditionView, 0, len(item.Status.Conditions))
	for _, condition := range item.Status.Conditions {
		conditions = append(conditions, domainresource.WorkloadConditionView{
			Type:               string(condition.Type),
			Status:             string(condition.Status),
			Reason:             condition.Reason,
			Message:            condition.Message,
			LastTransitionTime: condition.LastTransitionTime.Time.Format(time.RFC3339),
		})
	}
	return domainresource.DeploymentDetailView{
		Name:               item.Name,
		Namespace:          item.Namespace,
		DesiredReplicas:    desired,
		ReadyReplicas:      item.Status.ReadyReplicas,
		UpdatedReplicas:    item.Status.UpdatedReplicas,
		AvailableReplicas:  item.Status.AvailableReplicas,
		ObservedGeneration: item.Status.ObservedGeneration,
		Strategy:           string(item.Spec.Strategy.Type),
		CreatedAt:          item.CreationTimestamp.Time.Format(time.RFC3339),
		Labels:             item.Labels,
		Annotations:        item.Annotations,
		Selector:           item.Spec.Selector.MatchLabels,
		Containers:         containers,
		Conditions:         conditions,
		AllowedActions:     stringifyActions(decision.AllowedActions),
	}
}
func mapDeploymentRolloutStatus(item appsv1.Deployment) domainresource.DeploymentRolloutStatusView {
	desired := int32(1)
	if item.Spec.Replicas != nil {
		desired = *item.Spec.Replicas
	}
	status := "progressing"
	message := "rollout is progressing"
	for _, condition := range item.Status.Conditions {
		if condition.Type == appsv1.DeploymentAvailable && condition.Status == corev1.ConditionTrue && item.Status.UpdatedReplicas == desired && item.Status.AvailableReplicas == desired {
			status = "healthy"
			message = "deployment is fully available"
		}
		if condition.Type == appsv1.DeploymentReplicaFailure && condition.Status == corev1.ConditionTrue {
			status = "degraded"
			message = condition.Message
		}
	}
	conditions := make([]domainresource.WorkloadConditionView, 0, len(item.Status.Conditions))
	for _, condition := range item.Status.Conditions {
		conditions = append(conditions, domainresource.WorkloadConditionView{
			Type:               string(condition.Type),
			Status:             string(condition.Status),
			Reason:             condition.Reason,
			Message:            condition.Message,
			LastTransitionTime: condition.LastTransitionTime.Time.Format(time.RFC3339),
		})
	}
	return domainresource.DeploymentRolloutStatusView{
		Name:               item.Name,
		Namespace:          item.Namespace,
		Revision:           item.Annotations["deployment.kubernetes.io/revision"],
		Status:             status,
		Message:            message,
		DesiredReplicas:    desired,
		UpdatedReplicas:    item.Status.UpdatedReplicas,
		ReadyReplicas:      item.Status.ReadyReplicas,
		AvailableReplicas:  item.Status.AvailableReplicas,
		ObservedGeneration: item.Status.ObservedGeneration,
		Conditions:         conditions,
	}
}
func mapStatefulSet(item appsv1.StatefulSet, decision domainaccess.Decision) domainresource.StatefulSetView {
	desired := int32(1)
	if item.Spec.Replicas != nil {
		desired = *item.Spec.Replicas
	}
	return domainresource.StatefulSetView{Name: item.Name, Namespace: item.Namespace, ServiceName: item.Spec.ServiceName, DesiredReplicas: desired, ReadyReplicas: item.Status.ReadyReplicas, CurrentReplicas: item.Status.CurrentReplicas, AgeSeconds: secondsSince(item.CreationTimestamp.Time), AllowedActions: stringifyActions(decision.AllowedActions)}
}
func mapStatefulSetDetail(item appsv1.StatefulSet, decision domainaccess.Decision) domainresource.StatefulSetDetailView {
	desired := int32(1)
	if item.Spec.Replicas != nil {
		desired = *item.Spec.Replicas
	}
	return domainresource.StatefulSetDetailView{
		Name:            item.Name,
		Namespace:       item.Namespace,
		ServiceName:     item.Spec.ServiceName,
		DesiredReplicas: desired,
		ReadyReplicas:   item.Status.ReadyReplicas,
		CurrentReplicas: item.Status.CurrentReplicas,
		UpdateStrategy:  string(item.Spec.UpdateStrategy.Type),
		CurrentRevision: item.Status.CurrentRevision,
		UpdateRevision:  item.Status.UpdateRevision,
		CreatedAt:       item.CreationTimestamp.Time.Format(time.RFC3339),
		Labels:          item.Labels,
		Annotations:     item.Annotations,
		Selector:        item.Spec.Selector.MatchLabels,
		AllowedActions:  stringifyActions(decision.AllowedActions),
	}
}
func mapDaemonSet(item appsv1.DaemonSet, decision domainaccess.Decision) domainresource.DaemonSetView {
	return domainresource.DaemonSetView{
		Name:            item.Name,
		Namespace:       item.Namespace,
		DesiredNumber:   item.Status.DesiredNumberScheduled,
		CurrentNumber:   item.Status.CurrentNumberScheduled,
		ReadyNumber:     item.Status.NumberReady,
		AvailableNumber: item.Status.NumberAvailable,
		UpdatedNumber:   item.Status.UpdatedNumberScheduled,
		AgeSeconds:      secondsSince(item.CreationTimestamp.Time),
		AllowedActions:  stringifyActions(decision.AllowedActions),
	}
}
func mapDaemonSetDetail(item appsv1.DaemonSet, decision domainaccess.Decision) domainresource.DaemonSetDetailView {
	selector := map[string]string{}
	if item.Spec.Selector != nil {
		selector = item.Spec.Selector.MatchLabels
	}
	return domainresource.DaemonSetDetailView{
		Name:            item.Name,
		Namespace:       item.Namespace,
		DesiredNumber:   item.Status.DesiredNumberScheduled,
		CurrentNumber:   item.Status.CurrentNumberScheduled,
		ReadyNumber:     item.Status.NumberReady,
		AvailableNumber: item.Status.NumberAvailable,
		UpdatedNumber:   item.Status.UpdatedNumberScheduled,
		UpdateStrategy:  string(item.Spec.UpdateStrategy.Type),
		CreatedAt:       item.CreationTimestamp.Time.Format(time.RFC3339),
		Labels:          item.Labels,
		Annotations:     item.Annotations,
		Selector:        selector,
		AllowedActions:  stringifyActions(decision.AllowedActions),
	}
}
func mapJob(item batchv1.Job, decision domainaccess.Decision) domainresource.JobView {
	completions := int32(0)
	if item.Spec.Completions != nil {
		completions = *item.Spec.Completions
	}
	completionMode := ""
	if item.Spec.CompletionMode != nil {
		completionMode = string(*item.Spec.CompletionMode)
	}
	return domainresource.JobView{
		Name:           item.Name,
		Namespace:      item.Namespace,
		Completions:    completions,
		Succeeded:      item.Status.Succeeded,
		Failed:         item.Status.Failed,
		Active:         item.Status.Active,
		CompletionMode: completionMode,
		AgeSeconds:     secondsSince(item.CreationTimestamp.Time),
		AllowedActions: stringifyActions(decision.AllowedActions),
	}
}
func mapJobDetail(item batchv1.Job, decision domainaccess.Decision) domainresource.JobDetailView {
	completions := int32(0)
	if item.Spec.Completions != nil {
		completions = *item.Spec.Completions
	}
	parallelism := int32(1)
	if item.Spec.Parallelism != nil {
		parallelism = *item.Spec.Parallelism
	}
	completionMode := ""
	if item.Spec.CompletionMode != nil {
		completionMode = string(*item.Spec.CompletionMode)
	}
	startTime := ""
	if item.Status.StartTime != nil {
		startTime = item.Status.StartTime.Time.Format(time.RFC3339)
	}
	completionTime := ""
	if item.Status.CompletionTime != nil {
		completionTime = item.Status.CompletionTime.Time.Format(time.RFC3339)
	}
	return domainresource.JobDetailView{
		Name:           item.Name,
		Namespace:      item.Namespace,
		Completions:    completions,
		Parallelism:    parallelism,
		Succeeded:      item.Status.Succeeded,
		Failed:         item.Status.Failed,
		Active:         item.Status.Active,
		CompletionMode: completionMode,
		CreatedAt:      item.CreationTimestamp.Time.Format(time.RFC3339),
		StartTime:      startTime,
		CompletionTime: completionTime,
		Labels:         item.Labels,
		Annotations:    item.Annotations,
		AllowedActions: stringifyActions(decision.AllowedActions),
	}
}
func mapCronJob(item batchv1.CronJob, decision domainaccess.Decision) domainresource.CronJobView {
	lastScheduleTime := ""
	if item.Status.LastScheduleTime != nil {
		lastScheduleTime = item.Status.LastScheduleTime.Time.Format(time.RFC3339)
	}
	return domainresource.CronJobView{
		Name:             item.Name,
		Namespace:        item.Namespace,
		Schedule:         item.Spec.Schedule,
		Suspend:          item.Spec.Suspend != nil && *item.Spec.Suspend,
		ActiveJobs:       int32(len(item.Status.Active)),
		LastScheduleTime: lastScheduleTime,
		AgeSeconds:       secondsSince(item.CreationTimestamp.Time),
		AllowedActions:   stringifyActions(decision.AllowedActions),
	}
}
func mapCronJobDetail(item batchv1.CronJob, decision domainaccess.Decision) domainresource.CronJobDetailView {
	lastScheduleTime := ""
	if item.Status.LastScheduleTime != nil {
		lastScheduleTime = item.Status.LastScheduleTime.Time.Format(time.RFC3339)
	}
	timeZone := ""
	if item.Spec.TimeZone != nil {
		timeZone = *item.Spec.TimeZone
	}
	return domainresource.CronJobDetailView{
		Name:              item.Name,
		Namespace:         item.Namespace,
		Schedule:          item.Spec.Schedule,
		Suspend:           item.Spec.Suspend != nil && *item.Spec.Suspend,
		ActiveJobs:        int32(len(item.Status.Active)),
		LastScheduleTime:  lastScheduleTime,
		ConcurrencyPolicy: string(item.Spec.ConcurrencyPolicy),
		TimeZone:          timeZone,
		CreatedAt:         item.CreationTimestamp.Time.Format(time.RFC3339),
		Labels:            item.Labels,
		Annotations:       item.Annotations,
		AllowedActions:    stringifyActions(decision.AllowedActions),
	}
}
func mapReplicaSet(item appsv1.ReplicaSet, decision domainaccess.Decision) domainresource.ReplicaSetView {
	desired := int32(0)
	if item.Spec.Replicas != nil {
		desired = *item.Spec.Replicas
	}
	return domainresource.ReplicaSetView{
		Name:              item.Name,
		Namespace:         item.Namespace,
		DesiredReplicas:   desired,
		ReadyReplicas:     item.Status.ReadyReplicas,
		AvailableReplicas: item.Status.AvailableReplicas,
		AgeSeconds:        secondsSince(item.CreationTimestamp.Time),
		AllowedActions:    stringifyActions(decision.AllowedActions),
	}
}
func mapHorizontalPodAutoscaler(item autoscalingv2.HorizontalPodAutoscaler, decision domainaccess.Decision) domainresource.HorizontalPodAutoscalerView {
	minReplicas := int32(1)
	if item.Spec.MinReplicas != nil {
		minReplicas = *item.Spec.MinReplicas
	}
	return domainresource.HorizontalPodAutoscalerView{
		Name:            item.Name,
		Namespace:       item.Namespace,
		TargetRef:       fmt.Sprintf("%s/%s", item.Spec.ScaleTargetRef.Kind, item.Spec.ScaleTargetRef.Name),
		MinReplicas:     minReplicas,
		MaxReplicas:     item.Spec.MaxReplicas,
		CurrentReplicas: item.Status.CurrentReplicas,
		DesiredReplicas: item.Status.DesiredReplicas,
		AgeSeconds:      secondsSince(item.CreationTimestamp.Time),
		AllowedActions:  stringifyActions(decision.AllowedActions),
	}
}
func mapPodDisruptionBudget(item policyv1.PodDisruptionBudget, decision domainaccess.Decision) domainresource.PodDisruptionBudgetView {
	minAvailable := ""
	if item.Spec.MinAvailable != nil {
		minAvailable = item.Spec.MinAvailable.String()
	}
	maxUnavailable := ""
	if item.Spec.MaxUnavailable != nil {
		maxUnavailable = item.Spec.MaxUnavailable.String()
	}
	return domainresource.PodDisruptionBudgetView{
		Name:               item.Name,
		Namespace:          item.Namespace,
		MinAvailable:       minAvailable,
		MaxUnavailable:     maxUnavailable,
		CurrentHealthy:     item.Status.CurrentHealthy,
		DesiredHealthy:     item.Status.DesiredHealthy,
		DisruptionsAllowed: item.Status.DisruptionsAllowed,
		AgeSeconds:         secondsSince(item.CreationTimestamp.Time),
		AllowedActions:     stringifyActions(decision.AllowedActions),
	}
}
func ownedByDeployment(owners []metav1.OwnerReference, uid types.UID) bool {
	for _, owner := range owners {
		if owner.UID == uid && owner.Kind == "Deployment" {
			return true
		}
	}
	return false
}
func populateAllowedActionsDeployments(items []domainresource.DeploymentView, decision domainaccess.Decision) {
	for i := range items {
		if len(items[i].AllowedActions) == 0 {
			items[i].AllowedActions = stringifyActions(decision.AllowedActions)
		}
	}
}
func populateAllowedActionsStatefulSets(items []domainresource.StatefulSetView, decision domainaccess.Decision) {
	for i := range items {
		if len(items[i].AllowedActions) == 0 {
			items[i].AllowedActions = stringifyActions(decision.AllowedActions)
		}
	}
}
func populateAllowedActionsDaemonSets(items []domainresource.DaemonSetView, decision domainaccess.Decision) {
	for i := range items {
		if len(items[i].AllowedActions) == 0 {
			items[i].AllowedActions = stringifyActions(decision.AllowedActions)
		}
	}
}
func populateAllowedActionsJobs(items []domainresource.JobView, decision domainaccess.Decision) {
	for i := range items {
		if len(items[i].AllowedActions) == 0 {
			items[i].AllowedActions = stringifyActions(decision.AllowedActions)
		}
	}
}
func populateAllowedActionsCronJobs(items []domainresource.CronJobView, decision domainaccess.Decision) {
	for i := range items {
		if len(items[i].AllowedActions) == 0 {
			items[i].AllowedActions = stringifyActions(decision.AllowedActions)
		}
	}
}
func populateAllowedActionsReplicaSets(items []domainresource.ReplicaSetView, decision domainaccess.Decision) {
	for i := range items {
		if len(items[i].AllowedActions) == 0 {
			items[i].AllowedActions = stringifyActions(decision.AllowedActions)
		}
	}
}
func populateAllowedActionsHorizontalPodAutoscalers(items []domainresource.HorizontalPodAutoscalerView, decision domainaccess.Decision) {
	for i := range items {
		if len(items[i].AllowedActions) == 0 {
			items[i].AllowedActions = stringifyActions(decision.AllowedActions)
		}
	}
}
func populateAllowedActionsPodDisruptionBudgets(items []domainresource.PodDisruptionBudgetView, decision domainaccess.Decision) {
	for i := range items {
		if len(items[i].AllowedActions) == 0 {
			items[i].AllowedActions = stringifyActions(decision.AllowedActions)
		}
	}
}
