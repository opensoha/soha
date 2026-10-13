package copilot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"

	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type nativeToolDefinition struct {
	Version string `json:"version"`
	Digest  string `json:"digest"`
}

type nativeCatalogSnapshot struct {
	Revision string                          `json:"revision"`
	Tools    map[string]nativeToolDefinition `json:"tools"`
	Tasks    map[string]nativeToolDefinition `json:"tasks,omitempty"`
}

func nativeDefinition(tool domainaigateway.ToolCapability) (nativeToolDefinition, error) {
	if tool.Name == "" || tool.Version == "" {
		return nativeToolDefinition{}, fmt.Errorf("%w: Native tool lacks a capability version", apperrors.ErrInvalidArgument)
	}
	data, err := json.Marshal(tool)
	if err != nil || len(data) > 65536 {
		return nativeToolDefinition{}, fmt.Errorf("%w: Native capability definition is invalid or exceeds its budget", apperrors.ErrInvalidArgument)
	}
	digest := sha256.Sum256(data)
	return nativeToolDefinition{Version: tool.Version, Digest: hex.EncodeToString(digest[:])}, nil
}

func (s *Service) nativeGatewayCatalog(ctx context.Context, principal domainidentity.Principal, sessionID string) (domainaigateway.Manifest, error) {
	reader, ok := s.workbenchInvoker.(nativeCapabilities)
	if !ok {
		return domainaigateway.Manifest{}, apperrors.ErrUnsupportedOperation
	}
	return reader.WorkbenchCapabilities(ctx, principal, domainaigateway.ManifestRequest{SessionID: sessionID})
}

func (s *Service) freezeNativeCatalog(ctx context.Context, principal domainidentity.Principal, sessionID string, bindings []domaincopilot.AgentToolBinding, intent *domaincopilot.TaskIntent, disabled ...[]string) ([]domaincopilot.AgentToolBinding, nativeCatalogSnapshot, error) {
	snapshot := nativeCatalogSnapshot{Tools: map[string]nativeToolDefinition{}, Tasks: map[string]nativeToolDefinition{}}
	catalog, err := s.nativeGatewayCatalog(ctx, principal, sessionID)
	if err != nil && !errors.Is(err, apperrors.ErrAccessDenied) && !errors.Is(err, apperrors.ErrUnsupportedOperation) {
		return nil, snapshot, err
	}
	snapshot.Revision = catalog.CatalogRevision
	if s.nativeTasks != nil {
		snapshot.Tasks, err = freezeNativeTaskCatalog(catalog.Tools, disabled)
		if err != nil {
			return nil, snapshot, err
		}
	}
	aliases := map[string]string{}
	for _, tool := range catalog.Tools {
		if !slices.ContainsFunc(bindings, func(binding domaincopilot.AgentToolBinding) bool { return binding.ToolName == tool.Name }) || !nativeReadCapability(tool) {
			continue
		}
		alias := nativeFunctionName(tool.Name)
		if previous := aliases[alias]; previous != "" && previous != tool.Name {
			return nil, snapshot, fmt.Errorf("%w: ambiguous Native function name", apperrors.ErrInvalidArgument)
		}
		aliases[alias] = tool.Name
		definition, err := nativeDefinition(tool)
		if err != nil {
			return nil, snapshot, err
		}
		snapshot.Tools[tool.Name] = definition
	}
	if err := validateNativeCatalogIntent(intent, snapshot.Tools); err != nil {
		return nil, snapshot, err
	}
	frozen := []domaincopilot.AgentToolBinding{}
	for _, binding := range bindings {
		definition, allowed := snapshot.Tools[binding.ToolName]
		if !allowed && binding.ToolName != "harness.request_input" {
			continue
		}
		binding.Config = maps.Clone(binding.Config)
		if binding.Config == nil {
			binding.Config = map[string]any{}
		}
		binding.Config["capabilityVersion"] = definition.Version
		frozen = append(frozen, binding)
	}
	return frozen, snapshot, nil
}

func freezeNativeTaskCatalog(tools []domainaigateway.ToolCapability, disabled [][]string) (map[string]nativeToolDefinition, error) {
	frozen := map[string]nativeToolDefinition{}
	byName := map[string]domainaigateway.ToolCapability{}
	for _, tool := range tools {
		byName[tool.Name] = tool
	}
	if nativeTaskDisabled("harness.request_task", disabled) || nativeTaskDisabled("harness.discover_tasks", disabled) {
		return frozen, nil
	}
	for _, tool := range tools {
		if !nativeTaskCapability(tool) || nativeTaskDisabled(tool.Name, disabled) {
			continue
		}
		definitions, err := nativeTaskDefinitions(tool, byName, disabled)
		if err != nil {
			return nil, err
		}
		maps.Copy(frozen, definitions)
	}
	if len(frozen) > 512 {
		return nil, fmt.Errorf("%w: Native task catalog exceeds its budget", apperrors.ErrInvalidArgument)
	}
	return frozen, nil
}

func nativeTaskDisabled(name string, disabled [][]string) bool {
	return slices.ContainsFunc(disabled, func(names []string) bool { return slices.Contains(names, name) })
}

func nativeTaskDefinitions(tool domainaigateway.ToolCapability, catalog map[string]domainaigateway.ToolCapability, disabled [][]string) (map[string]nativeToolDefinition, error) {
	references := slices.Clone(tool.Execution.Checks)
	for _, name := range []string{tool.Execution.StatusTool, tool.Execution.CancelTool} {
		if name != "" {
			references = append(references, domainaigateway.CapabilityCheckReference{ToolName: name, CapabilityVersion: catalog[name].Version})
		}
	}
	definitions := map[string]nativeToolDefinition{}
	for _, reference := range references {
		current, found := catalog[reference.ToolName]
		if !found || reference.CapabilityVersion == "" || current.Version != reference.CapabilityVersion || nativeTaskDisabled(current.Name, disabled) {
			return nil, nil // No governed write without its currently available checks and recovery controls.
		}
		definition, err := nativeDefinition(current)
		if err != nil {
			return nil, err
		}
		definitions[current.Name] = definition
	}
	definition, err := nativeDefinition(tool)
	definitions[tool.Name] = definition
	return definitions, err
}

func nativeTaskCapability(tool domainaigateway.ToolCapability) bool {
	execution := tool.Execution
	if tool.Version == "" || execution == nil || !execution.Idempotent || (execution.Mode != "sync" && execution.Mode != "async") {
		return false
	}
	if execution.Mode == "async" && (execution.StatusTool == "" || execution.CancelTool == "" || execution.RecoveryMode == "") {
		return false
	}
	return nativeReadCapability(tool) || slices.ContainsFunc(execution.Checks, func(check domainaigateway.CapabilityCheckReference) bool { return check.Purpose == "verification" })
}

func (s *Service) currentNativeTaskCatalog(ctx context.Context, principal domainidentity.Principal, run domaincopilot.AgentRun) (map[string]domainaigateway.ToolCapability, error) {
	snapshot, frozen, err := nativeCatalogFrom(run)
	if err != nil || !frozen || len(snapshot.Tasks) == 0 {
		return nil, apperrors.ErrAccessDenied
	}
	catalog, err := s.nativeGatewayCatalog(ctx, principal, run.SessionID)
	if err != nil {
		return nil, err
	}
	current := map[string]domainaigateway.ToolCapability{}
	for _, tool := range catalog.Tools {
		definition, err := nativeDefinition(tool)
		if err == nil && snapshot.Tasks[tool.Name] == definition {
			current[tool.Name] = tool
		}
	}
	if len(current) != len(snapshot.Tasks) {
		return nil, fmt.Errorf("%w: frozen Native task capability changed or was revoked", apperrors.ErrAccessDenied)
	}
	return current, nil
}

func nativeReadCapability(tool domainaigateway.ToolCapability) bool {
	return tool.RiskLevel == domainaigateway.RiskLevelRead || tool.RiskLevel == domainaigateway.RiskLevelAnalyze
}

func validateNativeCatalogIntent(intent *domaincopilot.TaskIntent, available map[string]nativeToolDefinition) error {
	if intent == nil {
		return nil
	}
	for _, capability := range intent.Capabilities {
		if _, ok := available["copilot."+string(capability)+".analyze"]; !ok {
			return fmt.Errorf("%w: selected professional capability is not currently available", apperrors.ErrAccessDenied)
		}
	}
	return nil
}

func validateNativeRunCatalogIntent(run domaincopilot.AgentRun, available map[string]nativeToolDefinition) error {
	var intent domaincopilot.TaskIntent
	data, err := json.Marshal(run.Input["taskIntent"])
	if err == nil {
		err = json.Unmarshal(data, &intent)
	}
	if err != nil {
		return err
	}
	return validateNativeCatalogIntent(&intent, available)
}

func nativeCatalogFrom(run domaincopilot.AgentRun) (nativeCatalogSnapshot, bool, error) {
	value, exists := run.Input["_sohaNativeCatalog"]
	if !exists {
		return nativeCatalogSnapshot{}, false, nil
	}
	var snapshot nativeCatalogSnapshot
	data, err := json.Marshal(value)
	if err == nil {
		err = json.Unmarshal(data, &snapshot)
	}
	if err != nil || snapshot.Tools == nil {
		return snapshot, true, apperrors.ErrInvalidArgument
	}
	return snapshot, true, nil
}

func nativeCatalogMatches(snapshot nativeCatalogSnapshot, tool domainaigateway.ToolCapability) bool {
	definition, err := nativeDefinition(tool)
	return err == nil && snapshot.Tools[tool.Name] == definition
}

func (s *Service) authorizeNativeCatalogTool(ctx context.Context, principal domainidentity.Principal, run domaincopilot.AgentRun, binding domaincopilot.AgentToolBinding) error {
	snapshot, frozen, err := nativeCatalogFrom(run)
	if err != nil || !frozen {
		return err // Read-only legacy Native runs retain their original compatibility.
	}
	catalog, err := s.nativeGatewayCatalog(ctx, principal, run.SessionID)
	if err != nil {
		return err
	}
	for _, tool := range catalog.Tools {
		if tool.Name == binding.ToolName && nativeReadCapability(tool) && nativeCatalogMatches(snapshot, tool) {
			return nil
		}
	}
	return fmt.Errorf("%w: frozen Native capability changed or was revoked", apperrors.ErrAccessDenied)
}
