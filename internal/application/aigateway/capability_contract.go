package aigateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domaindocker "github.com/opensoha/soha/internal/domain/docker"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

func validateCapabilityVersion(tool domainaigateway.ToolCapability, requested string) error {
	if requested != "" && requested != tool.Version {
		return fmt.Errorf("%w: capability version changed or is unavailable; rediscover %s", apperrors.ErrConflict, tool.Name)
	}
	return nil
}

func validateApprovedCapability(tool domainaigateway.ToolCapability, request domainaigateway.ApprovalRequest) error {
	version, _ := request.RelatedIDs["capabilityVersion"].(string)
	if version != tool.Version {
		return fmt.Errorf("%w: approved capability version changed; submit a new approval request", apperrors.ErrConflict)
	}
	return validateCapabilityInput(tool, request.ToolInput)
}

func validateCapabilityInput(tool domainaigateway.ToolCapability, input map[string]any) error {
	// Legacy tools keep their existing domain validation until they opt into a contract.
	if tool.Version == "" {
		return nil
	}
	if tool.Execution != nil && tool.Execution.IdempotencyKeyField != "" {
		key, _ := input[tool.Execution.IdempotencyKeyField].(string)
		if strings.TrimSpace(key) == "" {
			return fmt.Errorf("%w: %s is required", apperrors.ErrInvalidArgument, tool.Execution.IdempotencyKeyField)
		}
	}
	if input == nil {
		input = map[string]any{}
	}
	compiled, err := compileCapabilityInputSchema(tool.InputSchema)
	if err != nil {
		return err
	}
	data, err := capabilityJSON(input)
	if err != nil || compiled.Validate(data) != nil {
		// Schema errors can contain rejected secret values; do not echo them to logs or callers.
		return fmt.Errorf("%w: input does not match capability %s version %s", apperrors.ErrInvalidArgument, tool.Name, tool.Version)
	}
	return nil
}

func compileCapabilityInputSchema(schema map[string]any) (*jsonschema.Schema, error) {
	if len(schema) == 0 {
		return nil, fmt.Errorf("%w: versioned capability has no input schema", apperrors.ErrInvalidArgument)
	}
	document, err := capabilityJSON(schema)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid capability input schema", apperrors.ErrInvalidArgument)
	}
	const location = "https://schemas.opensoha.invalid/capability-input"
	compiler := jsonschema.NewCompiler()
	compiler.UseLoader(capabilitySchemaLoader{})
	if err := compiler.AddResource(location, document); err != nil {
		return nil, fmt.Errorf("%w: invalid capability input schema", apperrors.ErrInvalidArgument)
	}
	compiled, err := compiler.Compile(location)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid or externally referenced capability input schema", apperrors.ErrInvalidArgument)
	}
	return compiled, nil
}

type capabilitySchemaLoader struct{}

func (capabilitySchemaLoader) Load(string) (any, error) {
	return nil, fmt.Errorf("capability schemas must be self-contained")
}

func capabilityJSON(value any) (any, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return jsonschema.UnmarshalJSON(bytes.NewReader(data))
}

func capabilityTaskRef(tool domainaigateway.ToolCapability, output any) *domainaigateway.CapabilityTaskRef {
	if tool.Execution == nil || tool.Execution.TaskKind != "docker.operation" {
		return nil
	}
	operation, ok := output.(domaindocker.Operation)
	if !ok {
		raw, err := json.Marshal(output)
		if err != nil || json.Unmarshal(raw, &operation) != nil {
			return nil
		}
	}
	if operation.ID == "" || operation.Status == "" {
		return nil
	}
	state := domaindocker.BuildOperationState(operation, time.Now().UTC())
	outcome := "unknown"
	if state.Terminal {
		switch operation.Status {
		case "completed":
			outcome = "succeeded"
		case "canceled":
			if acknowledged, _ := operation.Result["cancellationAcknowledged"].(bool); acknowledged {
				outcome = "canceled"
			}
		default:
			outcome = "failed"
		}
	}
	return &domainaigateway.CapabilityTaskRef{
		CancelCall: &domainaigateway.CapabilityCall{ToolName: "docker.operations.cancel", CapabilityVersion: "1", Input: map[string]any{"operationId": operation.ID, "idempotencyKey": "cancel-operation:" + operation.ID}},
		Outcome:    outcome,
		Kind:       "docker.operation", ID: operation.ID, Status: operation.Status, Terminal: state.Terminal,
		StatusCall: domainaigateway.CapabilityCall{
			ToolName: "docker.operations.get", CapabilityVersion: "1",
			Input: map[string]any{"operationId": operation.ID},
		},
	}
}

func (BuiltinCapabilityProvider) TaskReference(tool domainaigateway.ToolCapability, output, visibleOutput any) *domainaigateway.CapabilityTaskRef {
	return visibleCapabilityTaskRef(capabilityTaskRef(tool, output), visibleOutput)
}

func visibleCapabilityTaskRef(task *domainaigateway.CapabilityTaskRef, output any) *domainaigateway.CapabilityTaskRef {
	if task == nil {
		return nil
	}
	// A task reference must not restore identifiers or status removed by output policy.
	data, err := capabilityJSON(output)
	if err != nil {
		return nil
	}
	object, ok := data.(map[string]any)
	if !ok || object["id"] != task.ID || object["status"] != task.Status {
		return nil
	}
	return task
}
