package sohacli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
)

type tuiAPI struct{ client APIClient }

func (a tuiAPI) load(ctx context.Context, scope tuiScope, row *tuiRow) (tuiSnapshot, error) {
	if scope.section == "" {
		scope.section = tuiSections(scope.tab)[0]
	}
	var value any
	var err error
	if scope.logs && row != nil {
		value, err = a.logs(ctx, scope, *row)
		if err != nil {
			return tuiSnapshot{}, err
		}
		value, err = workbenchDisplayValue(a.client, value)
		if err != nil {
			return tuiSnapshot{}, err
		}
		return tuiSnapshot{rows: []tuiRow{*row}, detail: value, loaded: time.Now(), message: "Bounded log snapshot; r rereads. Detaching does not stop the workload."}, nil
	}
	switch scope.tab {
	case 0:
		value, err = a.kubernetes(ctx, scope, row)
	case 1:
		value, err = a.compute(ctx, scope, row)
	case 2:
		value, err = a.delivery(ctx, scope, row)
	case 3:
		value, err = a.ai(ctx, scope, row)
	case 4:
		switch scope.section {
		case "runs":
			value, err = a.ai(ctx, tuiScope{section: "runs"}, row)
		case "delivery":
			value, err = a.delivery(ctx, tuiScope{section: "tasks"}, row)
		default:
			if row != nil {
				value, err = a.client.GetComputeTask(ctx, ComputeTaskDomain(tuiString(row.value, "domain")), row.id)
			} else {
				value, err = a.client.ListComputeTasks(ctx, sohaapi.ListComputeTasksParams{Limit: 50, Cursor: sohaapi.ComputeCursor(scope.cursor)})
			}
		}
	case 5:
		if row != nil {
			value, err = a.client.GetApprovalTimeline(ctx, row.id, nil)
		} else {
			value, err = a.client.ListApprovalRequests(ctx, url.Values{"status": {"pending"}, "limit": {"50"}}, nil)
		}
	default:
		return tuiSnapshot{}, fmt.Errorf("unknown workbench")
	}
	if err != nil {
		return tuiSnapshot{}, err
	}
	value, err = workbenchDisplayValue(a.client, value)
	if err != nil {
		return tuiSnapshot{}, err
	}
	if row != nil {
		return tuiSnapshot{rows: []tuiRow{*row}, detail: value, loaded: time.Now()}, nil
	}
	return tuiRows(value)
}

func (a tuiAPI) read(ctx context.Context, path string, out any) (any, error) {
	err := a.client.doJSON(ctx, http.MethodGet, path, a.client.Token, nil, nil, out)
	return out, err
}

func (a tuiAPI) kubernetes(ctx context.Context, scope tuiScope, row *tuiRow) (any, error) {
	if scope.cluster == "" {
		return nil, fmt.Errorf("choose a cluster with --cluster-id; scope is never guessed")
	}
	base, err := kubernetesClusterPath(scope.cluster)
	if err != nil {
		return nil, err
	}
	if scope.section == "events" {
		return a.read(ctx, kubernetesQuery(base+"/events", scope.namespace), &sohaapi.KubernetesClusterEventListEnvelope{})
	}
	action, name, namespace := "list", "", scope.namespace
	if row != nil {
		action, name = "get", row.name
		if row.namespace != "" {
			namespace = row.namespace
		}
	}
	return a.client.ReadKubernetesResource(ctx, scope.cluster, namespace, scope.section, name, action)
}

func (a tuiAPI) compute(ctx context.Context, scope tuiScope, row *tuiRow) (any, error) {
	if scope.section == "vms" {
		if row != nil {
			return a.read(ctx, virtualizationPath+"/vms/"+url.PathEscape(row.id)+"/detail", &sohaapi.VirtualMachineDetailEnvelope{})
		}
		return a.read(ctx, virtualizationPath+"/vms?page="+strconv.Itoa(scope.page)+"&pageSize=50", &sohaapi.VirtualMachinePageEnvelope{})
	}
	// Docker configuration and runtime access tickets never enter the terminal UI.
	if row != nil {
		if scope.section == "services" {
			return row.value, nil
		}
		var out sohaapi.DockerResourceIdentityEnvelope
		return a.read(ctx, "/api/v1/docker/"+scope.section+"/"+url.PathEscape(row.id)+"/identity", &out)
	}
	path := "/api/v1/docker/" + scope.section + "?page=" + strconv.Itoa(scope.page) + "&pageSize=50"
	switch scope.section {
	case "hosts":
		return a.read(ctx, path, &struct {
			Data dockerInventoryPage[dockerHostInventory] `json:"data"`
		}{})
	case "projects":
		return a.read(ctx, path, &struct {
			Data dockerInventoryPage[dockerProjectInventory] `json:"data"`
		}{})
	case "services":
		return a.read(ctx, path, &struct {
			Data dockerInventoryPage[dockerServiceInventory] `json:"data"`
		}{})
	default:
		return nil, fmt.Errorf("unknown compute view")
	}
}

func (a tuiAPI) delivery(ctx context.Context, scope tuiScope, row *tuiRow) (any, error) {
	action, ids := "list", []string(nil)
	if row != nil {
		action, ids = "get", []string{row.id}
	}
	if scope.section == "tasks" {
		req, err := buildDeliveryWorkbenchRequest("tasks", action, ids, deliveryWorkbenchOptions{limit: 50})
		if err != nil {
			return nil, err
		}
		return a.read(ctx, req.path, req.out)
	}
	_, path, _, out, err := deliveryEndpoint(scope.section, action)
	if err != nil {
		return nil, err
	}
	if row != nil {
		path = strings.Replace(path, "{id}", url.PathEscape(row.id), 1)
	} else if scope.section == "batches" {
		path += "?limit=50"
	}
	return a.read(ctx, path, out)
}

func (a tuiAPI) ai(ctx context.Context, scope tuiScope, row *tuiRow) (any, error) {
	area, action, ids := "chat-sessions", "list", []string(nil)
	switch scope.section {
	case "runs":
		area = "runs"
	case "assistant-tasks":
		area = "assistant-tasks"
	case "artifacts":
		area = "assistant-artifacts"
	}
	if row != nil {
		if scope.section == "runs" {
			return a.read(ctx, aiRunEventsPath(url.PathEscape(row.id), 0), &sohaapi.AgentRunEventListEnvelope{})
		}
		if scope.section != "sessions" {
			return row.value, nil
		}
		area, action, ids = "chat-messages", "window", []string{row.id}
	}
	req, err := buildAIWorkbenchRequest(area, action, ids, aiWorkbenchOptions{limit: 40})
	if err != nil {
		return nil, err
	}
	return a.read(ctx, req.path, req.out)
}

func (a tuiAPI) act(ctx context.Context, scope tuiScope, row tuiRow, action string) (any, error) {
	value, err := a.mutate(ctx, scope, row, action)
	if err != nil {
		return nil, err
	}
	return workbenchDisplayValue(a.client, value)
}

func (a tuiAPI) mutate(ctx context.Context, scope tuiScope, row tuiRow, action string) (any, error) {
	if scope.section == "" {
		scope.section = tuiSections(scope.tab)[0]
	}
	if !kubernetesSegment(row.id) || !tuiActionAllowed(scope, row, action) {
		return nil, fmt.Errorf("action is unavailable for the reviewed target")
	}
	if scope.tab == 5 {
		return a.client.DecideApprovalRequest(ctx, row.id, action, "Reviewed in Soha TUI", nil)
	}
	if scope.section == "runs" {
		req, err := aiRunRequest("cancel", []string{row.id}, aiWorkbenchOptions{})
		if err != nil {
			return nil, err
		}
		err = a.client.doJSON(ctx, req.method, req.path, a.client.Token, nil, req.body, req.out)
		return req.out, err
	}
	if scope.tab == 2 || scope.section == "delivery" {
		req, err := buildDeliveryWorkbenchRequest("tasks", "cancel", []string{row.id}, deliveryWorkbenchOptions{reason: "Requested in Soha TUI"})
		if err != nil {
			return nil, err
		}
		err = executeDeliveryWorkbench(ctx, a.client, req)
		return req.out, err
	}
	domain := ComputeTaskDomain(tuiString(row.value, "domain"))
	input := ComputeTaskMutationRequest{Reason: "Requested in Soha TUI"}
	return a.client.CancelComputeTaskWithKey(ctx, domain, row.id, computeIdempotencyKey(string(domain), row.id, "cancel", input), input)
}

func (a tuiAPI) chat(ctx context.Context, session, key, content string, emit func(aiStreamOutput) error) error {
	if !kubernetesSegment(session) {
		return usageError{message: "one owned session ID is required"}
	}
	input := sohaapi.WorkbenchSendMessageStreamRequest{Content: content, RequestID: key}
	if err := prepareAIStreamInput(&input, key); err != nil {
		return err
	}
	return a.client.receiveAIChat(ctx, url.PathEscape(session), input, emit)
}

func tuiRows(value any) (tuiSnapshot, error) {
	var normalized any
	raw, err := json.Marshal(value)
	if err != nil {
		return tuiSnapshot{}, fmt.Errorf("invalid workbench snapshot")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&normalized) != nil {
		return tuiSnapshot{}, fmt.Errorf("invalid workbench snapshot")
	}
	var items []any
	total := int64(0)
	next := ""
	if object, ok := normalized.(map[string]any); ok {
		next = tuiString(object, "nextCursor")
		if data, exists := object["data"]; exists {
			normalized = data
		}
	}
	if object, ok := normalized.(map[string]any); ok {
		items, _ = object["items"].([]any)
		if n, ok := object["total"].(json.Number); ok {
			total, _ = n.Int64()
		}
		if cursor := tuiString(object, "nextCursor"); cursor != "" {
			next = cursor
		}
	} else {
		items, _ = normalized.([]any)
	}
	result := tuiSnapshot{loaded: time.Now(), total: total, nextCursor: next}
	if len(items) > 50 {
		items = items[:50]
		result.message = "Showing first 50 results; use native commands for full pagination."
	}
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			return tuiSnapshot{}, fmt.Errorf("invalid workbench item")
		}
		id := tuiString(object, "id", "name", "uid")
		if !kubernetesSegment(id) {
			return tuiSnapshot{}, fmt.Errorf("workbench item has no valid identity")
		}
		result.rows = append(result.rows, tuiRow{id: id, name: tuiString(object, "name", "title", "summary", "id"), status: tuiString(object, "normalizedStatus", "status", "phase", "activity"), namespace: tuiString(object, "namespace"), value: object})
	}
	return result, nil
}

func tuiString(object map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := object[key].(string); ok && value != "" {
			return value
		}
	}
	return ""
}

func decodeTUIEvent(event sohaapi.WorkbenchStreamEvent, out any) {
	raw, err := json.Marshal(event)
	if err == nil {
		_ = json.Unmarshal(raw, out)
	}
}
