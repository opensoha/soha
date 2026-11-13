package sohacli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

type capabilitySearchInput struct {
	Query        string `json:"query,omitempty" jsonschema:"Words describing the goal or expected effect"`
	ToolDomain   string `json:"toolDomain,omitempty" jsonschema:"Owning domain such as docker or delivery"`
	Action       string `json:"action,omitempty"`
	ResourceKind string `json:"resourceKind,omitempty"`
	Limit        int    `json:"limit,omitempty"`
	Cursor       string `json:"cursor,omitempty"`
}

type capabilityTaskIDInput struct {
	TaskID      string `json:"taskId" jsonschema:"Durable Soha capability task ID"`
	PlanVersion int    `json:"planVersion,omitempty" jsonschema:"For get only: archived plan revision, or zero for current"`
}

func (s mcpServer) addCapabilityTaskTools(server *mcpsdk.Server) {
	addCapabilityMCPTool(server, "soha.capabilities.search", "Discover authorized capabilities by goal, owning domain and resource kind. Rediscover when a catalog cursor expires.", true, func(ctx context.Context, input capabilitySearchInput) (any, error) {
		if input.Limit < 0 || input.Limit > 100 {
			return nil, fmt.Errorf("limit must be between 0 and 100")
		}
		query := url.Values{}
		for key, value := range map[string]string{"query": input.Query, "toolDomain": input.ToolDomain, "action": input.Action, "resourceKind": input.ResourceKind, "cursor": input.Cursor} {
			if value != "" {
				query.Set(key, value)
			}
		}
		limit := input.Limit
		if limit == 0 {
			limit = 50
		}
		query.Set("limit", strconv.Itoa(limit))
		return s.client.SearchCapabilities(ctx, s.headers, query)
	})
	for _, action := range []string{"validate", "create"} {
		name := "soha.tasks." + action
		if action == "validate" {
			name = "soha.plans.validate"
		}
		addCapabilityMCPTool(server, name, "Validate or submit a version-pinned plan. A stable idempotency key binds retries to one goal. Each step retains server authorization and approval; verification steps need domain evidence.", action == "validate", func(ctx context.Context, input sohaapi.CapabilityTaskInput) (any, error) {
			method, path, _, result, err := capabilityTaskEndpoint(action)
			if err != nil {
				return nil, err
			}
			err = s.client.doJSON(ctx, method, path, s.client.Token, s.headers, input, result)
			return result, err
		})
	}
	for _, action := range []string{"get", "cancel"} {
		addCapabilityMCPTool(server, "soha.tasks."+action, "Read or request cancellation of an existing Soha goal. Resume observation using this ID in any authorized client. Cancellation preserves completed effects; unknown child state is not success.", action == "get", func(ctx context.Context, input capabilityTaskIDInput) (any, error) {
			if !validDeliveryDocumentID(input.TaskID) {
				return nil, fmt.Errorf("taskId is required")
			}
			method, path, _, result, err := capabilityTaskEndpoint(action)
			if err != nil {
				return nil, err
			}
			path = strings.Replace(path, "{id}", url.PathEscape(input.TaskID), 1)
			if input.PlanVersion < 0 || (input.PlanVersion != 0 && action != "get") {
				return nil, fmt.Errorf("planVersion is only supported for get")
			}
			if input.PlanVersion > 0 {
				path += "?planVersion=" + strconv.Itoa(input.PlanVersion)
			}
			err = s.client.doJSON(ctx, method, path, s.client.Token, s.headers, nil, result)
			return result, err
		})
	}
	addCapabilityMCPTool(server, "soha.tasks.resume", "Resume a paused or terminal goal with a reviewed plan and expectedVersion. Preserve unresolved steps; use fresh verification steps. Prior plans and effects remain inspectable by planVersion.", false, func(ctx context.Context, input struct {
		TaskID   string                              `json:"taskId"`
		Revision sohaapi.CapabilityTaskRevisionInput `json:"revision"`
	}) (any, error) {
		if !validDeliveryDocumentID(input.TaskID) {
			return nil, fmt.Errorf("taskId is required")
		}
		method, path, _, result, err := capabilityTaskEndpoint("resume")
		if err != nil {
			return nil, err
		}
		path = strings.Replace(path, "{id}", url.PathEscape(input.TaskID), 1)
		err = s.client.doJSON(ctx, method, path, s.client.Token, s.headers, input.Revision, result)
		return result, err
	})
	addCapabilityMCPTool(server, "soha.tasks.list", "List this identity's durable goals under current authorization.", true, func(ctx context.Context, input struct {
		Limit int `json:"limit,omitempty"`
	}) (any, error) {
		limit := input.Limit
		if limit == 0 {
			limit = 20
		}
		if limit < 1 || limit > 100 {
			return nil, fmt.Errorf("limit must be between 1 and 100")
		}
		method, path, _, result, err := capabilityTaskEndpoint("list")
		if err != nil {
			return nil, err
		}
		err = s.client.doJSON(ctx, method, path+"?limit="+strconv.Itoa(limit), s.client.Token, s.headers, nil, result)
		return result, err
	})
}

func addCapabilityMCPTool[Input any](server *mcpsdk.Server, name, description string, readOnly bool, execute func(context.Context, Input) (any, error)) {
	destructive := !readOnly
	mcpsdk.AddTool(server, &mcpsdk.Tool{Name: name, Description: description, Annotations: &mcpsdk.ToolAnnotations{ReadOnlyHint: readOnly, DestructiveHint: &destructive, IdempotentHint: true}}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, input Input) (*mcpsdk.CallToolResult, any, error) {
		output, err := execute(ctx, input)
		if err != nil {
			return mcpTextResult(redactSensitiveText(err.Error()), true), nil, nil
		}
		visible := sanitizeCLIJSONValue(output)
		raw, err := json.Marshal(visible)
		if err != nil {
			return nil, nil, mcpBackendError(err)
		}
		return mcpTextResult(string(raw), false), visible, nil
	})
}
