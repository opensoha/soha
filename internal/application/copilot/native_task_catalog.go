package copilot

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	appaccess "github.com/opensoha/soha/internal/application/access"
	appaigateway "github.com/opensoha/soha/internal/application/aigateway"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func appendNativeTaskBindings(bindings []domaincopilot.AgentToolBinding, tasks map[string]nativeToolDefinition) []domaincopilot.AgentToolBinding {
	if len(tasks) == 0 {
		return bindings
	}
	for _, name := range []string{"harness.discover_tasks", "harness.request_task"} {
		bindings = append(bindings, domaincopilot.AgentToolBinding{ID: name, ToolName: name, ToolKind: "internal_api", AdapterID: "harness", PermissionKey: appaccess.PermAIGatewayInvoke})
	}
	return bindings
}

func nativeTaskModelTools(run domaincopilot.AgentRun, enabled bool) []appaigateway.WorkbenchTool {
	snapshot, frozen, err := nativeCatalogFrom(run)
	if !enabled || err != nil || !frozen || len(snapshot.Tasks) == 0 || len(stringValue(run.Input["_sohaAuthorizationCeiling"])) != 64 || len(stringValue(run.Input["_sohaGatewayAuthorizationCeiling"])) != 64 {
		return nil
	}
	return []appaigateway.WorkbenchTool{
		{Name: "harness_discover_tasks", Description: "Read the task's frozen, currently authorized capability definitions before composing a governed plan. Results are bounded and paginated; discovery grants no authority.", Parameters: map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{"query": map[string]any{"type": "string", "maxLength": 256}, "offset": map[string]any{"type": "integer", "minimum": 0, "maximum": 512}}}},
		{Name: "harness_request_task", Description: "Submit and wait for an existing governed CapabilityTask. Supply a version-pinned CapabilityPlan: goal, steps [{id, call:{toolName, capabilityVersion, input, secretRefs?}, dependsOn?, bindings:[{stepId, outputPath, inputPath}]?, timeoutSeconds?}], verificationSteps and optional timeoutSeconds. Include declared preconditions and verification checks; verification must bind the actual write receipt. Identity, task scope and idempotency keys are server-owned. Writes require domain approval and fresh assessments; never claim success from submission alone.", Parameters: map[string]any{"type": "object", "required": []string{"plan"}, "additionalProperties": false, "properties": map[string]any{"plan": map[string]any{"type": "object"}}}},
	}
}

func (s *Service) discoverNativeTasks(ctx context.Context, principal domainidentity.Principal, run domaincopilot.AgentRun, raw json.RawMessage) (map[string]any, error) {
	var input struct {
		Query  string `json:"query"`
		Offset int    `json:"offset"`
	}
	if err := decodeNativeTaskArguments(raw, &input); err != nil || len(input.Query) > 256 || input.Offset < 0 || input.Offset > 512 {
		return nil, apperrors.ErrInvalidArgument
	}
	catalog, err := s.currentNativeTaskCatalog(ctx, principal, run)
	if err != nil {
		return nil, err
	}
	names := []string{}
	query := strings.ToLower(input.Query)
	for name, tool := range catalog {
		if nativeTaskCapability(tool) && strings.Contains(strings.ToLower(name+" "+tool.Description), query) {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	items := []domainaigateway.ToolCapability{}
	offset := min(input.Offset, len(names))
	for offset < len(names) && len(items) < 8 {
		next := append(slices.Clone(items), catalog[names[offset]])
		data, err := json.Marshal(next)
		if err != nil || len(data) > 16384 {
			if len(items) == 0 {
				return nil, fmt.Errorf("%w: capability schema exceeds Native discovery budget", apperrors.ErrUnsupportedOperation)
			}
			break
		}
		items = next
		offset++
	}
	return map[string]any{"items": items, "nextOffset": offset, "hasMore": offset < len(names)}, nil
}

func validateNativePlanChecks(plan domainaigateway.CapabilityPlan, tools map[string]domainaigateway.ToolCapability) error {
	steps := map[string]domainaigateway.CapabilityPlanStep{}
	for _, step := range plan.Steps {
		steps[step.ID] = step
	}
	for _, write := range plan.Steps {
		tool := tools[write.Call.ToolName]
		if nativeReadCapability(tool) {
			continue
		}
		for _, check := range tool.Execution.Checks {
			if !nativePlanContainsCheck(plan, steps, write, check) {
				return fmt.Errorf("%w: plan lacks the declared write precondition or receipt-bound verification", apperrors.ErrInvalidArgument)
			}
		}
	}
	return nil
}

func nativePlanContainsCheck(plan domainaigateway.CapabilityPlan, steps map[string]domainaigateway.CapabilityPlanStep, write domainaigateway.CapabilityPlanStep, check domainaigateway.CapabilityCheckReference) bool {
	for _, step := range plan.Steps {
		if step.Call.ToolName != check.ToolName || step.Call.CapabilityVersion != check.CapabilityVersion {
			continue
		}
		if (check.Purpose == "precondition" || check.Purpose == "availability") && nativePlanDependsOn(steps, write.ID, step.ID, map[string]bool{}) {
			return true
		}
		if check.Purpose == "verification" && slices.Contains(plan.VerificationSteps, step.ID) && nativePlanDependsOn(steps, step.ID, write.ID, map[string]bool{}) && slices.ContainsFunc(step.Bindings, func(binding domainaigateway.CapabilityInputBinding) bool { return binding.StepID == write.ID }) {
			return true
		}
	}
	return false
}

func nativePlanDependsOn(steps map[string]domainaigateway.CapabilityPlanStep, id, dependency string, visited map[string]bool) bool {
	if visited[id] {
		return false
	}
	visited[id] = true
	for _, parent := range steps[id].DependsOn {
		if parent == dependency || nativePlanDependsOn(steps, parent, dependency, visited) {
			return true
		}
	}
	return false
}
