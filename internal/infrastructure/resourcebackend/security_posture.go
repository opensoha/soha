package resourcebackend

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func buildKubescapePosture(clusterID, namespace string, configurations, vulnerabilities []unstructured.Unstructured, limit int) domainresource.SecurityPosture {
	posture := domainresource.SecurityPosture{
		ClusterID: clusterID, Provider: "kubescape", Status: "available", GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Findings: []domainresource.SecurityFinding{}, Warnings: []string{},
	}
	if len(configurations) == 0 && len(vulnerabilities) == 0 {
		posture.Status = "unsupported"
		posture.Message = "Kubescape summary resources are not installed or have not produced results."
		return posture
	}
	for _, item := range configurations {
		addConfigurationSummary(&posture, clusterID, item)
	}
	for _, item := range vulnerabilities {
		addVulnerabilitySummary(&posture, clusterID, item)
	}
	sort.SliceStable(posture.Findings, func(i, j int) bool {
		if securitySeverityRank(posture.Findings[i].Severity) != securitySeverityRank(posture.Findings[j].Severity) {
			return securitySeverityRank(posture.Findings[i].Severity) > securitySeverityRank(posture.Findings[j].Severity)
		}
		return posture.Findings[i].ID < posture.Findings[j].ID
	})
	if limit > 0 && len(posture.Findings) > limit {
		posture.Findings = posture.Findings[:limit]
		posture.Warnings = append(posture.Warnings, "Security findings were truncated to the requested limit.")
	}
	return posture
}

func addConfigurationSummary(posture *domainresource.SecurityPosture, clusterID string, item unstructured.Unstructured) {
	severities, _, _ := unstructured.NestedMap(item.Object, "spec", "severities")
	addFlatSeverityCounts(&posture.Counts, severities)
	controls, _, _ := unstructured.NestedMap(item.Object, "spec", "controls")
	for key, raw := range controls {
		control, _ := raw.(map[string]any)
		status, _, _ := unstructured.NestedString(control, "status", "status")
		if !strings.EqualFold(status, "failed") && !strings.EqualFold(status, "warning") {
			continue
		}
		controlID, _, _ := unstructured.NestedString(control, "controlID")
		if controlID == "" {
			controlID = key
		}
		severity, _, _ := unstructured.NestedString(control, "severity", "severity")
		info, _, _ := unstructured.NestedString(control, "status", "info")
		if info == "" {
			info = "Kubescape configuration control failed"
		}
		resource := kubescapeResourceRef(clusterID, item)
		posture.Findings = append(posture.Findings, domainresource.SecurityFinding{
			ID: "configuration:" + controlID + ":" + item.GetNamespace() + ":" + item.GetName(), Category: "configuration",
			Severity: normalizeSecuritySeverity(severity), Title: info, Status: status, ControlID: controlID, Resource: resource,
		})
	}
}

func addVulnerabilitySummary(posture *domainresource.SecurityPosture, clusterID string, item unstructured.Unstructured) {
	severities, _, _ := unstructured.NestedMap(item.Object, "spec", "severities")
	highest, total := addNestedSeverityCounts(&posture.Counts, severities)
	if total == 0 {
		return
	}
	posture.Findings = append(posture.Findings, domainresource.SecurityFinding{
		ID: "vulnerability:" + item.GetNamespace() + ":" + item.GetName(), Category: "vulnerability", Severity: highest,
		Title: fmt.Sprintf("%d container image vulnerabilities", total), Status: "detected", Resource: kubescapeResourceRef(clusterID, item),
	})
}

func kubescapeResourceRef(clusterID string, item unstructured.Unstructured) *domainresource.ResourceRef {
	labels := item.GetLabels()
	kind := labels["kubescape.io/workload-kind"]
	name := labels["kubescape.io/workload-name"]
	namespace := labels["kubescape.io/workload-namespace"]
	if namespace == "" {
		namespace = item.GetNamespace()
	}
	if kind == "" || name == "" {
		kind, name = item.GetKind(), item.GetName()
	}
	apiVersion := labels["kubescape.io/workload-api-version"]
	if group := labels["kubescape.io/workload-api-group"]; group != "" {
		apiVersion = group + "/" + apiVersion
	}
	if apiVersion == "" {
		apiVersion = item.GetAPIVersion()
	}
	return &domainresource.ResourceRef{ClusterID: clusterID, APIVersion: apiVersion, Kind: kind, Name: name, Namespace: namespace, ScopeMode: domainresource.ResourceScopeModeNamespace}
}

func addFlatSeverityCounts(counts *domainresource.SecuritySeverityCounts, values map[string]any) {
	counts.Critical += nestedInt(values["critical"])
	counts.High += nestedInt(values["high"])
	counts.Medium += nestedInt(values["medium"])
	counts.Low += nestedInt(values["low"])
	counts.Unknown += nestedInt(values["unknown"])
}

func addNestedSeverityCounts(counts *domainresource.SecuritySeverityCounts, values map[string]any) (string, int64) {
	totals := map[string]int64{}
	for _, severity := range []string{"critical", "high", "medium", "low", "unknown"} {
		entry, _ := values[severity].(map[string]any)
		totals[severity] = nestedInt(entry["all"])
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

func nestedInt(value any) int64 {
	switch typed := value.(type) {
	case int64:
		return typed
	case int32:
		return int64(typed)
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

func normalizeSecuritySeverity(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "critical", "high", "medium", "low":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "unknown"
	}
}

func securitySeverityRank(value string) int {
	switch value {
	case "critical":
		return 5
	case "high":
		return 4
	case "medium":
		return 3
	case "low":
		return 2
	default:
		return 1
	}
}
