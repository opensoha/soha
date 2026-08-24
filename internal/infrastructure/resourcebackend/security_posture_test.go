package resourcebackend

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestBuildKubescapePostureAggregatesConfigurationAndVulnerabilitySummaries(t *testing.T) {
	configuration := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "spdx.softwarecomposition.kubescape.io/v1beta1", "kind": "WorkloadConfigurationScanSummary",
		"metadata": map[string]any{"name": "api", "namespace": "team-a", "labels": map[string]any{"kubescape.io/workload-kind": "Deployment", "kubescape.io/workload-name": "api", "kubescape.io/workload-api-version": "v1", "kubescape.io/workload-api-group": "apps"}},
		"spec": map[string]any{
			"severities": map[string]any{"critical": int64(1), "high": int64(2), "medium": int64(3), "low": int64(4), "unknown": int64(0)},
			"controls":   map[string]any{"C-001": map[string]any{"controlID": "C-001", "severity": map[string]any{"severity": "High"}, "status": map[string]any{"status": "failed", "info": "Privileged container"}}},
		},
	}}
	vulnerability := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "spdx.softwarecomposition.kubescape.io/v1beta1", "kind": "VulnerabilityManifestSummary",
		"metadata": map[string]any{"name": "api-container", "namespace": "team-a", "labels": map[string]any{"kubescape.io/workload-kind": "Deployment", "kubescape.io/workload-name": "api", "kubescape.io/workload-api-version": "v1", "kubescape.io/workload-api-group": "apps"}},
		"spec":     map[string]any{"severities": map[string]any{"critical": map[string]any{"all": int64(2)}, "high": map[string]any{"all": int64(1)}, "medium": map[string]any{"all": int64(0)}, "low": map[string]any{"all": int64(0)}, "unknown": map[string]any{"all": int64(0)}}},
	}}

	posture := buildKubescapePosture("cluster-a", "team-a", []unstructured.Unstructured{configuration}, []unstructured.Unstructured{vulnerability}, 100)
	if posture.Status != "available" || posture.Counts.Critical != 3 || posture.Counts.High != 3 {
		t.Fatalf("posture = %#v", posture)
	}
	if len(posture.Findings) != 2 || posture.Findings[0].Resource == nil || posture.Findings[0].Resource.Name != "api" {
		t.Fatalf("findings = %#v", posture.Findings)
	}
}
