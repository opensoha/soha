package resource

import (
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
)

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
