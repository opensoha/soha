package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	domainresource "github.com/opensoha/soha/agent/internal/domain/resource"
	contractruntime "github.com/opensoha/soha/contracts/resource/runtime"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

func (c *Client) GetResourceGraph(ctx context.Context, namespace, kind, name string) (domainresource.ResourceGraph, error) {
	metadataClient := c.metadata
	if c.restConfig != nil {
		var err error
		metadataClient, err = contractruntime.NewResourceGraphMetadataClient(c.restConfig)
		if err != nil {
			return domainresource.ResourceGraph{}, err
		}
	}
	return contractruntime.BuildKubernetesResourceGraphWithNetworkSources(ctx, c.typed, metadataClient, c.typed.CoreV1().RESTClient(), domainresource.ResourceRef{ClusterID: c.cfg.ID, Namespace: namespace, Kind: kind, Name: name})
}

var (
	agentKubescapeConfigurationGVR = schema.GroupVersionResource{Group: "spdx.softwarecomposition.kubescape.io", Version: "v1beta1", Resource: "workloadconfigurationscansummaries"}
	agentKubescapeVulnerabilityGVR = schema.GroupVersionResource{Group: "spdx.softwarecomposition.kubescape.io", Version: "v1beta1", Resource: "vulnerabilitymanifestsummaries"}
)

func (c *Client) GetSecurityPosture(ctx context.Context, namespace string, limit int) (domainresource.SecurityPosture, error) {
	configurations, configErr := c.listKubescapeSummaries(ctx, agentKubescapeConfigurationGVR, namespace)
	vulnerabilities, vulnerabilityErr := c.listKubescapeSummaries(ctx, agentKubescapeVulnerabilityGVR, namespace)
	if apierrors.IsNotFound(configErr) && apierrors.IsNotFound(vulnerabilityErr) {
		return domainresource.SecurityPosture{ClusterID: c.cfg.ID, Provider: "kubescape", Status: "unsupported", GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano), Message: "Kubescape summary resources are not installed or have not produced results.", Findings: []domainresource.SecurityFinding{}, Warnings: []string{}}, nil
	}
	posture := buildAgentKubescapePosture(c.cfg.ID, configurations, vulnerabilities, limit)
	if configErr != nil || vulnerabilityErr != nil {
		posture.Status = "partial"
		posture.Warnings = append(posture.Warnings, "Some Kubescape summary resources were unavailable.")
	}
	return posture, nil
}

func (c *Client) listKubescapeSummaries(ctx context.Context, gvr schema.GroupVersionResource, namespace string) ([]unstructured.Unstructured, error) {
	resource := c.dynamic.Resource(gvr)
	var reader dynamic.ResourceInterface = resource
	if namespace != "" {
		reader = resource.Namespace(namespace)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	page, err := reader.List(queryCtx, metav1.ListOptions{Limit: 500})
	if err != nil {
		return nil, err
	}
	return page.Items, nil
}

func buildAgentKubescapePosture(clusterID string, configurations, vulnerabilities []unstructured.Unstructured, limit int) domainresource.SecurityPosture {
	posture := domainresource.SecurityPosture{ClusterID: clusterID, Provider: "kubescape", Status: "available", GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano), Findings: []domainresource.SecurityFinding{}, Warnings: []string{}}
	for _, item := range configurations {
		severities, _, _ := unstructured.NestedMap(item.Object, "spec", "severities")
		addAgentFlatCounts(&posture.Counts, severities)
		controls, _, _ := unstructured.NestedMap(item.Object, "spec", "controls")
		for key, raw := range controls {
			control, _ := raw.(map[string]any)
			status, _, _ := unstructured.NestedString(control, "status", "status")
			if !strings.EqualFold(status, "failed") && !strings.EqualFold(status, "warning") {
				continue
			}
			severity, _, _ := unstructured.NestedString(control, "severity", "severity")
			info, _, _ := unstructured.NestedString(control, "status", "info")
			posture.Findings = append(posture.Findings, domainresource.SecurityFinding{ID: "configuration:" + key + ":" + item.GetNamespace() + ":" + item.GetName(), Category: "configuration", Severity: normalizeAgentSeverity(severity), Title: nonEmpty(info, "Kubescape configuration control failed"), Status: status, ControlID: key, Resource: agentKubescapeResource(clusterID, item)})
		}
	}
	for _, item := range vulnerabilities {
		severities, _, _ := unstructured.NestedMap(item.Object, "spec", "severities")
		highest, total := addAgentNestedCounts(&posture.Counts, severities)
		if total > 0 {
			posture.Findings = append(posture.Findings, domainresource.SecurityFinding{ID: "vulnerability:" + item.GetNamespace() + ":" + item.GetName(), Category: "vulnerability", Severity: highest, Title: fmt.Sprintf("%d container image vulnerabilities", total), Status: "detected", Resource: agentKubescapeResource(clusterID, item)})
		}
	}
	sort.SliceStable(posture.Findings, func(i, j int) bool { return posture.Findings[i].ID < posture.Findings[j].ID })
	if limit > 0 && len(posture.Findings) > limit {
		posture.Findings = posture.Findings[:limit]
		posture.Warnings = append(posture.Warnings, "Security findings were truncated to the requested limit.")
	}
	return posture
}

func agentKubescapeResource(clusterID string, item unstructured.Unstructured) *domainresource.ResourceRef {
	labels := item.GetLabels()
	kind, name := labels["kubescape.io/workload-kind"], labels["kubescape.io/workload-name"]
	if kind == "" || name == "" {
		kind, name = item.GetKind(), item.GetName()
	}
	namespace := labels["kubescape.io/workload-namespace"]
	if namespace == "" {
		namespace = item.GetNamespace()
	}
	apiVersion := labels["kubescape.io/workload-api-version"]
	if group := labels["kubescape.io/workload-api-group"]; group != "" {
		apiVersion = group + "/" + apiVersion
	}
	return &domainresource.ResourceRef{ClusterID: clusterID, APIVersion: nonEmpty(apiVersion, item.GetAPIVersion()), Kind: kind, Name: name, Namespace: namespace, ScopeMode: domainresource.ResourceScopeModeNamespace}
}

func addAgentFlatCounts(counts *domainresource.SecuritySeverityCounts, values map[string]any) {
	counts.Critical += agentInt(values["critical"])
	counts.High += agentInt(values["high"])
	counts.Medium += agentInt(values["medium"])
	counts.Low += agentInt(values["low"])
	counts.Unknown += agentInt(values["unknown"])
}

func addAgentNestedCounts(counts *domainresource.SecuritySeverityCounts, values map[string]any) (string, int64) {
	totals := map[string]int64{}
	for _, severity := range []string{"critical", "high", "medium", "low", "unknown"} {
		entry, _ := values[severity].(map[string]any)
		totals[severity] = agentInt(entry["all"])
	}
	counts.Critical += totals["critical"]
	counts.High += totals["high"]
	counts.Medium += totals["medium"]
	counts.Low += totals["low"]
	counts.Unknown += totals["unknown"]
	total := totals["critical"] + totals["high"] + totals["medium"] + totals["low"] + totals["unknown"]
	for _, severity := range []string{"critical", "high", "medium", "low", "unknown"} {
		if totals[severity] > 0 {
			return severity, total
		}
	}
	return "unknown", total
}

func agentInt(value any) int64 {
	switch typed := value.(type) {
	case int64:
		return typed
	case int:
		return int64(typed)
	case float64:
		return int64(typed)
	case json.Number:
		parsed, _ := typed.Int64()
		return parsed
	default:
		return 0
	}
}

func normalizeAgentSeverity(value string) string {
	switch strings.ToLower(value) {
	case "critical", "high", "medium", "low":
		return strings.ToLower(value)
	default:
		return "unknown"
	}
}

func nonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
