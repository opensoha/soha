package catalog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// ResolveParameters applies whole top-level values, preserving false, zero and
// empty collections. Nested maps are validated against their declared schema.
func (s ParameterSchema) ResolveParameters(defaults, parameters, overrides map[string]any, allowed []string) (map[string]any, error) {
	if s.Type != "object" {
		return nil, fmt.Errorf("parameter schema root must be an object")
	}
	values := make(map[string]any, len(defaults)+len(parameters))
	for key, value := range defaults {
		values[key] = value
	}
	for key, value := range parameters {
		values[key] = value
	}
	for key, value := range overrides {
		if !slices.Contains(allowed, key) {
			return nil, fmt.Errorf("parameter %q cannot be overridden for an environment", key)
		}
		values[key] = value
	}
	data, err := json.Marshal(values)
	if err != nil || len(data) > 64<<10 {
		return nil, fmt.Errorf("parameters must be JSON and cannot exceed 64 KiB")
	}
	// Normalize Go numeric types and detach the caller's nested maps.
	if err := json.Unmarshal(data, &values); err != nil {
		return nil, err
	}
	if err := boundedParameterValue(values, 0); err != nil {
		return nil, err
	}
	schema, err := s.Compile()
	if err != nil {
		return nil, err
	}
	if err := schema.Validate(values); err != nil {
		// jsonschema errors can include the offending value (including credentials).
		return nil, fmt.Errorf("parameters do not satisfy the template schema")
	}
	return values, nil
}

func boundedParameterValue(value any, depth int) error {
	if depth > 12 {
		return fmt.Errorf("parameter nesting exceeds 12 levels")
	}
	switch typed := value.(type) {
	case map[string]any:
		if len(typed) > 64 {
			return fmt.Errorf("parameter object exceeds 64 fields")
		}
		for key, child := range typed {
			if len(key) > 128 {
				return fmt.Errorf("parameter name exceeds 128 bytes")
			}
			if err := boundedParameterValue(child, depth+1); err != nil {
				return err
			}
		}
	case []any:
		if len(typed) > 256 {
			return fmt.Errorf("parameter array exceeds 256 items")
		}
		for _, child := range typed {
			if err := boundedParameterValue(child, depth+1); err != nil {
				return err
			}
		}
	case string:
		if len(typed) > 16384 {
			return fmt.Errorf("parameter string exceeds 16384 bytes")
		}
	case bool, float64:
	default:
		return fmt.Errorf("parameters cannot contain null or unsupported values")
	}
	return nil
}

func (s ParameterSchema) Compile() (*jsonschema.Schema, error) {
	data, err := json.Marshal(s)
	if err != nil || len(data) > 64<<10 {
		return nil, fmt.Errorf("parameter schema exceeds 64 KiB")
	}
	document, err := parameterSchemaDocument(s, 0)
	if err != nil {
		return nil, err
	}
	compiler := jsonschema.NewCompiler()
	const location = "https://schemas.opensoha.invalid/deployment-parameters"
	data, err = json.Marshal(document)
	if err != nil {
		return nil, err
	}
	resource, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if err := compiler.AddResource(location, resource); err != nil {
		return nil, fmt.Errorf("invalid parameter schema: %w", err)
	}
	compiled, err := compiler.Compile(location)
	if err != nil {
		return nil, fmt.Errorf("invalid parameter schema")
	}
	return compiled, nil
}

func parameterSchemaDocument(s ParameterSchema, depth int) (map[string]any, error) {
	if depth > 12 || len(s.Properties) > 64 || len(s.Required) > 64 || len(s.Enum) > 64 {
		return nil, fmt.Errorf("parameter schema exceeds structural limits")
	}
	if !slices.Contains([]string{"string", "boolean", "number", "integer", "object", "array", "secret_reference"}, s.Type) {
		return nil, fmt.Errorf("unsupported parameter type %q", s.Type)
	}
	if err := validateParameterConstraints(s); err != nil {
		return nil, err
	}
	if s.Type == "secret_reference" {
		return map[string]any{"type": "object", "required": []string{"name", "key"}, "additionalProperties": false,
			"properties": map[string]any{
				"name": map[string]any{"type": "string", "minLength": 1, "maxLength": 253, "pattern": `^[a-z0-9]([-a-z0-9.]*[a-z0-9])?$`},
				"key":  map[string]any{"type": "string", "minLength": 1, "maxLength": 253, "pattern": `^[-._a-zA-Z0-9]+$`},
			}}, nil
	}
	data, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, err
	}
	delete(document, "mapValues")
	if s.Type == "object" {
		return parameterObjectDocument(s, document, depth)
	}
	if len(s.Properties) > 0 || len(s.Required) > 0 || s.MapValues != nil {
		return nil, fmt.Errorf("only object schemas may declare properties, required or mapValues")
	}
	if s.Type == "array" {
		if s.Items == nil {
			return nil, fmt.Errorf("array parameter schema requires items")
		}
		item, err := parameterSchemaDocument(*s.Items, depth+1)
		if err != nil {
			return nil, err
		}
		document["items"] = item
	} else if s.Items != nil {
		return nil, fmt.Errorf("only array schemas may declare items")
	}
	return document, nil
}

func parameterObjectDocument(s ParameterSchema, document map[string]any, depth int) (map[string]any, error) {
	if s.Items != nil {
		return nil, fmt.Errorf("object schemas cannot declare items")
	}
	properties := make(map[string]any, len(s.Properties))
	for key, property := range s.Properties {
		if key == "" || len(key) > 128 {
			return nil, fmt.Errorf("invalid parameter name")
		}
		child, err := parameterSchemaDocument(property, depth+1)
		if err != nil {
			return nil, err
		}
		properties[key] = child
	}
	for _, key := range s.Required {
		if _, ok := properties[key]; !ok {
			return nil, fmt.Errorf("required parameter %q has no property schema", key)
		}
	}
	document["properties"], document["additionalProperties"] = properties, false
	if s.MapValues != nil {
		additional, err := parameterSchemaDocument(*s.MapValues, depth+1)
		if err != nil {
			return nil, err
		}
		document["additionalProperties"] = additional
	}
	return document, nil
}

func validateParameterConstraints(s ParameterSchema) error {
	if s.Type == "secret_reference" && (len(s.Properties) > 0 || len(s.Required) > 0 || s.Items != nil || s.MapValues != nil || len(s.Enum) > 0) {
		return fmt.Errorf("secret_reference has fixed name and key fields")
	}
	if err := validateParameterNumericBounds(s); err != nil {
		return err
	}
	if s.MinLength != nil || s.MaxLength != nil {
		if s.Type != "string" {
			return fmt.Errorf("only string schemas may declare length limits")
		}
		if err := validateParameterBounds(s.MinLength, s.MaxLength); err != nil {
			return err
		}
	}
	if s.MinItems != nil || s.MaxItems != nil {
		if s.Type != "array" {
			return fmt.Errorf("only array schemas may declare item limits")
		}
		if err := validateParameterBounds(s.MinItems, s.MaxItems); err != nil {
			return err
		}
	}
	return nil
}

func validateParameterNumericBounds(s ParameterSchema) error {
	if s.Minimum == nil && s.Maximum == nil {
		return nil
	}
	if s.Type != "integer" && s.Type != "number" {
		return fmt.Errorf("only number schemas may declare minimum or maximum")
	}
	if s.Minimum != nil && s.Maximum != nil && *s.Minimum > *s.Maximum {
		return fmt.Errorf("minimum exceeds maximum")
	}
	return nil
}

func validateParameterBounds(minimum, maximum *int) error {
	if minimum != nil && *minimum < 0 || maximum != nil && *maximum < 0 {
		return fmt.Errorf("parameter bounds must be nonnegative")
	}
	if minimum != nil && maximum != nil && *minimum > *maximum {
		return fmt.Errorf("parameter minimum exceeds maximum")
	}
	return nil
}
