package resource

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	domainsettings "github.com/opensoha/soha/internal/domain/settings"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type prometheusRangeResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Values [][]any `json:"values"`
		} `json:"result"`
	} `json:"data"`
	Error string `json:"error"`
}

type prometheusInstantResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric map[string]string `json:"metric"`
			Value  []any             `json:"value"`
		} `json:"result"`
	} `json:"data"`
	Error string `json:"error"`
}

type metricDefinition struct {
	Key   string
	Label string
	Unit  string
	Query string
}

type podUsageSummary struct {
	CPU    string
	Memory string
}

type podUsageValue struct {
	CPUCores    float64
	MemoryBytes float64
}

func (s *Service) GetPodMetrics(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, podName string, rangeMinutes, stepSeconds int) (domainresource.PodMetricsView, error) {
	connection, _, err := s.authorize(ctx, principal, clusterID, namespace, "Pod", domainaccess.ActionList)
	if err != nil {
		return domainresource.PodMetricsView{}, err
	}
	view := domainresource.PodMetricsView{
		PodName:      podName,
		Namespace:    namespace,
		Source:       "prometheus",
		GeneratedAt:  time.Now().UTC().Format(time.RFC3339),
		RangeMinutes: rangeMinutes,
		StepSeconds:  stepSeconds,
	}
	if s.settings == nil {
		view.Message = "monitoring settings are unavailable"
		_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Pod", podName, "metrics", "failure", view.Message)
		return view, nil
	}
	settings, err := s.resolveClusterPrometheusSettings(ctx, connection.Summary.ID)
	if err != nil {
		return domainresource.PodMetricsView{}, err
	}
	if rangeMinutes <= 0 {
		rangeMinutes = settings.DefaultRangeMinutes
	}
	if rangeMinutes <= 0 {
		rangeMinutes = 60
	}
	if stepSeconds <= 0 {
		stepSeconds = settings.StepSeconds
	}
	if stepSeconds <= 0 {
		stepSeconds = 60
	}
	view.RangeMinutes = rangeMinutes
	view.StepSeconds = stepSeconds
	view.GrafanaBaseURL = settings.GrafanaBaseURL
	if !settings.Enabled || strings.TrimSpace(settings.BaseURL) == "" {
		view.Message = "prometheus is not configured"
		_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Pod", podName, "metrics", "success", view.Message)
		return view, nil
	}

	view.Configured = true
	queryRange := time.Duration(rangeMinutes) * time.Minute
	definitions := buildPodMetricDefinitions(namespace, podName, clusterID, settings.ClusterLabel)
	fallbackDefinitions := []metricDefinition(nil)
	if hasPrometheusClusterMatcher(clusterID, settings.ClusterLabel) {
		fallbackDefinitions = buildPodMetricDefinitions(namespace, podName, "", "")
	}
	series, firstError := s.queryMetricSeriesWithFallback(
		ctx,
		settings.BaseURL,
		settings.BearerToken,
		definitions,
		fallbackDefinitions,
		namespace,
		[]string{podName},
		queryRange,
		time.Duration(stepSeconds)*time.Second,
	)
	view.Series = series
	if firstError != "" && len(series) == 0 {
		view.Message = firstError
	}
	if firstError == "" && len(series) == 0 {
		view.Message = "prometheus returned no pod metrics for the selected range"
	}
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Pod", podName, "metrics", "success", fmt.Sprintf("read pod metrics via prometheus with %d series", len(series)))
	return view, nil
}

func (s *Service) GetDeploymentMetrics(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, deploymentName string, rangeMinutes, stepSeconds int) (domainresource.ResourceMetricsView, error) {
	view := domainresource.ResourceMetricsView{
		ResourceKind: "Deployment",
		ResourceName: deploymentName,
		Namespace:    namespace,
		Source:       "prometheus",
		GeneratedAt:  time.Now().UTC().Format(time.RFC3339),
		RangeMinutes: rangeMinutes,
		StepSeconds:  stepSeconds,
	}
	connection, _, err := s.authorize(ctx, principal, clusterID, namespace, "Deployment", domainaccess.ActionList)
	if err != nil {
		return view, err
	}
	detail, err := s.GetDeploymentDetail(ctx, principal, clusterID, namespace, deploymentName)
	if err != nil {
		return view, err
	}
	pods, err := s.ListPods(ctx, principal, clusterID, namespace)
	if err != nil {
		return view, err
	}
	names := selectPodsBySelector(pods, detail.Selector)
	return s.queryResourceMetrics(ctx, principal, connection.Summary.ID, namespace, "Deployment", deploymentName, names, rangeMinutes, stepSeconds)
}

func (s *Service) GetStatefulSetMetrics(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, statefulSetName string, rangeMinutes, stepSeconds int) (domainresource.ResourceMetricsView, error) {
	view := domainresource.ResourceMetricsView{
		ResourceKind: "StatefulSet",
		ResourceName: statefulSetName,
		Namespace:    namespace,
		Source:       "prometheus",
		GeneratedAt:  time.Now().UTC().Format(time.RFC3339),
		RangeMinutes: rangeMinutes,
		StepSeconds:  stepSeconds,
	}
	connection, _, err := s.authorize(ctx, principal, clusterID, namespace, "StatefulSet", domainaccess.ActionList)
	if err != nil {
		return view, err
	}
	detail, err := s.GetStatefulSetDetail(ctx, principal, clusterID, namespace, statefulSetName)
	if err != nil {
		return view, err
	}
	pods, err := s.ListPods(ctx, principal, clusterID, namespace)
	if err != nil {
		return view, err
	}
	names := selectPodsBySelector(pods, detail.Selector)
	return s.queryResourceMetrics(ctx, principal, connection.Summary.ID, namespace, "StatefulSet", statefulSetName, names, rangeMinutes, stepSeconds)
}

func (s *Service) GetDaemonSetMetrics(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, daemonSetName string, rangeMinutes, stepSeconds int) (domainresource.ResourceMetricsView, error) {
	view := domainresource.ResourceMetricsView{
		ResourceKind: "DaemonSet",
		ResourceName: daemonSetName,
		Namespace:    namespace,
		Source:       "prometheus",
		GeneratedAt:  time.Now().UTC().Format(time.RFC3339),
		RangeMinutes: rangeMinutes,
		StepSeconds:  stepSeconds,
	}
	connection, _, err := s.authorize(ctx, principal, clusterID, namespace, "DaemonSet", domainaccess.ActionList)
	if err != nil {
		return view, err
	}
	detail, err := s.GetDaemonSetDetail(ctx, principal, clusterID, namespace, daemonSetName)
	if err != nil {
		return view, err
	}
	pods, err := s.ListPods(ctx, principal, clusterID, namespace)
	if err != nil {
		return view, err
	}
	names := selectPodsBySelector(pods, detail.Selector)
	return s.queryResourceMetrics(ctx, principal, connection.Summary.ID, namespace, "DaemonSet", daemonSetName, names, rangeMinutes, stepSeconds)
}

func (s *Service) GetServiceMetrics(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, serviceName string, rangeMinutes, stepSeconds int) (domainresource.ResourceMetricsView, error) {
	view := domainresource.ResourceMetricsView{
		ResourceKind: "Service",
		ResourceName: serviceName,
		Namespace:    namespace,
		Source:       "prometheus",
		GeneratedAt:  time.Now().UTC().Format(time.RFC3339),
		RangeMinutes: rangeMinutes,
		StepSeconds:  stepSeconds,
	}
	connection, _, err := s.authorize(ctx, principal, clusterID, namespace, "Service", domainaccess.ActionList)
	if err != nil {
		return view, err
	}
	services, err := s.ListServices(ctx, principal, clusterID, namespace)
	if err != nil {
		return view, err
	}
	var selector map[string]string
	for _, service := range services {
		if service.Name == serviceName {
			selector = service.Selector
			break
		}
	}
	pods, err := s.ListPods(ctx, principal, clusterID, namespace)
	if err != nil {
		return view, err
	}
	names := selectPodsBySelector(pods, selector)
	return s.queryResourceMetrics(ctx, principal, connection.Summary.ID, namespace, "Service", serviceName, names, rangeMinutes, stepSeconds)
}

func (s *Service) queryResourceMetrics(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, kind, name string, podNames []string, rangeMinutes, stepSeconds int) (domainresource.ResourceMetricsView, error) {
	view := domainresource.ResourceMetricsView{
		ResourceKind: kind,
		ResourceName: name,
		Namespace:    namespace,
		Source:       "prometheus",
		GeneratedAt:  time.Now().UTC().Format(time.RFC3339),
		RangeMinutes: rangeMinutes,
		StepSeconds:  stepSeconds,
	}
	if s.settings == nil {
		view.Message = "monitoring settings are unavailable"
		return view, nil
	}
	settings, err := s.resolveClusterPrometheusSettings(ctx, clusterID)
	if err != nil {
		return view, err
	}
	if rangeMinutes <= 0 {
		rangeMinutes = settings.DefaultRangeMinutes
	}
	if rangeMinutes <= 0 {
		rangeMinutes = 60
	}
	if stepSeconds <= 0 {
		stepSeconds = settings.StepSeconds
	}
	if stepSeconds <= 0 {
		stepSeconds = 60
	}
	view.RangeMinutes = rangeMinutes
	view.StepSeconds = stepSeconds
	view.GrafanaBaseURL = settings.GrafanaBaseURL
	if !settings.Enabled || strings.TrimSpace(settings.BaseURL) == "" {
		view.Message = "prometheus is not configured"
		return view, nil
	}
	if len(podNames) == 0 {
		view.Message = "no matching pods were resolved for the selected resource"
		return view, nil
	}
	view.Configured = true
	queryRange := time.Duration(rangeMinutes) * time.Minute
	definitions := buildPodSetMetricDefinitions(namespace, podNames, clusterID, settings.ClusterLabel)
	fallbackDefinitions := []metricDefinition(nil)
	if hasPrometheusClusterMatcher(clusterID, settings.ClusterLabel) {
		fallbackDefinitions = buildPodSetMetricDefinitions(namespace, podNames, "", "")
	}
	series, firstError := s.queryMetricSeriesWithFallback(
		ctx,
		settings.BaseURL,
		settings.BearerToken,
		definitions,
		fallbackDefinitions,
		namespace,
		podNames,
		queryRange,
		time.Duration(stepSeconds)*time.Second,
	)
	view.Series = series
	if firstError != "" && len(series) == 0 {
		view.Message = firstError
	}
	if firstError == "" && len(series) == 0 {
		view.Message = "prometheus returned no metrics for the selected resource"
	}
	_ = s.recordAudit(ctx, principal, clusterID, namespace, kind, name, "metrics", "success", fmt.Sprintf("read %s metrics via prometheus with %d series", strings.ToLower(kind), len(series)))
	return view, nil
}

func buildPodMetricDefinitions(namespace, podName, clusterID, clusterLabel string) []metricDefinition {
	matchers := []string{
		fmt.Sprintf(`namespace=%q`, namespace),
		fmt.Sprintf(`pod=%q`, podName),
		`container!=""`,
		`container!="POD"`,
	}
	if strings.TrimSpace(clusterLabel) != "" && strings.TrimSpace(clusterID) != "" {
		matchers = append(matchers, fmt.Sprintf(`%s=%q`, clusterLabel, clusterID))
	}
	filter := strings.Join(matchers, ",")
	return []metricDefinition{
		{
			Key:   "cpu",
			Label: "CPU Usage",
			Unit:  "cores",
			Query: fmt.Sprintf(`sum(rate(container_cpu_usage_seconds_total{%s}[5m]))`, filter),
		},
		{
			Key:   "memory",
			Label: "Memory Working Set",
			Unit:  "bytes",
			Query: fmt.Sprintf(`sum(container_memory_working_set_bytes{%s})`, filter),
		},
		{
			Key:   "network_rx",
			Label: "Network Receive",
			Unit:  "bytes/s",
			Query: fmt.Sprintf(`sum(rate(container_network_receive_bytes_total{%s}[5m]))`, filter),
		},
		{
			Key:   "network_tx",
			Label: "Network Transmit",
			Unit:  "bytes/s",
			Query: fmt.Sprintf(`sum(rate(container_network_transmit_bytes_total{%s}[5m]))`, filter),
		},
		{
			Key:   "disk_read",
			Label: "Disk Read",
			Unit:  "bytes/s",
			Query: fmt.Sprintf(`sum(rate(container_fs_reads_bytes_total{%s}[5m]))`, filter),
		},
		{
			Key:   "disk_write",
			Label: "Disk Write",
			Unit:  "bytes/s",
			Query: fmt.Sprintf(`sum(rate(container_fs_writes_bytes_total{%s}[5m]))`, filter),
		},
		{
			Key:   "connections",
			Label: "Open Connections",
			Unit:  "count",
			Query: fmt.Sprintf(`sum(container_sockets{%s})`, filter),
		},
	}
}

func buildPodSetMetricDefinitions(namespace string, podNames []string, clusterID, clusterLabel string) []metricDefinition {
	escapedPods := make([]string, 0, len(podNames))
	for _, podName := range podNames {
		podName = strings.TrimSpace(podName)
		if podName == "" {
			continue
		}
		escapedPods = append(escapedPods, regexp.QuoteMeta(podName))
	}
	if len(escapedPods) == 0 {
		return nil
	}
	matchers := []string{
		fmt.Sprintf(`namespace=%q`, namespace),
		fmt.Sprintf(`pod=~%q`, strings.Join(escapedPods, "|")),
		`container!=""`,
		`container!="POD"`,
	}
	if strings.TrimSpace(clusterLabel) != "" && strings.TrimSpace(clusterID) != "" {
		matchers = append(matchers, fmt.Sprintf(`%s=%q`, clusterLabel, clusterID))
	}
	filter := strings.Join(matchers, ",")
	return []metricDefinition{
		{Key: "cpu", Label: "CPU Usage", Unit: "cores", Query: fmt.Sprintf(`sum(rate(container_cpu_usage_seconds_total{%s}[5m]))`, filter)},
		{Key: "memory", Label: "Memory Working Set", Unit: "bytes", Query: fmt.Sprintf(`sum(container_memory_working_set_bytes{%s})`, filter)},
		{Key: "network_rx", Label: "Network Receive", Unit: "bytes/s", Query: fmt.Sprintf(`sum(rate(container_network_receive_bytes_total{%s}[5m]))`, filter)},
		{Key: "network_tx", Label: "Network Transmit", Unit: "bytes/s", Query: fmt.Sprintf(`sum(rate(container_network_transmit_bytes_total{%s}[5m]))`, filter)},
		{Key: "disk_read", Label: "Disk Read", Unit: "bytes/s", Query: fmt.Sprintf(`sum(rate(container_fs_reads_bytes_total{%s}[5m]))`, filter)},
		{Key: "disk_write", Label: "Disk Write", Unit: "bytes/s", Query: fmt.Sprintf(`sum(rate(container_fs_writes_bytes_total{%s}[5m]))`, filter)},
		{Key: "connections", Label: "Open Connections", Unit: "count", Query: fmt.Sprintf(`sum(container_sockets{%s})`, filter)},
	}
}

func buildPodSetMetricMatcher(namespace string, podNames []string, clusterID, clusterLabel string) string {
	escapedPods := make([]string, 0, len(podNames))
	for _, podName := range podNames {
		podName = strings.TrimSpace(podName)
		if podName == "" {
			continue
		}
		escapedPods = append(escapedPods, regexp.QuoteMeta(podName))
	}
	if len(escapedPods) == 0 {
		return ""
	}
	matchers := []string{
		fmt.Sprintf(`namespace=%q`, namespace),
		fmt.Sprintf(`pod=~%q`, strings.Join(escapedPods, "|")),
		`container!=""`,
		`container!="POD"`,
	}
	if strings.TrimSpace(clusterLabel) != "" && strings.TrimSpace(clusterID) != "" {
		matchers = append(matchers, fmt.Sprintf(`%s=%q`, clusterLabel, clusterID))
	}
	return strings.Join(matchers, ",")
}

func (s *Service) listPodUsageSummaries(ctx context.Context, clusterID, namespace string, pods []domainresource.PodView) map[string]podUsageSummary {
	if len(pods) == 0 {
		return nil
	}

	settings, err := s.resolveClusterPrometheusSettings(ctx, clusterID)
	if err != nil || !settings.Enabled || strings.TrimSpace(settings.BaseURL) == "" {
		return nil
	}

	rawPods := make([]corev1.Pod, 0, len(pods))
	for _, pod := range pods {
		rawPods = append(rawPods, corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: pod.Name, Namespace: pod.Namespace}})
	}

	values, err := s.listPodUsageValues(ctx, settings, clusterID, rawPods)
	if err != nil {
		return nil
	}

	out := make(map[string]podUsageSummary, len(pods))
	for _, pod := range pods {
		if value, ok := values[podMetricsKey(pod.Namespace, pod.Name)]; ok {
			summary := podUsageSummary{}
			if value.CPUCores > 0 {
				summary.CPU = formatCPUUsage(value.CPUCores)
			}
			if value.MemoryBytes > 0 {
				summary.Memory = formatMemoryBytes(value.MemoryBytes)
			}
			if summary.CPU != "" || summary.Memory != "" {
				out[podMetricsKey(pod.Namespace, pod.Name)] = summary
			}
		}
	}
	return out
}

func (s *Service) resolveClusterPrometheusSettings(ctx context.Context, clusterID string) (domainsettings.PrometheusSettings, error) {
	settings := domainsettings.PrometheusSettings{
		DefaultRangeMinutes: 60,
		StepSeconds:         60,
	}
	if s.settings != nil {
		resolved, err := s.settings.ResolveMonitoringSettings(ctx)
		if err != nil {
			return settings, err
		}
		settings = resolved.Prometheus
	}
	applyPrometheusMetadataOverride(&settings, s.clusterPrometheusMetadata(ctx, clusterID))
	if settings.DefaultRangeMinutes <= 0 {
		settings.DefaultRangeMinutes = 60
	}
	if settings.StepSeconds <= 0 {
		settings.StepSeconds = 60
	}
	if strings.TrimSpace(settings.BaseURL) != "" {
		settings.Enabled = true
	}
	return settings, nil
}

func (s *Service) clusterPrometheusMetadata(ctx context.Context, clusterID string) map[string]any {
	if s.resolver == nil || strings.TrimSpace(clusterID) == "" {
		return nil
	}
	connection, err := s.resolver.GetConnection(ctx, clusterID)
	if err != nil {
		return nil
	}
	return connection.Metadata
}

func applyPrometheusMetadataOverride(settings *domainsettings.PrometheusSettings, metadata map[string]any) {
	if settings == nil || metadata == nil {
		return
	}
	if value := strings.TrimSpace(metadataValue(metadata, "prometheus_url")); value != "" {
		settings.BaseURL = value
		settings.Enabled = true
	}
	if value := strings.TrimSpace(metadataValue(metadata, "prometheus_bearer_token")); value != "" {
		settings.BearerToken = value
	}
	if value := strings.TrimSpace(metadataValue(metadata, "prometheus_cluster_label")); value != "" {
		settings.ClusterLabel = value
	}
	if value := strings.TrimSpace(metadataValue(metadata, "grafana_base_url")); value != "" {
		settings.GrafanaBaseURL = value
	}
}

func metadataValue(metadata map[string]any, key string) string {
	value, _ := metadata[key].(string)
	return value
}

func podMetricsKey(namespace, name string) string {
	return strings.TrimSpace(namespace) + "/" + strings.TrimSpace(name)
}

func (s *Service) listPodUsageValues(ctx context.Context, settings domainsettings.PrometheusSettings, clusterID string, pods []corev1.Pod) (map[string]podUsageValue, error) {
	if len(pods) == 0 || !settings.Enabled || strings.TrimSpace(settings.BaseURL) == "" {
		return nil, nil
	}

	podsByNamespace := make(map[string][]string)
	seen := make(map[string]struct{}, len(pods))
	for _, pod := range pods {
		namespace := strings.TrimSpace(pod.Namespace)
		name := strings.TrimSpace(pod.Name)
		if namespace == "" || name == "" {
			continue
		}
		key := podMetricsKey(namespace, name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		podsByNamespace[namespace] = append(podsByNamespace[namespace], name)
	}

	if len(podsByNamespace) == 0 {
		return nil, nil
	}

	out := make(map[string]podUsageValue, len(pods))
	var firstErr error
	for namespace, names := range podsByNamespace {
		filter := buildPodSetMetricMatcher(namespace, names, clusterID, settings.ClusterLabel)
		if filter == "" {
			continue
		}

		cpuByPod, cpuErr := s.queryPrometheusInstantByPod(ctx, settings.BaseURL, settings.BearerToken, fmt.Sprintf(`sum by (pod) (rate(container_cpu_usage_seconds_total{%s}[5m]))`, filter))
		memoryByPod, memoryErr := s.queryPrometheusInstantByPod(ctx, settings.BaseURL, settings.BearerToken, fmt.Sprintf(`sum by (pod) (container_memory_working_set_bytes{%s})`, filter))
		if hasPrometheusClusterMatcher(clusterID, settings.ClusterLabel) && len(cpuByPod) == 0 && len(memoryByPod) == 0 {
			fallbackFilter := buildPodSetMetricMatcher(namespace, names, "", "")
			fallbackCPUByPod, fallbackCPUErr := s.queryPrometheusInstantByPod(ctx, settings.BaseURL, settings.BearerToken, fmt.Sprintf(`sum by (pod) (rate(container_cpu_usage_seconds_total{%s}[5m]))`, fallbackFilter))
			fallbackMemoryByPod, fallbackMemoryErr := s.queryPrometheusInstantByPod(ctx, settings.BaseURL, settings.BearerToken, fmt.Sprintf(`sum by (pod) (container_memory_working_set_bytes{%s})`, fallbackFilter))
			if len(fallbackCPUByPod) > 0 || len(fallbackMemoryByPod) > 0 {
				cpuByPod = fallbackCPUByPod
				memoryByPod = fallbackMemoryByPod
				cpuErr = fallbackCPUErr
				memoryErr = fallbackMemoryErr
			} else {
				cpuErr = firstNonNilError(cpuErr, fallbackCPUErr)
				memoryErr = firstNonNilError(memoryErr, fallbackMemoryErr)
			}
		}
		if cpuErr != nil && memoryErr != nil {
			if firstErr == nil {
				firstErr = firstNonNilError(cpuErr, memoryErr)
			}
			continue
		}

		for _, name := range names {
			usage := out[podMetricsKey(namespace, name)]
			if value, ok := cpuByPod[name]; ok {
				usage.CPUCores = value
			}
			if value, ok := memoryByPod[name]; ok {
				usage.MemoryBytes = value
			}
			if usage.CPUCores > 0 || usage.MemoryBytes > 0 {
				out[podMetricsKey(namespace, name)] = usage
			}
		}
	}

	if len(out) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return out, nil
}

func hasPrometheusClusterMatcher(clusterID, clusterLabel string) bool {
	return strings.TrimSpace(clusterID) != "" && strings.TrimSpace(clusterLabel) != ""
}

func (s *Service) canUseUnscopedPodMetricsFallback(ctx context.Context, baseURL, bearerToken, namespace string, podNames []string) (bool, error) {
	filter := buildPodSetMetricMatcher(namespace, podNames, "", "")
	if filter == "" {
		return false, nil
	}
	counts, err := s.queryPrometheusInstantByPod(
		ctx,
		baseURL,
		bearerToken,
		fmt.Sprintf(`count by (pod) (count by (pod, job, instance) (container_memory_working_set_bytes{%s}))`, filter),
	)
	if err != nil || len(counts) == 0 {
		return false, nil
	}
	for _, count := range counts {
		if count > 1.001 {
			return false, fmt.Errorf("prometheus fallback was skipped because matching pod metrics were ambiguous without the configured cluster label")
		}
	}
	return true, nil
}

func firstNonNilError(errors ...error) error {
	for _, err := range errors {
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) queryMetricSeries(ctx context.Context, baseURL, bearerToken string, definitions []metricDefinition, queryRange, step time.Duration) ([]domainresource.MetricSeriesView, string) {
	series := make([]domainresource.MetricSeriesView, 0, len(definitions))
	var firstError string
	for _, definition := range definitions {
		points, latest, queryErr := s.queryPrometheusRange(ctx, baseURL, bearerToken, definition.Query, queryRange, step)
		if queryErr != nil {
			if firstError == "" {
				firstError = queryErr.Error()
			}
			continue
		}
		if len(points) == 0 {
			continue
		}
		series = append(series, domainresource.MetricSeriesView{
			Key:    definition.Key,
			Label:  definition.Label,
			Unit:   definition.Unit,
			Latest: latest,
			Points: points,
		})
	}
	return series, firstError
}

func (s *Service) queryMetricSeriesWithFallback(ctx context.Context, baseURL, bearerToken string, definitions, fallbackDefinitions []metricDefinition, namespace string, podNames []string, queryRange, step time.Duration) ([]domainresource.MetricSeriesView, string) {
	series, firstError := s.queryMetricSeries(ctx, baseURL, bearerToken, definitions, queryRange, step)
	if len(series) > 0 || len(fallbackDefinitions) == 0 {
		return series, firstError
	}
	canFallback, fallbackCheckErr := s.canUseUnscopedPodMetricsFallback(ctx, baseURL, bearerToken, namespace, podNames)
	if fallbackCheckErr != nil {
		if firstError == "" {
			firstError = fallbackCheckErr.Error()
		}
		return series, firstError
	}
	if !canFallback {
		return series, firstError
	}
	fallbackSeries, fallbackError := s.queryMetricSeries(ctx, baseURL, bearerToken, fallbackDefinitions, queryRange, step)
	if len(fallbackSeries) > 0 {
		return fallbackSeries, ""
	}
	if firstError == "" {
		firstError = fallbackError
	}
	return series, firstError
}

func selectPodsBySelector(pods []domainresource.PodView, selector map[string]string) []string {
	if len(selector) == 0 {
		return nil
	}
	items := make([]string, 0)
	for _, pod := range pods {
		if labelsMatchSelector(pod.Labels, selector) {
			items = append(items, pod.Name)
		}
	}
	return items
}

func labelsMatchSelector(labels map[string]string, selector map[string]string) bool {
	if len(selector) == 0 {
		return false
	}
	for key, value := range selector {
		if strings.TrimSpace(labels[key]) != strings.TrimSpace(value) {
			return false
		}
	}
	return true
}

func (s *Service) queryPrometheusRange(ctx context.Context, baseURL, bearerToken, promQuery string, queryRange, step time.Duration) ([]domainresource.MetricPointView, float64, error) {
	end := time.Now().UTC()
	start := end.Add(-queryRange)
	requestURL, err := url.Parse(strings.TrimRight(baseURL, "/") + "/api/v1/query_range")
	if err != nil {
		return nil, 0, fmt.Errorf("%w: invalid prometheus url", apperrors.ErrInvalidArgument)
	}
	params := requestURL.Query()
	params.Set("query", promQuery)
	params.Set("start", strconv.FormatInt(start.Unix(), 10))
	params.Set("end", strconv.FormatInt(end.Unix(), 10))
	params.Set("step", strconv.FormatInt(int64(step.Seconds()), 10))
	requestURL.RawQuery = params.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return nil, 0, err
	}
	if strings.TrimSpace(bearerToken) != "" {
		request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(bearerToken))
	}
	response, err := s.httpClient.Do(request)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	defer response.Body.Close()
	if response.StatusCode >= http.StatusBadRequest {
		return nil, 0, fmt.Errorf("%w: prometheus returned %s", apperrors.ErrClusterUnready, response.Status)
	}
	var payload prometheusRangeResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, 0, err
	}
	if payload.Status != "success" {
		return nil, 0, fmt.Errorf("%w: %s", apperrors.ErrClusterUnready, strings.TrimSpace(payload.Error))
	}
	pointMap := map[int64]float64{}
	for _, result := range payload.Data.Result {
		for _, value := range result.Values {
			if len(value) < 2 {
				continue
			}
			timestamp, ok := asUnixTimestamp(value[0])
			if !ok {
				continue
			}
			floatValue, ok := asPrometheusFloat(value[1])
			if !ok {
				continue
			}
			pointMap[timestamp] += floatValue
		}
	}
	if len(pointMap) == 0 {
		return nil, 0, nil
	}
	timestamps := make([]int64, 0, len(pointMap))
	for timestamp := range pointMap {
		timestamps = append(timestamps, timestamp)
	}
	sort.Slice(timestamps, func(i, j int) bool { return timestamps[i] < timestamps[j] })
	points := make([]domainresource.MetricPointView, 0, len(timestamps))
	latest := 0.0
	for _, timestamp := range timestamps {
		value := pointMap[timestamp]
		latest = value
		points = append(points, domainresource.MetricPointView{
			Timestamp: time.Unix(timestamp, 0).UTC().Format(time.RFC3339),
			Value:     value,
		})
	}
	return points, latest, nil
}

func (s *Service) queryPrometheusInstantByPod(ctx context.Context, baseURL, bearerToken, promQuery string) (map[string]float64, error) {
	requestURL, err := url.Parse(strings.TrimRight(baseURL, "/") + "/api/v1/query")
	if err != nil {
		return nil, fmt.Errorf("%w: invalid prometheus url", apperrors.ErrInvalidArgument)
	}
	params := requestURL.Query()
	params.Set("query", promQuery)
	params.Set("time", strconv.FormatInt(time.Now().UTC().Unix(), 10))
	requestURL.RawQuery = params.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(bearerToken) != "" {
		request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(bearerToken))
	}

	response, err := s.httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	if response.StatusCode >= 400 {
		return nil, fmt.Errorf("prometheus request failed with status %d", response.StatusCode)
	}

	var payload prometheusInstantResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, err
	}
	if payload.Status != "success" {
		if strings.TrimSpace(payload.Error) != "" {
			return nil, errors.New(payload.Error)
		}
		return nil, fmt.Errorf("prometheus instant query failed")
	}

	results := make(map[string]float64, len(payload.Data.Result))
	for _, item := range payload.Data.Result {
		podName := strings.TrimSpace(item.Metric["pod"])
		if podName == "" || len(item.Value) < 2 {
			continue
		}
		floatValue, ok := asPrometheusFloat(item.Value[1])
		if !ok {
			continue
		}
		results[podName] = floatValue
	}
	return results, nil
}

func asUnixTimestamp(value any) (int64, bool) {
	switch typed := value.(type) {
	case float64:
		return int64(typed), true
	case int64:
		return typed, true
	case int:
		return int64(typed), true
	default:
		return 0, false
	}
}

func asPrometheusFloat(value any) (float64, bool) {
	switch typed := value.(type) {
	case string:
		floatValue, err := strconv.ParseFloat(typed, 64)
		if err != nil {
			return 0, false
		}
		return floatValue, true
	case float64:
		return typed, true
	default:
		return 0, false
	}
}

func formatCPUUsage(value float64) string {
	if value == 0 {
		return "0"
	}
	if value < 1 {
		return fmt.Sprintf("%.0fm", value*1000)
	}
	return fmt.Sprintf("%.2f", value)
}

func formatMemoryBytes(value float64) string {
	if value == 0 {
		return "0 B"
	}
	const (
		kib = 1024
		mib = 1024 * kib
		gib = 1024 * mib
	)
	switch {
	case value >= gib:
		return fmt.Sprintf("%.1f Gi", value/gib)
	case value >= mib:
		return fmt.Sprintf("%.1f Mi", value/mib)
	case value >= kib:
		return fmt.Sprintf("%.1f Ki", value/kib)
	default:
		return fmt.Sprintf("%.0f B", value)
	}
}
