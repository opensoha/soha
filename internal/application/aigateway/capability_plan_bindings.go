package aigateway

import (
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"

	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func capabilityPointerTokens(path string) ([]string, error) {
	if !strings.HasPrefix(path, "/") || len(path) > 512 {
		return nil, fmt.Errorf("invalid JSON pointer")
	}
	tokens := strings.Split(path[1:], "/")
	for i, token := range tokens {
		for j := 0; j < len(token); j++ {
			if token[j] != '~' {
				continue
			}
			if j+1 == len(token) || (token[j+1] != '0' && token[j+1] != '1') {
				return nil, fmt.Errorf("invalid JSON pointer escape")
			}
			j++
		}
		tokens[i] = strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~")
	}
	return tokens, nil
}

func capabilityPointerGet(value any, path string) (any, bool) {
	tokens, err := capabilityPointerTokens(path)
	if err != nil {
		return nil, false
	}
	for _, token := range tokens {
		switch object := value.(type) {
		case map[string]any:
			var found bool
			value, found = object[token]
			if !found {
				return nil, false
			}
		case []any:
			index, err := strconv.Atoi(token)
			if err != nil || index < 0 || index >= len(object) || strconv.Itoa(index) != token {
				return nil, false
			}
			value = object[index]
		default:
			return nil, false
		}
	}
	return value, true
}

func capabilityPointerSet(input map[string]any, path string, value any) error {
	tokens, err := capabilityPointerTokens(path)
	if err != nil {
		return err
	}
	var parent any = input
	for i, token := range tokens {
		last := i == len(tokens)-1
		switch object := parent.(type) {
		case map[string]any:
			if last {
				object[token] = value
				return nil
			}
			if _, found := object[token]; !found {
				object[token] = map[string]any{}
			}
			parent = object[token]
		case []any:
			index, err := strconv.Atoi(token)
			if err != nil || index < 0 || index >= len(object) || strconv.Itoa(index) != token {
				return fmt.Errorf("invalid binding array position")
			}
			if last {
				object[index] = value
				return nil
			}
			parent = object[index]
		default:
			return fmt.Errorf("binding parent is not a container")
		}
	}
	return nil
}

func capabilitySemantic(values []domainaigateway.CapabilityValueSemantic, path string) (domainaigateway.CapabilityValueSemantic, bool) {
	for _, value := range values {
		if concrete, ok := matchCapabilitySemantic(value, path); ok {
			return concrete, true
		}
	}
	return domainaigateway.CapabilityValueSemantic{}, false
}

func compatibleCapabilitySemantics(source, target domainaigateway.CapabilityValueSemantic) bool {
	if source.Kind == "" || source.Kind != target.Kind || source.Unit != target.Unit || len(source.ScopePaths) != len(target.ScopePaths) {
		return false
	}
	for key := range target.ScopePaths {
		if _, found := source.ScopePaths[key]; !found {
			return false
		}
	}
	return true
}

// Bind only prior, successfully observed domain output. Schema and actual scope
// checks happen after all values are resolved; failed or masked output is unusable.
func (s *Service) ResolveCapabilityStep(step domainaigateway.CapabilityPlanStep, plan domainaigateway.CapabilityPlan, completed map[string]domainaigateway.ToolInvocationResult) (domainaigateway.ToolInvocationRequest, error) {
	tool, found := s.toolByName(step.Call.ToolName)
	if !found || step.Call.CapabilityVersion == "" || validateCapabilityVersion(tool, step.Call.CapabilityVersion) != nil {
		return domainaigateway.ToolInvocationRequest{}, apperrors.ErrConflict
	}
	value, err := capabilityJSON(step.Call.Input)
	if err != nil {
		return domainaigateway.ToolInvocationRequest{}, err
	}
	input, ok := value.(map[string]any)
	if !ok {
		input = map[string]any{}
	}
	sources := map[string]domainaigateway.CapabilityPlanStep{}
	for _, candidate := range plan.Steps {
		sources[candidate.ID] = candidate
	}
	for _, binding := range step.Bindings {
		if _, exists := sources[binding.StepID]; !exists || !slices.Contains(step.DependsOn, binding.StepID) {
			return domainaigateway.ToolInvocationRequest{}, fmt.Errorf("%w: binding source is not an explicit dependency", apperrors.ErrInvalidArgument)
		}
		result, found := completed[binding.StepID]
		if !found {
			return domainaigateway.ToolInvocationRequest{}, apperrors.ErrConflict
		}
		if err := bindCapabilityOutput(input, binding, result); err != nil {
			return domainaigateway.ToolInvocationRequest{}, err
		}
	}
	for _, binding := range step.Bindings {
		sourceTool, _ := s.toolByName(sources[binding.StepID].Call.ToolName)
		source, _ := capabilitySemantic(sourceTool.OutputSemantics, binding.OutputPath)
		target, _ := capabilitySemantic(tool.InputSemantics, binding.InputPath)
		if !compatibleCapabilitySemantics(source, target) {
			return domainaigateway.ToolInvocationRequest{}, fmt.Errorf("%w: binding semantics changed", apperrors.ErrConflict)
		}
		output, _ := capabilityJSON(completed[binding.StepID].Output)
		for scope, inputPath := range target.ScopePaths {
			left, sourceOK := capabilityPointerGet(output, source.ScopePaths[scope])
			right, targetOK := capabilityPointerGet(input, inputPath)
			if !sourceOK || !targetOK || !reflect.DeepEqual(left, right) {
				return domainaigateway.ToolInvocationRequest{}, fmt.Errorf("%w: binding resource scope does not match", apperrors.ErrAccessDenied)
			}
		}
	}
	if err := validateCapabilityInput(tool, input); err != nil {
		return domainaigateway.ToolInvocationRequest{}, err
	}
	return domainaigateway.ToolInvocationRequest{ToolName: tool.Name, CapabilityVersion: tool.Version, Input: input, SecretRefs: step.Call.SecretRefs}, nil
}

func bindCapabilityOutput(input map[string]any, binding domainaigateway.CapabilityInputBinding, result domainaigateway.ToolInvocationResult) error {
	if result.Result != "success" || (result.Task != nil && (!result.Task.Terminal || result.Task.Outcome != "succeeded")) {
		return fmt.Errorf("%w: binding source is not complete", apperrors.ErrConflict)
	}
	output, err := capabilityJSON(result.Output)
	if err != nil {
		return err
	}
	bound, ok := capabilityPointerGet(output, binding.OutputPath)
	if !ok {
		return fmt.Errorf("%w: binding output is missing or redacted", apperrors.ErrInvalidArgument)
	}
	if err := capabilityPointerSet(input, binding.InputPath, bound); err != nil {
		return fmt.Errorf("%w: invalid binding destination", apperrors.ErrInvalidArgument)
	}
	return nil
}

func matchCapabilitySemantic(value domainaigateway.CapabilityValueSemantic, path string) (domainaigateway.CapabilityValueSemantic, bool) {
	pattern, err := capabilityPointerTokens(value.Path)
	actual, actualErr := capabilityPointerTokens(path)
	if err != nil || actualErr != nil || len(pattern) != len(actual) {
		return value, false
	}
	captures := []string{}
	for i, token := range pattern {
		if token != "*" {
			if token != actual[i] {
				return value, false
			}
			continue
		}
		index, err := strconv.Atoi(actual[i])
		if err != nil || index < 0 || strconv.Itoa(index) != actual[i] {
			return value, false
		}
		captures = append(captures, actual[i])
	}
	value.Path = path
	scopes := map[string]string{}
	for name, scope := range value.ScopePaths {
		tokens, err := capabilityPointerTokens(scope)
		if err != nil {
			return value, false
		}
		next := 0
		for i, token := range tokens {
			if token == "*" {
				if next >= len(captures) {
					return value, false
				}
				tokens[i] = captures[next]
				next++
			}
			tokens[i] = strings.ReplaceAll(strings.ReplaceAll(tokens[i], "~", "~0"), "/", "~1")
		}
		scopes[name] = "/" + strings.Join(tokens, "/")
	}
	value.ScopePaths = scopes
	return value, true
}
