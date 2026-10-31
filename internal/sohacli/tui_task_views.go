package sohacli

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
)

func (a tuiAPI) logs(ctx context.Context, scope tuiScope, row tuiRow) (any, error) {
	if scope.tab == 0 && scope.section != "events" {
		options := logQueryOptions{namespace: row.namespace, tail: 100, limit: 100, sourceMode: "auto", direction: "backward"}
		if options.namespace == "" {
			options.namespace = scope.namespace
		}
		if scope.section == "" || scope.section == "pods" {
			options.podNames = row.name
		} else {
			endpoint, err := kubernetesEndpoint(scope.section)
			if err != nil {
				return nil, err
			}
			options.workloadKind, options.workloadName = endpoint.kind, row.name
		}
		query, err := buildLogQuery(options)
		if err != nil {
			return nil, err
		}
		return a.client.QueryClusterLogs(ctx, scope.cluster, query)
	}
	if scope.tab == 1 && scope.section == "projects" {
		query, err := buildLogQuery(logQueryOptions{tail: 100, limit: 100, sourceMode: "auto", direction: "backward"})
		if err != nil {
			return nil, err
		}
		return a.client.QueryDockerProjectLogs(ctx, row.id, query)
	}
	if scope.tab == 2 && scope.section == "tasks" || scope.tab == 4 && scope.section == "delivery" {
		req, err := buildDeliveryWorkbenchRequest("tasks", "logs", []string{row.id}, deliveryWorkbenchOptions{limit: 100})
		if err != nil {
			return nil, err
		}
		return a.read(ctx, req.path, req.out)
	}
	if scope.tab == 4 && (scope.section == "" || scope.section == "compute") {
		return a.client.ListComputeTaskLogs(ctx, ComputeTaskDomain(tuiString(row.value, "domain")), row.id)
	}
	return nil, fmt.Errorf("this object has no log reader; run events use f to follow the original cursor")
}

func (m *tuiModel) pollAfterCancel() tea.Cmd {
	if m.watch.scope.section == "runs" {
		id := m.watch.row.id
		m.watch = nil
		return m.startFollow(id, m.sessionID)
	}
	if tuiResultTerminal(m.snapshot.detail) {
		m.watch = nil
		m.message = "Authoritative task status is terminal; inspect the result before assuming business success."
		return nil
	}
	target, epoch := *m.watch, m.epoch
	m.message = "Cancellation requested; waiting for authoritative terminal state. q only detaches."
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return tuiPoll{epoch: epoch, target: target} })
}

func tuiResultTerminal(value any) bool {
	object, ok := sanitizeCLIJSONValue(value).(map[string]any)
	if !ok {
		return false
	}
	if data, ok := object["data"].(map[string]any); ok {
		object = data
	}
	return tuiTerminalStatus(tuiString(object, "normalizedStatus", "status"))
}

func tuiTerminalStatus(status string) bool {
	switch status {
	case "succeeded", "completed", "failed", "canceled", "cancelled", "expired", "callback_timeout":
		return true
	default:
		return false
	}
}
