package aigateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) ValidateCapabilityPlan(ctx context.Context, principal domainidentity.Principal, input domainaigateway.CapabilityTaskInput) (domainaigateway.CapabilityPlanValidation, error) {
	result := domainaigateway.CapabilityPlanValidation{Issues: []domainaigateway.CapabilityPlanIssue{}}
	schema, err := capabilityTaskInputSchema()
	if err != nil {
		return result, fmt.Errorf("%w: capability plan contract unavailable", apperrors.ErrInvalidArgument)
	}
	data, err := capabilityJSON(input)
	if err != nil || schema.Validate(data) != nil {
		result.Issues = append(result.Issues, planIssue("", "invalid_shape", "Plan does not match the public capability contract"))
		return result, nil
	}
	manifest, err := s.Capabilities(ctx, principal, domainaigateway.ManifestRequest{AIClientID: input.AIClientID, SkillID: input.SkillID})
	if err != nil {
		return result, err
	}
	if summary := gatewayRedactionAuditSummaryForValue(input.Plan.Goal, gatewayRedactionRule{SecretTypes: []string{"default"}}, "goal"); !summary.empty() {
		result.Issues = append(result.Issues, planIssue("", "secret_reference_required", "The goal description must not contain secrets"))
	}
	tools := map[string]domainaigateway.ToolCapability{}
	for _, tool := range manifest.Tools {
		tools[tool.Name] = tool
	}
	steps := map[string]domainaigateway.CapabilityPlanStep{}
	for _, step := range input.Plan.Steps {
		if _, duplicate := steps[step.ID]; duplicate {
			result.Issues = append(result.Issues, planIssue(step.ID, "duplicate_step", "Step IDs must be unique"))
		}
		steps[step.ID] = step
		result.Issues = append(result.Issues, validatePlannedCapability(step, tools)...)
	}
	for _, step := range input.Plan.Steps {
		result.Issues = append(result.Issues, validatePlannedBindings(step, steps, tools)...)
	}
	if capabilityPlanHasCycle(steps) {
		result.Issues = append(result.Issues, planIssue("", "dependency_cycle", "Plan dependencies must be acyclic"))
	}
	for _, id := range input.Plan.VerificationSteps {
		step, found := steps[id]
		if !found || !tools[step.Call.ToolName].ProducesAssessment {
			result.Issues = append(result.Issues, planIssue(id, "missing_assessment", "A verification step must produce a domain assessment"))
		}
	}
	result.Valid = len(result.Issues) == 0
	raw, err := json.Marshal(input)
	if err != nil {
		return result, err
	}
	sum := sha256.Sum256(raw)
	result.Digest = "sha256:" + hex.EncodeToString(sum[:])
	return result, nil
}

func planIssue(step, code, message string) domainaigateway.CapabilityPlanIssue {
	return domainaigateway.CapabilityPlanIssue{StepID: step, Code: code, Message: message}
}

func validatePlannedCapability(step domainaigateway.CapabilityPlanStep, tools map[string]domainaigateway.ToolCapability) []domainaigateway.CapabilityPlanIssue {
	issue := func(code, message string) []domainaigateway.CapabilityPlanIssue {
		return []domainaigateway.CapabilityPlanIssue{planIssue(step.ID, code, message)}
	}
	tool, found := tools[step.Call.ToolName]
	if !found {
		return issue("capability_unavailable", "Capability is unavailable to this identity and client")
	}
	if step.Call.CapabilityVersion == "" || tool.Version == "" || validateCapabilityVersion(tool, step.Call.CapabilityVersion) != nil {
		return issue("version_unavailable", "Pin an available capability version")
	}
	if tool.Execution == nil || !tool.Execution.Idempotent {
		return issue("recovery_not_supported", "Durable plans require an explicit idempotency contract")
	}
	sensitive := gatewayRedactionAuditSummaryForValue(step.Call.Input, gatewayRedactionRule{SecretTypes: []string{"default"}}, "input")
	if !sensitive.empty() {
		return issue("secret_reference_required", "Use SecretRef instead of persisting sensitive literal input")
	}
	if !validPlannedCapabilityInput(tool, step) {
		return issue("invalid_input", "Input does not match the selected capability version")
	}
	if tool.Execution.IdempotencyKeyField != "" {
		key, ok := step.Call.Input[tool.Execution.IdempotencyKeyField].(string)
		if !ok || strings.TrimSpace(key) == "" {
			return issue("idempotency_key_required", "Declare a stable idempotency key before submission")
		}
		for _, binding := range step.Bindings {
			if binding.InputPath == "/"+tool.Execution.IdempotencyKeyField {
				return issue("idempotency_binding_forbidden", "Idempotency keys cannot depend on runtime output")
			}
		}
	}
	return nil
}

func validatePlannedBindings(step domainaigateway.CapabilityPlanStep, steps map[string]domainaigateway.CapabilityPlanStep, tools map[string]domainaigateway.ToolCapability) []domainaigateway.CapabilityPlanIssue {
	issues := []domainaigateway.CapabilityPlanIssue{}
	for _, dependency := range step.DependsOn {
		if _, found := steps[dependency]; !found || dependency == step.ID {
			issues = append(issues, planIssue(step.ID, "invalid_dependency", "Dependency does not identify another plan step"))
		}
	}
	paths := []string{}
	for _, binding := range step.Bindings {
		sourceStep, found := steps[binding.StepID]
		if !found || !slices.Contains(step.DependsOn, binding.StepID) {
			issues = append(issues, planIssue(step.ID, "missing_dependency", "Each binding source must be an explicit dependency"))
			continue
		}
		_, sourceErr := capabilityPointerTokens(binding.OutputPath)
		_, targetErr := capabilityPointerTokens(binding.InputPath)
		if sourceErr != nil || targetErr != nil {
			issues = append(issues, planIssue(step.ID, "invalid_pointer", "Binding paths must be valid JSON pointers"))
			continue
		}
		for _, path := range paths {
			if path == binding.InputPath || strings.HasPrefix(path, binding.InputPath+"/") || strings.HasPrefix(binding.InputPath, path+"/") {
				issues = append(issues, planIssue(step.ID, "overlapping_binding", "Binding destinations cannot overlap"))
			}
		}
		paths = append(paths, binding.InputPath)
		if _, supplied := capabilityPointerGet(step.Call.Input, binding.InputPath); supplied {
			issues = append(issues, planIssue(step.ID, "binding_overwrites_input", "A binding must not overwrite a literal input"))
		}
		copyValue, copyErr := capabilityJSON(step.Call.Input)
		copyInput, _ := copyValue.(map[string]any)
		if copyInput == nil {
			copyInput = map[string]any{}
		}
		if copyErr != nil || capabilityPointerSet(copyInput, binding.InputPath, nil) != nil {
			issues = append(issues, planIssue(step.ID, "invalid_binding_parent", "A binding destination has a scalar parent or an invalid array position"))
		}
		source, sourceOK := capabilitySemantic(tools[sourceStep.Call.ToolName].OutputSemantics, binding.OutputPath)
		target, targetOK := capabilitySemantic(tools[step.Call.ToolName].InputSemantics, binding.InputPath)
		if !sourceOK || !targetOK || !compatibleCapabilitySemantics(source, target) {
			issues = append(issues, planIssue(step.ID, "semantic_mismatch", "Binding kinds, units and scope dimensions must match registered contracts"))
		}
	}
	return issues
}

func capabilityPlanHasCycle(steps map[string]domainaigateway.CapabilityPlanStep) bool {
	marks := map[string]int{}
	var visit func(string) bool
	visit = func(id string) bool {
		if marks[id] == 1 {
			return true
		}
		if marks[id] == 2 {
			return false
		}
		marks[id] = 1
		for _, dependency := range steps[id].DependsOn {
			if _, found := steps[dependency]; found && visit(dependency) {
				return true
			}
		}
		marks[id] = 2
		return false
	}
	for id := range steps {
		if visit(id) {
			return true
		}
	}
	return false
}
