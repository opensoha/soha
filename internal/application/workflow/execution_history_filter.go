package workflow

import (
	"slices"
	"strings"

	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
)

func executionHistoryMatches(entry domainworkflow.ExecutionHistoryEntry, f domainworkflow.ExecutionHistoryFilter) bool {
	status := ""
	values := []string{entry.ID}
	switch {
	case entry.Batch != nil:
		batch := entry.Batch
		status = batch.Status
		values = append(values, batch.Definition.Name, batch.CreatedBy, batch.StopSummary)
		for _, snapshot := range batch.Targets {
			values = append(values, snapshot.ApplicationName, snapshot.ServiceName, snapshot.EnvironmentName, snapshot.Target.ApplicationID, snapshot.Target.ServiceID)
		}
		for _, node := range batch.Nodes {
			values = append(values, node.BuildRecordID, node.ExecutionTaskID, node.Summary)
		}
	case entry.Application != nil:
		run := entry.Application
		status = run.Status
		values = append(values, run.WorkflowName, run.ApplicationID, run.Namespace, run.DeploymentName)
		values = append(values, executionHistoryMetadataText(run.Metadata)...)
	case entry.Build != nil:
		status = entry.Build.Status
		values = append(values, entry.Build.ApplicationID, entry.Build.SourceSystem)
		values = append(values, executionHistoryMetadataText(entry.Build.Metadata)...)
	default:
		return false
	}
	return executionHistoryStatusMatches(status, f.Status) && strings.Contains(strings.ToLower(strings.Join(values, " ")), strings.ToLower(f.Search))
}

func executionHistoryMetadataText(metadata map[string]any) []string {
	values := []string{}
	for _, value := range metadata {
		if text, ok := value.(string); ok {
			values = append(values, text)
		}
	}
	return values
}

func executionHistoryStatusMatches(status, filter string) bool {
	status = strings.ToLower(strings.TrimSpace(status))
	statuses := map[string][]string{
		"running":   {"pending", "queued", "running", "waiting_execution", "canceling"},
		"approval":  {"waiting_approval", "pending_approval", "awaiting_approval"},
		"succeeded": {"completed", "succeeded", "success", "ready"},
		"failed":    {"failed", "error", "rejected", "partially_completed", "expired", "timeout", "timed_out", "callback_timeout"},
		"canceled":  {"canceled", "cancelled"},
	}
	allowed, filtered := statuses[filter]
	return !filtered || slices.Contains(allowed, status)
}
