package resource

import (
	"context"
	"sort"

	domainsettings "github.com/opensoha/soha/internal/domain/settings"
)

// listScopedPodUsageValues batches authorized identities by namespace and
// preserves the configured cluster matcher on every query.
func (s *metricsSupport) listScopedPodUsageValues(ctx context.Context, settings domainsettings.PrometheusSettings, clusterID string, pods []podIdentity) (map[string]podUsageValue, error) {
	ctx = context.WithValue(ctx, prometheusClusterKey{}, clusterID)
	grouped := groupPodNamesByNamespace(pods)
	namespaces := make([]string, 0, len(grouped))
	for namespace := range grouped {
		namespaces = append(namespaces, namespace)
	}
	sort.Strings(namespaces)
	out := make(map[string]podUsageValue, len(pods))
	var firstErr error
	for _, namespace := range namespaces {
		if ctx.Err() != nil {
			firstErr = firstNonNilError(firstErr, ctx.Err())
			break
		}
		names := grouped[namespace]
		filter := buildPodSetMetricMatcher(namespace, names, prometheusClusterValue(settings, clusterID), settings.ClusterLabel)
		usage := s.queryPodUsageWithFilter(ctx, settings, filter)
		firstErr = firstNonNilError(firstErr, usage.cpuErr, usage.memoryErr)
		mergeNamespacePodUsage(out, namespace, names, usage)
	}
	if len(out) == 0 {
		return nil, firstErr
	}
	return out, nil
}
