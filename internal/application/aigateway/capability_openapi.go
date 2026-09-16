package aigateway

import (
	"encoding/json"
	"fmt"
	contractsopenapi "github.com/opensoha/soha-contracts/openapi"
	"strings"
)

// Publish the canonical input with only its transitive local references. MCP
// clients receive a self-contained schema, not a second copy of the HTTP DTO.
func capabilityOpenAPISchema(name string) (map[string]any, error) {
	var document struct {
		Components struct {
			Schemas map[string]json.RawMessage `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(contractsopenapi.JSON(), &document); err != nil {
		return nil, err
	}
	definitions := map[string]any{}
	var collect func(string) (map[string]any, error)
	var visit func(any) error
	collect = func(name string) (map[string]any, error) {
		var schema map[string]any
		if err := json.Unmarshal(document.Components.Schemas[name], &schema); err != nil {
			return nil, fmt.Errorf("capability schema %s: %w", name, err)
		}
		definitions[name] = schema // Mark before visiting to support recursive DTOs.
		if err := visit(schema); err != nil {
			return nil, err
		}
		return schema, nil
	}
	visit = func(value any) error {
		switch item := value.(type) {
		case map[string]any:
			for key, child := range item {
				if key == "$ref" {
					ref, ok := child.(string)
					if !ok || !strings.HasPrefix(ref, "#/components/schemas/") {
						return fmt.Errorf("unsupported capability schema reference")
					}
					target := strings.TrimPrefix(ref, "#/components/schemas/")
					item[key] = "#/$defs/" + target
					if _, exists := definitions[target]; !exists {
						if _, err := collect(target); err != nil {
							return err
						}
					}
				} else if err := visit(child); err != nil {
					return err
				}
			}
		case []any:
			for _, child := range item {
				if err := visit(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	root, err := collect(name)
	if err != nil {
		return nil, err
	}
	// Clone the root so recursive definitions cannot create a Go map cycle.
	raw, err := json.Marshal(root)
	if err != nil {
		return nil, err
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		return nil, err
	}
	schema["$defs"] = definitions
	return schema, nil
}

// capabilityOpenAPIObject exposes validated object properties for the providers
// that add their own stable operation keys to an existing public input.
func capabilityOpenAPIObject(name string) (map[string]any, map[string]any, error) {
	schema, err := capabilityOpenAPISchema(name)
	if err != nil {
		return nil, nil, err
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		return nil, nil, fmt.Errorf("capability schema %s has no object properties", name)
	}
	return schema, properties, nil
}
