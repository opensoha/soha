package delivery

import (
	"encoding/json"
	dm "github.com/opensoha/soha/internal/domain/manifest"
	"strings"
)

func projectConfigurationTask(task ExecutionTask) ExecutionTask {
	if !SensitiveExecutionTask(task) {
		return task
	}
	summary := make(map[string]any)
	for _, key := range []string{"action", "packageId", "bindingId", "deploymentId", "rootResource", "generation", "revision", "clusterId", "namespace"} {
		if value, present := task.Payload[key]; present {
			summary[key] = value
		}
	}
	task.Payload = summary
	result := make(map[string]any)
	for _, key := range []string{"action", "deploymentId", "generation", "stale", "evidenceRefs"} {
		if value, present := task.Result[key]; present {
			result[key] = value
		}
	}
	data, _ := json.Marshal(task.Result)
	var typed dm.TaskResult
	if json.Unmarshal(data, &typed) == nil {
		result["diagnostics"] = configurationDiagnosticSummary(typed.Diagnostics)
		if typed.Preflight != nil {
			typed.Preflight.RenderedDigest = ""
			typed.Preflight.Diagnostics = configurationDiagnosticSummary(typed.Preflight.Diagnostics)
			result["preflight"] = typed.Preflight
		}
		for i := range typed.Inventory {
			typed.Inventory[i].DesiredObjectDigest = ""
			typed.Inventory[i].ObservedObjectDigest = ""
		}
		result["inventory"] = typed.Inventory
	}
	if task.Result["error"] != nil {
		result["error"] = "manifest execution failed; configuration diagnostics require sensitive data access"
	}
	task.Result = result
	task.Artifacts = nil
	return task
}

func configurationDiagnosticSummary(items []dm.Diagnostic) []dm.Diagnostic {
	result := make([]dm.Diagnostic, len(items))
	for i, item := range items {
		result[i] = dm.Diagnostic{Stage: item.Stage, Severity: item.Severity, Code: item.Code, Message: "configuration diagnostics require sensitive data access", APIVersion: item.APIVersion, Kind: item.Kind, Namespace: item.Namespace, Name: item.Name}
	}
	return result
}

func SensitiveExecutionTask(task ExecutionTask) bool {
	if task.Payload["rootResource"] != nil {
		return true
	}
	if !strings.HasPrefix(task.TaskKind, "manifest_") {
		return false
	}
	if dm.SensitiveTaskPayload(task.Payload) {
		return true
	}
	data, err := json.Marshal(task.Result)
	var result dm.TaskResult
	if err != nil || json.Unmarshal(data, &result) != nil {
		return true
	}
	return dm.SensitiveFiles(result.SyncedFiles) || dm.SensitiveFiles(result.AdoptedFiles)
}
