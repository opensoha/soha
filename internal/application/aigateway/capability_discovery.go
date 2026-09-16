package aigateway

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type capabilityCursor struct {
	Revision string `json:"revision"`
	Offset   int    `json:"offset"`
}

// Discovery runs after identity, grant, policy and skill filtering. Its cursor
// conveys a position in that visible catalog, never authorization.
func discoverCapabilities(tools []domainaigateway.ToolCapability, input domainaigateway.ManifestRequest) ([]domainaigateway.ToolCapability, string, string, error) {
	if input.Limit < 0 || input.Limit > 100 || len(input.Cursor) > 512 || len(input.Query) > 200 || len(input.ToolDomain) > 100 || len(input.Action) > 100 || len(input.ResourceKind) > 100 {
		return nil, "", "", fmt.Errorf("%w: invalid capability discovery filter", apperrors.ErrInvalidArgument)
	}
	found := make([]domainaigateway.ToolCapability, 0, len(tools))
	visible := make(map[string]domainaigateway.ToolCapability, len(tools))
	for _, tool := range tools {
		visible[tool.Name] = tool
	}
	for _, tool := range tools {
		if capabilityMatches(tool, input) {
			tool = withVisibleCapabilityChecks(tool, visible)
			found = append(found, tool)
		}
	}
	sort.Slice(found, func(i, j int) bool { return found[i].Name < found[j].Name })
	raw, err := json.Marshal(found)
	if err != nil {
		return nil, "", "", err
	}
	hash := sha256.Sum256(raw)
	revision := "sha256:" + hex.EncodeToString(hash[:])
	offset, err := capabilityDiscoveryOffset(input.Cursor, revision, len(found))
	if err != nil {
		return nil, "", "", err
	}
	limit := input.Limit
	if limit == 0 {
		if input.Query == "" && input.ToolDomain == "" && input.Action == "" && input.ResourceKind == "" && input.Cursor == "" {
			limit = len(found)
		} else {
			limit = 100
		}
	}
	end := min(offset+limit, len(found))
	next := ""
	if end < len(found) {
		cursor, _ := json.Marshal(capabilityCursor{Revision: revision, Offset: end})
		next = base64.RawURLEncoding.EncodeToString(cursor)
	}
	return found[offset:end], revision, next, nil
}

// A check is a discoverable read capability, not an implicit execution step.
// Copy metadata so one caller's grants cannot mutate the shared catalog.
func withVisibleCapabilityChecks(tool domainaigateway.ToolCapability, visible map[string]domainaigateway.ToolCapability) domainaigateway.ToolCapability {
	if tool.Execution == nil || len(tool.Execution.Checks) == 0 {
		return tool
	}
	execution := *tool.Execution
	execution.Checks = nil
	for _, check := range tool.Execution.Checks {
		target, ok := visible[check.ToolName]
		if !ok || check.CapabilityVersion == "" || check.CapabilityVersion != target.Version || target.Execution == nil || target.Execution.Mode != "sync" || !target.Execution.Idempotent {
			continue
		}
		if target.RiskLevel != domainaigateway.RiskLevelRead && target.RiskLevel != domainaigateway.RiskLevelAnalyze {
			continue
		}
		switch check.Purpose {
		case "availability", "precondition":
		case "verification":
			if !target.ProducesAssessment {
				continue
			}
		default:
			continue
		}
		if !slices.Contains(execution.Checks, check) {
			execution.Checks = append(execution.Checks, check)
		}
	}
	tool.Execution = &execution
	return tool
}

func capabilityDiscoveryOffset(value, revision string, count int) (int, error) {
	offset := 0
	if value != "" {
		data, err := base64.RawURLEncoding.DecodeString(value)
		var cursor capabilityCursor
		if err != nil || json.Unmarshal(data, &cursor) != nil || cursor.Offset < 0 || cursor.Offset > count {
			return 0, fmt.Errorf("%w: invalid capability cursor", apperrors.ErrInvalidArgument)
		}
		if cursor.Revision != revision {
			return 0, fmt.Errorf("%w: visible capability catalog changed; restart discovery", apperrors.ErrConflict)
		}
		offset = cursor.Offset
	}
	return offset, nil
}

func capabilityMatches(tool domainaigateway.ToolCapability, input domainaigateway.ManifestRequest) bool {
	if input.ToolDomain != "" && tool.Domain != input.ToolDomain {
		return false
	}
	if input.Action != "" && tool.Action != input.Action {
		return false
	}
	if input.ResourceKind != "" && !slices.ContainsFunc(tool.InputSemantics, func(value domainaigateway.CapabilityValueSemantic) bool { return value.Kind == input.ResourceKind }) {
		return false
	}
	text := strings.ToLower(strings.Join([]string{tool.Name, tool.Title, tool.Description, tool.Domain, tool.Action, strings.Join(tool.Effects, " ")}, " "))
	for _, term := range strings.Fields(strings.ToLower(input.Query)) {
		if !strings.Contains(text, term) {
			return false
		}
	}
	return true
}
