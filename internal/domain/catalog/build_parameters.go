package catalog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

var buildVariableName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)
var buildTemplateToken = regexp.MustCompile(`\{\{([A-Za-z_][A-Za-z0-9_]*)\}\}`)
var buildLiteral = regexp.MustCompile(`^[A-Za-z0-9_./:@+,=-]*$`)

// BuildVariables adapts the existing flat variable catalog to the same bounded
// validator used by deployment templates. Runtime values never extend the schema.
func BuildVariables(schema, defaults, parameters map[string]any, requireAll bool) (map[string]any, error) {
	encoded, err := json.Marshal(schema)
	if err != nil || len(encoded) > 64<<10 || len(schema) > 64 {
		return nil, fmt.Errorf("build variable schema exceeds limits")
	}
	root := ParameterSchema{Type: "object", Properties: map[string]ParameterSchema{}}
	for name, raw := range schema {
		if !buildVariableName.MatchString(name) || name == "IMAGE_REF" || name == "CONTEXT_DIR" || name == "DOCKERFILE_PATH" || strings.HasPrefix(name, "SOHA_") {
			return nil, fmt.Errorf("invalid or reserved build variable name")
		}
		spec, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("build variable schema must contain property objects")
		}
		field, err := buildVariableSchema(spec)
		if err != nil {
			return nil, err
		}
		root.Properties[name] = field
		if required, _ := spec["required"].(bool); required && requireAll {
			root.Required = append(root.Required, name)
		}
	}
	return root.ResolveParameters(defaults, parameters, nil, nil)
}

func buildVariableSchema(spec map[string]any) (ParameterSchema, error) {
	property := make(map[string]any, len(spec))
	for key, value := range spec {
		if key != "required" && key != "title" && key != "label" {
			property[key] = value
		}
	}
	data, err := json.Marshal(property)
	if err != nil {
		return ParameterSchema{}, fmt.Errorf("invalid build variable schema")
	}
	var field ParameterSchema
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&field); err != nil {
		return ParameterSchema{}, fmt.Errorf("invalid build variable schema")
	}
	switch field.Type {
	case "string", "number", "integer", "boolean":
	default:
		return ParameterSchema{}, fmt.Errorf("build variables must declare a scalar type; credentials use secret leases")
	}
	return field, nil
}

// RenderBuildLiterals is intentionally limited to inert tokens. Arbitrary text is
// available to commands through quoted SOHA_BUILD_<name> environment variables.
// This also prevents Dockerfile values from introducing instructions or RUN code.
func RenderBuildLiterals(content string, values map[string]any) (string, error) {
	var invalid bool
	rendered := buildTemplateToken.ReplaceAllStringFunc(content, func(token string) string {
		name := token[2 : len(token)-2]
		value, exists := values[name]
		if !exists || !buildLiteral.MatchString(fmt.Sprint(value)) {
			invalid = true
			return token
		}
		return fmt.Sprint(value)
	})
	if invalid {
		return "", fmt.Errorf("template placeholders require declared literal values; use a quoted SOHA_BUILD variable for free text")
	}
	return rendered, nil
}
