package aigateway

import (
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	contractresource "github.com/opensoha/soha-contracts/resource"
	domainai "github.com/opensoha/soha/internal/domain/aigateway"
)

func podExecutionAssessment(receipt contractresource.PodExecutionReceipt) domainai.CapabilityAssessment {
	verdict, summary := "inconclusive", "Original process completion is unconfirmed"
	incomplete := true
	if receipt.FinishedAt != nil && !receipt.FinishedAt.IsZero() && !receipt.FinishedAt.After(time.Now().Add(30*time.Second)) {
		switch receipt.Status {
		case "succeeded":
			if receipt.OutputAvailable && receipt.Output != nil && receipt.Output.ExitCode != nil && *receipt.Output.ExitCode == 0 && receipt.Output.UIDVerifiedBeforeStart && receipt.Output.Status == "succeeded" {
				verdict, summary, incomplete = "satisfied", "Original process finished with exit code zero", false
			}
		case "failed", "timed_out", "canceled":
			verdict, summary, incomplete = "unsatisfied", "Original process did not complete successfully", false
		}
	}
	observed := receipt.CreatedAt
	if receipt.FinishedAt != nil {
		observed = *receipt.FinishedAt
	}
	if observed.IsZero() {
		observed = time.Now().UTC()
		incomplete = true
		verdict = "inconclusive"
	}
	return domainai.CapabilityAssessment{Verdict: sohaapi.CapabilityAssessmentVerdict(verdict), Summary: summary, Evidence: []domainai.CapabilityEvidence{{Kind: "k8s.pod_execution", Source: "k8s.pod_execution:" + receipt.ID, ObservedAt: observed, Incomplete: incomplete, Summary: summary, Reference: &sohaapi.CapabilityCall{ToolName: "k8s.pods.exec.receipt", CapabilityVersion: "1", Input: map[string]any{"clusterId": receipt.ClusterID, "namespace": receipt.Namespace, "podName": receipt.PodName, "executionId": receipt.ID}}, Resource: &domainai.CapabilityResourceRef{Kind: "k8s.pod_execution", ID: receipt.ID}}}}
}
