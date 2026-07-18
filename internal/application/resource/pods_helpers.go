package resource

import (
	"context"
	"sort"
	"strings"
	"time"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
)

func normalizeTerminalShell(shell string) string {
	switch strings.TrimSpace(shell) {
	case "/bin/bash":
		return "/bin/bash"
	case "/bin/ash":
		return "/bin/ash"
	case "/busybox/sh":
		return "/busybox/sh"
	default:
		return "/bin/sh"
	}
}

func shouldPopulatePodUsageSummaries(namespace string) bool {
	return strings.TrimSpace(namespace) != ""
}

func buildWorkloadOverview(clusterID, namespace, source string, items []domainresource.PodView) domainresource.WorkloadOverviewView {
	view := domainresource.WorkloadOverviewView{
		ClusterID:   clusterID,
		Namespace:   strings.TrimSpace(namespace),
		Source:      source,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	}
	namespaceSummary := make(map[string]*domainresource.WorkloadOverviewNamespaceView, len(items))
	problematicPods := make([]domainresource.WorkloadOverviewPodView, 0)
	for _, item := range items {
		if problematic := accumulateWorkloadOverview(&view, namespaceSummary, item); problematic != nil {
			problematicPods = append(problematicPods, *problematic)
		}
	}
	view.NamespaceBreakdown = topWorkloadNamespaces(namespaceSummary, 6)
	view.ProblematicPods = topProblematicPods(problematicPods, 8)
	return view
}

func accumulateWorkloadOverview(view *domainresource.WorkloadOverviewView, namespaces map[string]*domainresource.WorkloadOverviewNamespaceView, item domainresource.PodView) *domainresource.WorkloadOverviewPodView {
	view.TotalPods++
	phase := normalizedPodPhase(item.Phase)
	incrementWorkloadPhase(view, phase)
	attention := podNeedsAttention(item)
	if item.Restarts > 0 {
		view.RestartingPods++
	}
	if attention {
		view.AtRiskPods++
	}
	summary := namespaces[item.Namespace]
	if summary == nil {
		summary = &domainresource.WorkloadOverviewNamespaceView{Namespace: item.Namespace}
		namespaces[item.Namespace] = summary
	}
	summary.TotalPods++
	if phase == "Running" {
		summary.RunningPods++
	}
	if item.Restarts > 0 {
		summary.RestartingPods++
	}
	if attention {
		summary.AtRiskPods++
		return &domainresource.WorkloadOverviewPodView{
			Name: item.Name, Namespace: item.Namespace, Phase: phase,
			ReadyContainers: item.ReadyContainers, Restarts: item.Restarts,
			NodeName: item.NodeName, AgeSeconds: item.AgeSeconds,
		}
	}
	return nil
}

func incrementWorkloadPhase(view *domainresource.WorkloadOverviewView, phase string) {
	switch phase {
	case "Running":
		view.RunningPods++
	case "Pending":
		view.PendingPods++
	case "Succeeded":
		view.SucceededPods++
	case "Failed":
		view.FailedPods++
	default:
		view.UnknownPods++
	}
}

func topWorkloadNamespaces(items map[string]*domainresource.WorkloadOverviewNamespaceView, limit int) []domainresource.WorkloadOverviewNamespaceView {
	result := make([]domainresource.WorkloadOverviewNamespaceView, 0, len(items))
	for _, item := range items {
		result = append(result, *item)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].AtRiskPods != result[j].AtRiskPods {
			return result[i].AtRiskPods > result[j].AtRiskPods
		}
		if result[i].RestartingPods != result[j].RestartingPods {
			return result[i].RestartingPods > result[j].RestartingPods
		}
		if result[i].TotalPods != result[j].TotalPods {
			return result[i].TotalPods > result[j].TotalPods
		}
		return result[i].Namespace < result[j].Namespace
	})
	if len(result) > limit {
		return result[:limit]
	}
	return result
}

func topProblematicPods(items []domainresource.WorkloadOverviewPodView, limit int) []domainresource.WorkloadOverviewPodView {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Restarts != items[j].Restarts {
			return items[i].Restarts > items[j].Restarts
		}
		if podPhaseSeverity(items[i].Phase) != podPhaseSeverity(items[j].Phase) {
			return podPhaseSeverity(items[i].Phase) > podPhaseSeverity(items[j].Phase)
		}
		if items[i].AgeSeconds != items[j].AgeSeconds {
			return items[i].AgeSeconds > items[j].AgeSeconds
		}
		if items[i].Namespace != items[j].Namespace {
			return items[i].Namespace < items[j].Namespace
		}
		return items[i].Name < items[j].Name
	})
	if len(items) > limit {
		return items[:limit]
	}
	return items
}

func normalizedPodPhase(phase string) string {
	trimmed := strings.TrimSpace(phase)
	if trimmed == "" {
		return "Unknown"
	}
	return trimmed
}

func podNeedsAttention(item domainresource.PodView) bool {
	if item.Restarts > 0 {
		return true
	}
	switch normalizedPodPhase(item.Phase) {
	case "Pending", "Failed", "Unknown":
		return true
	default:
		return false
	}
}

func podPhaseSeverity(phase string) int {
	switch normalizedPodPhase(phase) {
	case "Failed":
		return 4
	case "Pending":
		return 3
	case "Unknown":
		return 2
	case "Running":
		return 1
	default:
		return 0
	}
}

func selectorMatchesPodLabels(selector, labels map[string]string) bool {
	if len(selector) == 0 {
		return false
	}
	for key, value := range selector {
		if labels[key] != value {
			return false
		}
	}
	return true
}

func filterPodsBySelector(items []domainresource.PodView, selector map[string]string) []domainresource.PodView {
	if len(selector) == 0 {
		return nil
	}
	filtered := make([]domainresource.PodView, 0, len(items))
	for _, item := range items {
		if selectorMatchesPodLabels(selector, item.Labels) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func (s *Workloads) listPodViews(ctx context.Context, clusterID, namespace string, connection domaincluster.Connection, decision domainaccess.Decision, includeUsage bool) ([]domainresource.PodView, string, error) {
	var (
		items  []domainresource.PodView
		source string
	)
	route, err := s.routePods(connection, clusterID)
	if err != nil {
		return nil, "", err
	}
	items, source, err = route.ListPods(ctx, namespace)
	if err != nil {
		return nil, "", route.RuntimeError(err)
	}
	if route.SupportsUsageMetrics() && includeUsage && shouldPopulatePodUsageSummaries(namespace) {
		metricsCtx, metricsCancel := context.WithTimeout(ctx, 1200*time.Millisecond)
		if metrics := s.listPodUsageSummaries(metricsCtx, clusterID, namespace, items); len(metrics) > 0 {
			for index := range items {
				if usage, ok := metrics[podMetricsKey(items[index].Namespace, items[index].Name)]; ok {
					items[index].CPU = usage.CPU
					items[index].Memory = usage.Memory
				}
			}
		}
		metricsCancel()
	}
	items = filterScopedNamespaceItems(items, decision, func(item domainresource.PodView) string { return item.Namespace })
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Name != items[j].Name {
			return items[i].Name < items[j].Name
		}
		return items[i].Namespace < items[j].Namespace
	})
	return items, source, nil
}

func populateAllowedActionsPods(items []domainresource.PodView, decision domainaccess.Decision) {
	for i := range items {
		if len(items[i].AllowedActions) == 0 {
			items[i].AllowedActions = stringifyActions(decision.AllowedActions)
		}
	}
}
