package knowledge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

const sourceImportPolicyVersion = "opensoha.dev/knowledge-source-import-policy/v1"

type SourceImportPolicy struct {
	SchemaVersion string      `json:"schemaVersion"`
	Scope         AccessScope `json:"scope"`
}

func ParseSourceImportPolicy(config map[string]any) (*SourceImportPolicy, error) {
	value, present := config["importPolicy"]
	if !present {
		return nil, nil
	}
	payload, err := json.Marshal(value)
	if err != nil || len(payload) > 32<<10 {
		return nil, fmt.Errorf("%w: source import policy must be bounded JSON", ErrInvalidInput)
	}
	var policy *SourceImportPolicy
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&policy); err != nil || policy == nil || policy.SchemaVersion != sourceImportPolicyVersion {
		return nil, fmt.Errorf("%w: source import policy is invalid or unsupported", ErrInvalidInput)
	}
	switch policy.Scope.Visibility {
	case "private", "scoped", "public":
	default:
		return nil, fmt.Errorf("%w: source import visibility is invalid", ErrInvalidInput)
	}
	if err := validateSourceImportGrants(payload, policy.Scope); err != nil {
		return nil, err
	}
	return policy, nil
}

func validateSourceImportGrants(payload []byte, scope AccessScope) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(payload, &raw); err != nil || len(raw) != 2 || raw["schemaVersion"] == nil || raw["scope"] == nil {
		return fmt.Errorf("%w: source import policy fields are invalid", ErrInvalidInput)
	}
	var rawScope map[string]json.RawMessage
	if err := json.Unmarshal(raw["scope"], &rawScope); err != nil || rawScope["visibility"] == nil {
		return fmt.Errorf("%w: source import scope is invalid", ErrInvalidInput)
	}
	// encoding/json matches struct fields without case sensitivity; the schema does not.
	for field := range rawScope {
		switch field {
		case "visibility", "users", "roles", "teams", "projects":
		default:
			return fmt.Errorf("%w: source import scope fields are invalid", ErrInvalidInput)
		}
	}
	for name, grants := range map[string][]string{"users": scope.Users, "roles": scope.Roles, "teams": scope.Teams, "projects": scope.Projects} {
		if bytes.Equal(bytes.TrimSpace(rawScope[name]), []byte("null")) || len(grants) > 100 {
			return fmt.Errorf("%w: source import grants must be bounded arrays", ErrInvalidInput)
		}
		seen := make(map[string]bool, len(grants))
		for _, id := range grants {
			if parts := strings.Fields(id); len(parts) != 1 || parts[0] != id || utf8.RuneCountInString(id) > 128 || seen[id] {
				return fmt.Errorf("%w: source import grants contain an invalid or duplicate ID", ErrInvalidInput)
			}
			seen[id] = true
		}
	}
	return nil
}
