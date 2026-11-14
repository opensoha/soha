package aigateway

import (
	"encoding/json"

	contractresource "github.com/opensoha/soha/contracts/resource"
	domainai "github.com/opensoha/soha/internal/domain/aigateway"
)

func podGatewayOutput(receipt contractresource.PodExecutionReceipt) contractresource.PodGatewayResult {
	result := contractresource.NewPodGatewayResult(receipt)
	if !receipt.OutputAvailable || receipt.Output == nil {
		return result
	}
	output := *receipt.Output
	if output.StdoutEncoding != "utf-8" || output.StderrEncoding != "utf-8" {
		result.OutputState = "binary_withheld"
		return result
	}
	rule := gatewayRedactionRule{SecretTypes: []string{"default"}, Replacement: "[REDACTED]"}
	output.Stdout = gatewayRedactSensitiveTextWithRule(output.Stdout, rule)
	output.Stderr = gatewayRedactSensitiveTextWithRule(output.Stderr, rule)
	result.OutputState = "text"
	if output.Stdout != receipt.Output.Stdout || output.Stderr != receipt.Output.Stderr {
		result.OutputState = "redacted"
	}
	result.Output = &output
	return result
}

func (*podExecutionCapabilityProvider) TaskReference(tool domainai.ToolCapability, output, visible any) *domainai.CapabilityTaskRef {
	if tool.Execution == nil || tool.Execution.TaskKind != "k8s.pod_execution" {
		return nil
	}
	var original, shown contractresource.PodGatewayResult
	raw, err := json.Marshal(output)
	if err != nil || json.Unmarshal(raw, &original) != nil {
		return nil
	}
	raw, err = json.Marshal(visible)
	if err != nil || json.Unmarshal(raw, &shown) != nil {
		return nil
	}
	item := original.Receipt
	if !sameVisiblePodExecution(item, shown.Receipt) {
		return nil
	}
	outcome, terminal := "unknown", true
	switch item.Status {
	case "succeeded":
		outcome = "succeeded"
	case "failed", "timed_out", "canceled":
		outcome = "failed"
	case "running":
		terminal = false
	case "unknown", "transport_lost":
	default:
		return nil
	}
	return &domainai.CapabilityTaskRef{Kind: "k8s.pod_execution", ID: item.ID, Status: item.Status, Outcome: outcome, Terminal: terminal, StatusCall: domainai.CapabilityCall{ToolName: "k8s.pods.exec.receipt", CapabilityVersion: "1", Input: map[string]any{"clusterId": item.ClusterID, "namespace": item.Namespace, "podName": item.PodName, "executionId": item.ID}}}
}

func sameVisiblePodExecution(original, shown contractresource.PodGatewayReceipt) bool {
	return original.ID != "" && original.ClusterID != "" && original.Namespace != "" && original.PodName != "" && shown.ID == original.ID && shown.Status == original.Status && shown.ClusterID == original.ClusterID && shown.Namespace == original.Namespace && shown.PodName == original.PodName
}
