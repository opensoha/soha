package resource

import (
	"context"
	"fmt"
	"strings"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (w *Workloads) ListDeployments(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.DeploymentView, error) {
	return listWorkloadResources(ctx, w, principal, clusterID, namespace, workloadListSpec[domainresource.DeploymentView]{
		kind: "Deployment", auditText: "listed deployments",
		agent: func(client WorkloadAgent) ([]domainresource.DeploymentView, error) {
			return client.ListDeployments(ctx, namespace)
		},
		direct: func() ([]domainresource.DeploymentView, string, error) {
			return w.direct.ListDeployments(ctx, clusterID, namespace)
		},
		namespaceOf: func(item domainresource.DeploymentView) string { return item.Namespace }, populate: populateAllowedActionsDeployments,
	})
}

func (w *Workloads) GetDeploymentDetail(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.DeploymentDetailView, error) {
	return getWorkloadResource(ctx, w, principal, clusterID, namespace, name, workloadGetSpec[domainresource.DeploymentDetailView]{
		kind: "Deployment", auditText: "viewed deployment detail",
		agent: func(client WorkloadAgent) (domainresource.DeploymentDetailView, error) {
			return client.GetDeploymentDetail(ctx, namespace, name)
		},
		direct: func() (domainresource.DeploymentDetailView, string, error) {
			return liveWorkload(func() (domainresource.DeploymentDetailView, error) {
				return w.direct.GetDeploymentDetail(ctx, clusterID, namespace, name)
			})
		},
		finalize: func(item *domainresource.DeploymentDetailView, decision domainaccess.Decision) {
			item.AllowedActions = stringifyActions(decision.AllowedActions)
		},
	})
}

func (w *Workloads) GetDeploymentYAML(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.ResourceYAMLView, error) {
	return getWorkloadResource(ctx, w, principal, clusterID, namespace, name, workloadGetSpec[domainresource.ResourceYAMLView]{
		kind: "Deployment", auditText: "viewed deployment yaml",
		agent: func(client WorkloadAgent) (domainresource.ResourceYAMLView, error) {
			return client.GetDeploymentYAML(ctx, namespace, name)
		},
		direct: func() (domainresource.ResourceYAMLView, string, error) {
			return liveWorkload(func() (domainresource.ResourceYAMLView, error) {
				return w.direct.GetDeploymentYAML(ctx, clusterID, namespace, name)
			})
		},
	})
}

func (w *Workloads) ApplyDeploymentYAML(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name, content string) (domainresource.ResourceYAMLView, error) {
	return w.yaml.applyResourceYAML(ctx, principal, clusterID, namespace, "Deployment", name, content)
}

func (w *Workloads) GetStatefulSetDetail(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.StatefulSetDetailView, error) {
	return getWorkloadResource(ctx, w, principal, clusterID, namespace, name, workloadGetSpec[domainresource.StatefulSetDetailView]{
		kind: "StatefulSet", auditText: "viewed statefulset detail",
		agent: func(client WorkloadAgent) (domainresource.StatefulSetDetailView, error) {
			return client.GetStatefulSetDetail(ctx, namespace, name)
		},
		direct: func() (domainresource.StatefulSetDetailView, string, error) {
			return liveWorkload(func() (domainresource.StatefulSetDetailView, error) {
				return w.direct.GetStatefulSetDetail(ctx, clusterID, namespace, name)
			})
		},
		finalize: func(item *domainresource.StatefulSetDetailView, decision domainaccess.Decision) {
			item.AllowedActions = stringifyActions(decision.AllowedActions)
		},
	})
}

func (w *Workloads) GetStatefulSetYAML(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.ResourceYAMLView, error) {
	return getWorkloadResource(ctx, w, principal, clusterID, namespace, name, workloadGetSpec[domainresource.ResourceYAMLView]{
		kind: "StatefulSet", auditText: "viewed statefulset yaml",
		agent: func(client WorkloadAgent) (domainresource.ResourceYAMLView, error) {
			return client.GetStatefulSetYAML(ctx, namespace, name)
		},
		direct: func() (domainresource.ResourceYAMLView, string, error) {
			return liveWorkload(func() (domainresource.ResourceYAMLView, error) {
				return w.direct.GetStatefulSetYAML(ctx, clusterID, namespace, name)
			})
		},
	})
}

func (w *Workloads) ApplyStatefulSetYAML(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name, content string) (domainresource.ResourceYAMLView, error) {
	return w.yaml.applyResourceYAML(ctx, principal, clusterID, namespace, "StatefulSet", name, content)
}

func (w *Workloads) GetDaemonSetDetail(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.DaemonSetDetailView, error) {
	return getWorkloadResource(ctx, w, principal, clusterID, namespace, name, workloadGetSpec[domainresource.DaemonSetDetailView]{
		kind: "DaemonSet", auditText: "viewed daemonset detail",
		agent: func(client WorkloadAgent) (domainresource.DaemonSetDetailView, error) {
			return client.GetDaemonSetDetail(ctx, namespace, name)
		},
		direct: func() (domainresource.DaemonSetDetailView, string, error) {
			return liveWorkload(func() (domainresource.DaemonSetDetailView, error) {
				return w.direct.GetDaemonSetDetail(ctx, clusterID, namespace, name)
			})
		},
		finalize: func(item *domainresource.DaemonSetDetailView, decision domainaccess.Decision) {
			item.AllowedActions = stringifyActions(decision.AllowedActions)
		},
	})
}

func (w *Workloads) GetDaemonSetYAML(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.ResourceYAMLView, error) {
	return getWorkloadResource(ctx, w, principal, clusterID, namespace, name, workloadGetSpec[domainresource.ResourceYAMLView]{
		kind: "DaemonSet", auditText: "viewed daemonset yaml",
		agent: func(client WorkloadAgent) (domainresource.ResourceYAMLView, error) {
			return client.GetDaemonSetYAML(ctx, namespace, name)
		},
		direct: func() (domainresource.ResourceYAMLView, string, error) {
			return liveWorkload(func() (domainresource.ResourceYAMLView, error) {
				return w.direct.GetDaemonSetYAML(ctx, clusterID, namespace, name)
			})
		},
	})
}

func (w *Workloads) ApplyDaemonSetYAML(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name, content string) (domainresource.ResourceYAMLView, error) {
	return w.yaml.applyResourceYAML(ctx, principal, clusterID, namespace, "DaemonSet", name, content)
}

func (w *Workloads) GetJobDetail(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.JobDetailView, error) {
	return getWorkloadResource(ctx, w, principal, clusterID, namespace, name, workloadGetSpec[domainresource.JobDetailView]{
		kind: "Job", auditText: "viewed job detail",
		agent: func(client WorkloadAgent) (domainresource.JobDetailView, error) {
			return client.GetJobDetail(ctx, namespace, name)
		},
		direct: func() (domainresource.JobDetailView, string, error) {
			return liveWorkload(func() (domainresource.JobDetailView, error) {
				return w.direct.GetJobDetail(ctx, clusterID, namespace, name)
			})
		},
		finalize: func(item *domainresource.JobDetailView, decision domainaccess.Decision) {
			item.AllowedActions = stringifyActions(decision.AllowedActions)
		},
	})
}

func (w *Workloads) GetJobYAML(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.ResourceYAMLView, error) {
	return getWorkloadResource(ctx, w, principal, clusterID, namespace, name, workloadGetSpec[domainresource.ResourceYAMLView]{
		kind: "Job", auditText: "viewed job yaml",
		agent: func(client WorkloadAgent) (domainresource.ResourceYAMLView, error) {
			return client.GetJobYAML(ctx, namespace, name)
		},
		direct: func() (domainresource.ResourceYAMLView, string, error) {
			return liveWorkload(func() (domainresource.ResourceYAMLView, error) {
				return w.direct.GetJobYAML(ctx, clusterID, namespace, name)
			})
		},
	})
}

func (w *Workloads) ApplyJobYAML(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name, content string) (domainresource.ResourceYAMLView, error) {
	return w.yaml.applyResourceYAML(ctx, principal, clusterID, namespace, "Job", name, content)
}

func (w *Workloads) GetCronJobDetail(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.CronJobDetailView, error) {
	return getWorkloadResource(ctx, w, principal, clusterID, namespace, name, workloadGetSpec[domainresource.CronJobDetailView]{
		kind: "CronJob", auditText: "viewed cronjob detail",
		agent: func(client WorkloadAgent) (domainresource.CronJobDetailView, error) {
			return client.GetCronJobDetail(ctx, namespace, name)
		},
		direct: func() (domainresource.CronJobDetailView, string, error) {
			return liveWorkload(func() (domainresource.CronJobDetailView, error) {
				return w.direct.GetCronJobDetail(ctx, clusterID, namespace, name)
			})
		},
		finalize: func(item *domainresource.CronJobDetailView, decision domainaccess.Decision) {
			item.AllowedActions = stringifyActions(decision.AllowedActions)
		},
	})
}

func (w *Workloads) GetCronJobYAML(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.ResourceYAMLView, error) {
	return getWorkloadResource(ctx, w, principal, clusterID, namespace, name, workloadGetSpec[domainresource.ResourceYAMLView]{
		kind: "CronJob", auditText: "viewed cronjob yaml",
		agent: func(client WorkloadAgent) (domainresource.ResourceYAMLView, error) {
			return client.GetCronJobYAML(ctx, namespace, name)
		},
		direct: func() (domainresource.ResourceYAMLView, string, error) {
			return liveWorkload(func() (domainresource.ResourceYAMLView, error) {
				return w.direct.GetCronJobYAML(ctx, clusterID, namespace, name)
			})
		},
	})
}

func (w *Workloads) ApplyCronJobYAML(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name, content string) (domainresource.ResourceYAMLView, error) {
	return w.yaml.applyResourceYAML(ctx, principal, clusterID, namespace, "CronJob", name, content)
}

func (w *Workloads) SetCronJobSuspend(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string, suspend bool) (domainresource.CronJobDetailView, error) {
	connection, decision, err := w.authorize(ctx, principal, clusterID, namespace, "CronJob", domainaccess.ActionUpdate)
	if err != nil {
		return domainresource.CronJobDetailView{}, err
	}
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		return domainresource.CronJobDetailView{}, fmt.Errorf("%w: cronjob suspend is not supported for agent-connected clusters yet", apperrors.ErrUnsupportedOperation)
	}
	item, err := w.direct.SetCronJobSuspend(ctx, clusterID, namespace, name, suspend)
	if err != nil {
		_ = w.recordAudit(ctx, principal, connection.Summary.ID, namespace, "CronJob", name, string(domainaccess.ActionUpdate), "failure", err.Error())
		return domainresource.CronJobDetailView{}, err
	}
	item.AllowedActions = stringifyActions(decision.AllowedActions)
	_ = w.recordAudit(ctx, principal, connection.Summary.ID, namespace, "CronJob", name, string(domainaccess.ActionUpdate), "success", fmt.Sprintf("set cronjob suspend=%t", suspend))
	return item, nil
}

func (w *Workloads) GetDeploymentRolloutStatus(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.DeploymentRolloutStatusView, error) {
	return getWorkloadResource(ctx, w, principal, clusterID, namespace, name, workloadGetSpec[domainresource.DeploymentRolloutStatusView]{
		kind: "Deployment", auditText: "viewed deployment rollout status",
		agent: func(client WorkloadAgent) (domainresource.DeploymentRolloutStatusView, error) {
			return client.GetDeploymentRolloutStatus(ctx, namespace, name)
		},
		direct: func() (domainresource.DeploymentRolloutStatusView, string, error) {
			return liveWorkload(func() (domainresource.DeploymentRolloutStatusView, error) {
				return w.direct.GetDeploymentRolloutStatus(ctx, clusterID, namespace, name)
			})
		},
	})
}

func (w *Workloads) ListDeploymentRolloutHistory(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) ([]domainresource.RolloutHistoryView, error) {
	return getWorkloadResource(ctx, w, principal, clusterID, namespace, name, workloadGetSpec[[]domainresource.RolloutHistoryView]{
		kind: "Deployment", auditText: "listed deployment rollout history",
		agent: func(client WorkloadAgent) ([]domainresource.RolloutHistoryView, error) {
			return client.ListDeploymentRolloutHistory(ctx, namespace, name)
		},
		direct: func() ([]domainresource.RolloutHistoryView, string, error) {
			return liveWorkload(func() ([]domainresource.RolloutHistoryView, error) {
				return w.direct.ListDeploymentRolloutHistory(ctx, clusterID, namespace, name)
			})
		},
	})
}

func (w *Workloads) RollbackDeployment(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name, revision string) (domainresource.DeploymentRollbackView, error) {
	if err := w.authorizeDeploymentPermission(ctx, principal, appaccess.PermPlatformDeploymentRollback); err != nil {
		return domainresource.DeploymentRollbackView{}, err
	}
	connection, _, err := w.authorize(ctx, principal, clusterID, namespace, "Deployment", domainaccess.ActionRollback)
	if err != nil {
		return domainresource.DeploymentRollbackView{}, err
	}
	revision = strings.TrimSpace(revision)
	if revision == "" {
		return domainresource.DeploymentRollbackView{}, fmt.Errorf("%w: revision is required", apperrors.ErrInvalidArgument)
	}
	source := "live"
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		client, err := w.workloadAgentClient(connection)
		if err != nil {
			return domainresource.DeploymentRollbackView{}, err
		}
		err = client.RollbackDeployment(ctx, namespace, name, revision)
		source = "agent"
		if err != nil {
			_ = w.recordAudit(ctx, principal, clusterID, namespace, "Deployment", name, string(domainaccess.ActionRollback), "failure", err.Error())
			return domainresource.DeploymentRollbackView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
	} else if err := w.direct.RollbackDeployment(ctx, clusterID, namespace, name, revision); err != nil {
		_ = w.recordAudit(ctx, principal, clusterID, namespace, "Deployment", name, string(domainaccess.ActionRollback), "failure", err.Error())
		return domainresource.DeploymentRollbackView{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	message := fmt.Sprintf("rolled back deployment to revision %s via %s", revision, source)
	_ = w.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Deployment", name, string(domainaccess.ActionRollback), "success", message)
	w.recordOperation(ctx, principal, "platform.deployment.rollback", connection.Summary.ID, namespace, "Deployment", name, message, map[string]any{"revision": revision})
	return domainresource.DeploymentRollbackView{
		Name: name, Namespace: namespace, TargetRevision: revision,
		Message: fmt.Sprintf("Rollback to revision %s requested.", revision), RequestedAt: now,
	}, nil
}

func (w *Workloads) ListStatefulSets(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.StatefulSetView, error) {
	return listWorkloadResources(ctx, w, principal, clusterID, namespace, workloadListSpec[domainresource.StatefulSetView]{
		kind: "StatefulSet", auditText: "listed statefulsets",
		agent: func(client WorkloadAgent) ([]domainresource.StatefulSetView, error) {
			return client.ListStatefulSets(ctx, namespace)
		},
		direct: func() ([]domainresource.StatefulSetView, string, error) {
			return w.direct.ListStatefulSets(ctx, clusterID, namespace)
		},
		namespaceOf: func(item domainresource.StatefulSetView) string { return item.Namespace }, populate: populateAllowedActionsStatefulSets,
	})
}

func (w *Workloads) ListDaemonSets(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.DaemonSetView, error) {
	return listWorkloadResources(ctx, w, principal, clusterID, namespace, workloadListSpec[domainresource.DaemonSetView]{
		kind: "DaemonSet", auditText: "listed daemonsets",
		agent: func(client WorkloadAgent) ([]domainresource.DaemonSetView, error) {
			return client.ListDaemonSets(ctx, namespace)
		},
		direct: func() ([]domainresource.DaemonSetView, string, error) {
			return liveWorkload(func() ([]domainresource.DaemonSetView, error) {
				return w.direct.ListDaemonSets(ctx, clusterID, namespace)
			})
		},
		namespaceOf: func(item domainresource.DaemonSetView) string { return item.Namespace }, populate: populateAllowedActionsDaemonSets,
	})
}

func (w *Workloads) ListJobs(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.JobView, error) {
	return listWorkloadResources(ctx, w, principal, clusterID, namespace, workloadListSpec[domainresource.JobView]{
		kind: "Job", auditText: "listed jobs",
		agent: func(client WorkloadAgent) ([]domainresource.JobView, error) { return client.ListJobs(ctx, namespace) },
		direct: func() ([]domainresource.JobView, string, error) {
			return liveWorkload(func() ([]domainresource.JobView, error) { return w.direct.ListJobs(ctx, clusterID, namespace) })
		},
		namespaceOf: func(item domainresource.JobView) string { return item.Namespace }, populate: populateAllowedActionsJobs,
	})
}

func (w *Workloads) ListCronJobs(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.CronJobView, error) {
	return listWorkloadResources(ctx, w, principal, clusterID, namespace, workloadListSpec[domainresource.CronJobView]{
		kind: "CronJob", auditText: "listed cronjobs",
		agent: func(client WorkloadAgent) ([]domainresource.CronJobView, error) {
			return client.ListCronJobs(ctx, namespace)
		},
		direct: func() ([]domainresource.CronJobView, string, error) {
			return liveWorkload(func() ([]domainresource.CronJobView, error) { return w.direct.ListCronJobs(ctx, clusterID, namespace) })
		},
		namespaceOf: func(item domainresource.CronJobView) string { return item.Namespace }, populate: populateAllowedActionsCronJobs,
	})
}

func (w *Workloads) ListReplicaSets(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.ReplicaSetView, error) {
	return listWorkloadResources(ctx, w, principal, clusterID, namespace, workloadListSpec[domainresource.ReplicaSetView]{
		kind: "ReplicaSet", auditText: "listed replicasets",
		agent: func(client WorkloadAgent) ([]domainresource.ReplicaSetView, error) {
			return client.ListReplicaSets(ctx, namespace)
		},
		direct: func() ([]domainresource.ReplicaSetView, string, error) {
			return liveWorkload(func() ([]domainresource.ReplicaSetView, error) {
				return w.direct.ListReplicaSets(ctx, clusterID, namespace)
			})
		},
		namespaceOf: func(item domainresource.ReplicaSetView) string { return item.Namespace }, populate: populateAllowedActionsReplicaSets,
	})
}

func (w *Workloads) GetReplicaSetDetail(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.ReplicaSetDetailView, error) {
	return getWorkloadResource(ctx, w, principal, clusterID, namespace, name, workloadGetSpec[domainresource.ReplicaSetDetailView]{
		kind: "ReplicaSet", auditText: "viewed replicaset detail",
		agent: func(client WorkloadAgent) (domainresource.ReplicaSetDetailView, error) {
			return client.GetReplicaSetDetail(ctx, namespace, name)
		},
		direct: func() (domainresource.ReplicaSetDetailView, string, error) {
			return liveWorkload(func() (domainresource.ReplicaSetDetailView, error) {
				return w.direct.GetReplicaSetDetail(ctx, clusterID, namespace, name)
			})
		},
		finalize: func(item *domainresource.ReplicaSetDetailView, decision domainaccess.Decision) {
			item.AllowedActions = stringifyActions(decision.AllowedActions)
		},
	})
}

func (w *Workloads) ListHorizontalPodAutoscalers(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.HorizontalPodAutoscalerView, error) {
	return listWorkloadResources(ctx, w, principal, clusterID, namespace, workloadListSpec[domainresource.HorizontalPodAutoscalerView]{
		kind: "HorizontalPodAutoscaler", auditText: "listed hpas",
		agent: func(client WorkloadAgent) ([]domainresource.HorizontalPodAutoscalerView, error) {
			return client.ListHorizontalPodAutoscalers(ctx, namespace)
		},
		direct: func() ([]domainresource.HorizontalPodAutoscalerView, string, error) {
			return liveWorkload(func() ([]domainresource.HorizontalPodAutoscalerView, error) {
				return w.direct.ListHorizontalPodAutoscalers(ctx, clusterID, namespace)
			})
		},
		namespaceOf: func(item domainresource.HorizontalPodAutoscalerView) string { return item.Namespace }, populate: populateAllowedActionsHorizontalPodAutoscalers,
	})
}

func (w *Workloads) GetHorizontalPodAutoscalerDetail(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.HorizontalPodAutoscalerDetailView, error) {
	return getWorkloadResource(ctx, w, principal, clusterID, namespace, name, workloadGetSpec[domainresource.HorizontalPodAutoscalerDetailView]{
		kind: "HorizontalPodAutoscaler", auditText: "viewed hpa detail",
		agent: func(client WorkloadAgent) (domainresource.HorizontalPodAutoscalerDetailView, error) {
			return client.GetHorizontalPodAutoscalerDetail(ctx, namespace, name)
		},
		direct: func() (domainresource.HorizontalPodAutoscalerDetailView, string, error) {
			return liveWorkload(func() (domainresource.HorizontalPodAutoscalerDetailView, error) {
				return w.direct.GetHorizontalPodAutoscalerDetail(ctx, clusterID, namespace, name)
			})
		},
		finalize: func(item *domainresource.HorizontalPodAutoscalerDetailView, decision domainaccess.Decision) {
			item.AllowedActions = stringifyActions(decision.AllowedActions)
		},
	})
}

func (w *Workloads) ListPodDisruptionBudgets(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.PodDisruptionBudgetView, error) {
	return listWorkloadResources(ctx, w, principal, clusterID, namespace, workloadListSpec[domainresource.PodDisruptionBudgetView]{
		kind: "PodDisruptionBudget", auditText: "listed pod disruption budgets",
		agent: func(client WorkloadAgent) ([]domainresource.PodDisruptionBudgetView, error) {
			return client.ListPodDisruptionBudgets(ctx, namespace)
		},
		direct: func() ([]domainresource.PodDisruptionBudgetView, string, error) {
			return liveWorkload(func() ([]domainresource.PodDisruptionBudgetView, error) {
				return w.direct.ListPodDisruptionBudgets(ctx, clusterID, namespace)
			})
		},
		namespaceOf: func(item domainresource.PodDisruptionBudgetView) string { return item.Namespace }, populate: populateAllowedActionsPodDisruptionBudgets,
	})
}

func (w *Workloads) GetPodDisruptionBudgetDetail(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.PodDisruptionBudgetDetailView, error) {
	item, err := getWorkloadResource(ctx, w, principal, clusterID, namespace, name, workloadGetSpec[domainresource.PodDisruptionBudgetDetailView]{
		kind: "PodDisruptionBudget", auditText: "viewed pod disruption budget detail",
		agent: func(client WorkloadAgent) (domainresource.PodDisruptionBudgetDetailView, error) {
			return client.GetPodDisruptionBudgetDetail(ctx, namespace, name)
		},
		direct: func() (domainresource.PodDisruptionBudgetDetailView, string, error) {
			return liveWorkload(func() (domainresource.PodDisruptionBudgetDetailView, error) {
				return w.direct.GetPodDisruptionBudgetDetail(ctx, clusterID, namespace, name)
			})
		},
		finalize: func(item *domainresource.PodDisruptionBudgetDetailView, decision domainaccess.Decision) {
			item.AllowedActions = stringifyActions(decision.AllowedActions)
		},
	})
	if err != nil {
		return domainresource.PodDisruptionBudgetDetailView{}, err
	}
	return w.redactPDBRelations(ctx, principal, clusterID, namespace, item), nil
}

func (w *Workloads) redactPDBRelations(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string, item domainresource.PodDisruptionBudgetDetailView) domainresource.PodDisruptionBudgetDetailView {
	_, podsDecision, err := w.decide(ctx, principal, clusterID, namespace, "workloads", "Pod", domainaccess.ActionView)
	if err != nil || !podsDecision.Allowed {
		item.Pods, item.Workload = nil, nil
		return item
	}
	if item.Workload != nil {
		_, workloadDecision, err := w.decide(ctx, principal, clusterID, namespace, "workloads", item.Workload.Kind, domainaccess.ActionView)
		if err != nil || !workloadDecision.Allowed {
			item.Workload = nil
		}
	}
	return item
}

func (w *Workloads) RestartDeployment(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) error {
	return performWorkloadMutation(ctx, w, principal, clusterID, namespace, name, workloadMutationSpec{
		permission: appaccess.PermPlatformDeploymentRestart, kind: "Deployment", action: domainaccess.ActionRestart,
		agent:            func(client WorkloadAgent) error { return client.RestartDeployment(ctx, namespace, name) },
		direct:           func() error { return w.direct.RestartDeployment(ctx, clusterID, namespace, name) },
		successMessage:   func(source string) string { return fmt.Sprintf("restarted deployment via %s", source) },
		auditErrorPrefix: "record restart deployment audit", operation: "platform.deployment.restart",
	})
}

func (w *Workloads) ScaleDeployment(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string, replicas int32) error {
	return performWorkloadMutation(ctx, w, principal, clusterID, namespace, name, workloadMutationSpec{
		permission: appaccess.PermPlatformDeploymentScale, kind: "Deployment", action: domainaccess.ActionScale,
		agent:            func(client WorkloadAgent) error { return client.ScaleDeployment(ctx, namespace, name, replicas) },
		direct:           func() error { return w.direct.ScaleDeployment(ctx, clusterID, namespace, name, replicas) },
		successMessage:   func(source string) string { return fmt.Sprintf("scaled deployment to %d via %s", replicas, source) },
		auditErrorPrefix: "record scale deployment audit", operation: "platform.deployment.scale", attributes: map[string]any{"replicas": replicas},
	})
}

func (w *Workloads) RestartStatefulSet(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) error {
	return performWorkloadMutation(ctx, w, principal, clusterID, namespace, name, workloadMutationSpec{
		permission: appaccess.PermPlatformDeploymentRestart, kind: "StatefulSet", action: domainaccess.ActionRestart,
		agent:            func(client WorkloadAgent) error { return client.RestartStatefulSet(ctx, namespace, name) },
		direct:           func() error { return w.direct.RestartStatefulSet(ctx, clusterID, namespace, name) },
		successMessage:   func(source string) string { return fmt.Sprintf("restarted statefulset via %s", source) },
		auditErrorPrefix: "record restart statefulset audit", operation: "platform.statefulset.restart",
	})
}

func (w *Workloads) ScaleStatefulSet(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string, replicas int32) error {
	return performWorkloadMutation(ctx, w, principal, clusterID, namespace, name, workloadMutationSpec{
		permission: appaccess.PermPlatformDeploymentScale, kind: "StatefulSet", action: domainaccess.ActionScale,
		agent:            func(client WorkloadAgent) error { return client.ScaleStatefulSet(ctx, namespace, name, replicas) },
		direct:           func() error { return w.direct.ScaleStatefulSet(ctx, clusterID, namespace, name, replicas) },
		successMessage:   func(source string) string { return fmt.Sprintf("scaled statefulset to %d via %s", replicas, source) },
		auditErrorPrefix: "record scale statefulset audit", operation: "platform.statefulset.scale", attributes: map[string]any{"replicas": replicas},
	})
}

func (w *Workloads) RestartDaemonSet(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) error {
	return performWorkloadMutation(ctx, w, principal, clusterID, namespace, name, workloadMutationSpec{
		permission: appaccess.PermPlatformDeploymentRestart, kind: "DaemonSet", action: domainaccess.ActionRestart,
		agent:            func(client WorkloadAgent) error { return client.RestartDaemonSet(ctx, namespace, name) },
		direct:           func() error { return w.direct.RestartDaemonSet(ctx, clusterID, namespace, name) },
		successMessage:   func(source string) string { return fmt.Sprintf("restarted daemonset via %s", source) },
		auditErrorPrefix: "record restart daemonset audit", operation: "platform.daemonset.restart",
	})
}
