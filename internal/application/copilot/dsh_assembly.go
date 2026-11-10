package copilot

import (
	"context"
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	domain "github.com/opensoha/soha/internal/domain/copilot"
	identity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const dshAssemblySchema = "opensoha.dev/dsh-runtime/v1"

func (s *Service) findProfiledDSHRetry(ctx context.Context, owner, sessionID, requestID, fingerprint string) (*domain.AgentRun, error) {
	items, err := s.agentRuns.ListAgentRuns(ctx, domain.AgentRunFilter{CreatedBy: owner, SessionID: sessionID, NativeRequestID: requestID, Limit: 1})
	if err != nil || len(items) == 0 {
		return nil, err
	}
	saved := stringValue(items[0].Input["runtimeRequestFingerprint"])
	if saved == "" {
		return nil, nil
	}
	if saved != fingerprint {
		return nil, apperrors.ErrConflict
	}
	return &items[0], nil
}

func frozenDSHProfile(run domain.AgentRun) (*domain.RuntimeProfile, error) {
	value, exists := run.Input["runtimeProfile"]
	if !exists {
		return nil, nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var profile domain.RuntimeProfile
	if err := json.Unmarshal(raw, &profile); err != nil || profile.ID == "" || profile.Revision <= 0 {
		return nil, apperrors.ErrConflict
	}
	return &profile, nil
}

func (s *Service) attachDSHAssembly(ctx context.Context, run domain.AgentRun, start *domain.DSHStart) error {
	profile, err := frozenDSHProfile(run)
	if err != nil || profile == nil {
		return err
	}
	if profile.Scope != start.Profile {
		return apperrors.ErrAccessDenied
	}
	if err := s.authorizeRuntimeProfile(ctx, profile.ID, profile.Revision, profile.Scope); err != nil {
		return err
	}
	if err := s.authorizeRuntimeAssets(ctx, profile.Assets); err != nil {
		return err
	}
	if profile.MaxOutputTokens > 0 {
		start.MaxOutputTokens = min(start.MaxOutputTokens, int64(profile.MaxOutputTokens))
	}
	start.Assembly = &domain.DSHAssembly{
		ProfileID: profile.ID,
		Revision:  profile.Revision,
		Assets:    append([]domain.RuntimeAssetRef{}, profile.Assets...),
	}
	return nil
}

func (s *Service) authorizeDSHProfileBinding(ctx context.Context, principal identity.Principal, run domain.AgentRun, start *domain.DSHStart) error {
	if start == nil || start.Assembly == nil {
		return nil
	}
	session, err := s.ownedSession(ctx, principal, run.SessionID)
	if err != nil {
		return err
	}
	if runtimeProfileScope(session) != start.Profile || parseSessionMetadata(session.Metadata).ArchivedAt != "" {
		return apperrors.ErrAccessDenied
	}
	if err := s.authorizeRuntimeProfile(ctx, start.Assembly.ProfileID, start.Assembly.Revision, start.Profile); err != nil {
		return err
	}
	return s.authorizeRuntimeAssets(ctx, start.Assembly.Assets)
}

func rejectUnconfirmedDSHAssembly(run *domain.AgentRun, cp dshCheckpoint) {
	if profile, err := frozenDSHProfile(*run); err == nil && profile != nil && cp.AssemblyDecision == nil {
		run.Output["runtimeAssembly"] = domain.RuntimeAssembly{ProfileID: profile.ID, Revision: profile.Revision, Status: "rejected", Assets: []domain.RuntimeAssetStatus{}}
	}
}

func (s *Service) ConfirmDSHAssembly(ctx context.Context, grant string, report domain.DSHAssemblyReport) (domain.DSHAssemblyDecision, error) {
	if report.SchemaVersion != dshAssemblySchema || report.Kind != "assembly_report" || report.RunID == "" {
		return domain.DSHAssemblyDecision{}, apperrors.ErrInvalidArgument
	}
	lock := s.dshLock(report.RunID)
	lock.Lock()
	defer lock.Unlock()
	id := domain.DSHIdentity{RunID: report.RunID, Attempt: report.Attempt, Fence: report.Fence}
	lease, cp, _, ctx, err := s.authorizeDSHCallback(ctx, grant, id)
	if err != nil {
		return domain.DSHAssemblyDecision{}, err
	}
	if !cp.Submitted || cp.Start.Assembly == nil {
		return domain.DSHAssemblyDecision{}, apperrors.ErrConflict
	}
	decision, err := decideDSHAssembly(*cp.Start.Assembly, report)
	if err != nil {
		return domain.DSHAssemblyDecision{}, err
	}
	if len(cp.Start.Tools)+len(report.NativeTools) > 64 || slices.ContainsFunc(report.NativeTools, func(tool domain.DSHNativeTool) bool {
		return slices.ContainsFunc(cp.Start.Tools, func(base domain.DSHTool) bool { return base.Name == tool.Name })
	}) {
		return domain.DSHAssemblyDecision{}, apperrors.ErrAccessDenied
	}
	if cp.AssemblyDecision != nil {
		if dshHash(*cp.AssemblyDecision) != dshHash(decision) {
			return domain.DSHAssemblyDecision{}, apperrors.ErrConflict
		}
		return *cp.AssemblyDecision, nil
	}
	cp.AssemblyDecision = &decision
	assembly := domain.RuntimeAssembly{ProfileID: decision.ProfileID, Revision: decision.Revision, Status: decision.Status, Assets: decision.Assets}
	lease.Run.Output["runtimeAssembly"] = assembly
	event := domain.WorkbenchStreamEvent{Type: "agent.status", RunID: lease.Run.ID, ProviderID: lease.Run.ProviderID, ProviderKind: lease.Run.ProviderKind, Status: lease.Run.Status, Metadata: map[string]any{"runtimeAssembly": assembly}}
	if _, err = s.saveDSH(ctx, lease, cp, lease.Phase, []domain.WorkbenchStreamEvent{event}); err != nil {
		return domain.DSHAssemblyDecision{}, err
	}
	return decision, nil
}

func decideDSHAssembly(assembly domain.DSHAssembly, report domain.DSHAssemblyReport) (domain.DSHAssemblyDecision, error) {
	if report.ProfileID != assembly.ProfileID || report.Revision != assembly.Revision || report.Assets == nil || len(report.Assets) != len(assembly.Assets) {
		return domain.DSHAssemblyDecision{}, apperrors.ErrAccessDenied
	}
	decision := domain.DSHAssemblyDecision{DSHAssemblyReport: report, Status: "confirmed"}
	decision.Kind = "assembly_decision"
	discovered, err := indexDSHNativeTools(report.NativeTools)
	if err != nil {
		return domain.DSHAssemblyDecision{}, err
	}
	for i, asset := range assembly.Assets {
		actual := report.Assets[i]
		if actual.Kind != asset.Kind || actual.ID != asset.ID || actual.Version != asset.Version {
			return domain.DSHAssemblyDecision{}, apperrors.ErrAccessDenied
		}
		switch actual.Status {
		case "loaded":
			key := [3]string{actual.Kind, actual.ID, actual.Version}
			if !validLoadedDSHAsset(actual, discovered[key]) {
				return domain.DSHAssemblyDecision{}, apperrors.ErrAccessDenied
			}
			delete(discovered, key)
		case "unavailable":
			if len(actual.Capabilities) != 0 || !slices.Contains([]string{"asset_unavailable", "asset_initialization_failed", "dependency_unavailable"}, actual.Code) {
				return domain.DSHAssemblyDecision{}, apperrors.ErrAccessDenied
			}
			if asset.Required {
				decision.Status = "rejected"
			} else if decision.Status == "confirmed" {
				decision.Status = "degraded"
			}
		default:
			return domain.DSHAssemblyDecision{}, apperrors.ErrInvalidArgument
		}
	}
	if len(discovered) != 0 {
		return domain.DSHAssemblyDecision{}, apperrors.ErrAccessDenied
	}
	return decision, nil
}

func validLoadedDSHAsset(asset domain.RuntimeAssetStatus, tools []string) bool {
	if asset.Code != "" {
		return false
	}
	if asset.Kind == "hook" {
		return asset.Version == "1" && (asset.ID == "tool-limit" || asset.ID == "tool-audit") && slices.Equal(asset.Capabilities, []string{asset.ID})
	}
	return len(tools) > 0 && slices.Equal(asset.Capabilities, tools)
}

var dshPluginToolName = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

func indexDSHNativeTools(tools []domain.DSHNativeTool) (map[[3]string][]string, error) {
	index := make(map[[3]string][]string)
	seen := make(map[string]bool)
	for _, tool := range tools {
		prefix := tool.Kind + "_" + strings.ReplaceAll(tool.AssetID, "-", "_") + "_"
		if (tool.Kind != "mcp" && tool.Kind != "skill") || !strings.HasPrefix(tool.Name, prefix) || !dshPluginToolName.MatchString(tool.Name) || seen[tool.Name] || utf8.RuneCountInString(tool.Description) > 512 {
			return nil, apperrors.ErrAccessDenied
		}
		var schema map[string]any
		if len(tool.InputSchema) > 131072 || json.Unmarshal(tool.InputSchema, &schema) != nil || stringValue(schema["type"]) != "object" {
			return nil, apperrors.ErrInvalidArgument
		}
		key := [3]string{tool.Kind, tool.AssetID, tool.Version}
		index[key] = append(index[key], tool.Name)
		seen[tool.Name] = true
	}
	return index, nil
}

func dshNativeTools(cp dshCheckpoint) []domain.DSHNativeTool {
	if cp.AssemblyDecision == nil || !dshAssemblyConfirmed(cp) {
		return nil
	}
	return cp.AssemblyDecision.NativeTools
}

func dshEffectiveStart(cp dshCheckpoint) domain.DSHStart {
	start := *cp.Start
	if tools := dshNativeTools(cp); len(tools) > 0 {
		start.Tools = slices.Clone(start.Tools)
		for _, tool := range tools {
			start.Tools = append(start.Tools, domain.DSHTool{Name: tool.Name, Description: tool.Description, InputSchema: tool.InputSchema, ReadOnly: true})
		}
	}
	return start
}

func dshAssemblyConfirmed(cp dshCheckpoint) bool {
	return cp.Start.Assembly == nil || (cp.AssemblyDecision != nil && (cp.AssemblyDecision.Status == "confirmed" || cp.AssemblyDecision.Status == "degraded"))
}

func dshHookAuditLoaded(cp dshCheckpoint) bool {
	return cp.AssemblyDecision != nil && slices.ContainsFunc(cp.AssemblyDecision.Assets, func(asset domain.RuntimeAssetStatus) bool {
		return asset.Kind == "hook" && asset.ID == "tool-audit" && asset.Status == "loaded"
	})
}

func dshHookObservation(cp dshCheckpoint, event domain.DSHEvent) map[string]any {
	if !dshHookAuditLoaded(cp) {
		return nil
	}
	name := cp.ToolNames[event.CallID]
	if name == "" {
		return nil
	}
	return map[string]any{"id": "tool-audit", "version": "1", "callId": event.CallID, "toolName": name, "outcome": event.Outcome}
}
