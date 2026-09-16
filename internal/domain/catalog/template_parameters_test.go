package catalog

import (
	"strings"
	"testing"
)

func TestTemplateParametersValidateAndPreserveValues(t *testing.T) {
	schema := ParameterSchema{Type: "object", Required: []string{"replicas", "token"}, Properties: map[string]ParameterSchema{
		"replicas": {Type: "integer"}, "enabled": {Type: "boolean"}, "token": {Type: "secret_reference"},
		"args":   {Type: "array", Items: &ParameterSchema{Type: "string"}},
		"labels": {Type: "object", MapValues: &ParameterSchema{Type: "string"}},
	}}
	defaults := map[string]any{"replicas": 1, "enabled": true, "args": []string{"default"}}
	parameters := map[string]any{"token": map[string]any{"name": "app-secret", "key": "token"}, "labels": map[string]any{"app": "api"}}
	result, err := schema.ResolveParameters(defaults, parameters, map[string]any{"replicas": 0, "enabled": false, "args": []any{}}, []string{"replicas", "enabled", "args"})
	if err != nil {
		t.Fatal(err)
	}
	args, ok := result["args"].([]any)
	if !ok || result["replicas"] != float64(0) || result["enabled"] != false || len(args) != 0 {
		t.Fatalf("explicit empty values were lost: %#v", result)
	}
	labels, ok := result["labels"].(map[string]any)
	if !ok {
		t.Fatal("labels are not an object")
	}
	labels["app"] = "changed"
	original, ok := parameters["labels"].(map[string]any)
	if !ok || original["app"] != "api" {
		t.Fatal("resolving parameters mutated the caller")
	}
	for name, input := range map[string]map[string]any{
		"unknown": {"unexpected": true}, "wrong type": {"replicas": "1"},
		"plaintext credential":   {"token": "do-not-print-this-secret"},
		"cross namespace secret": {"token": map[string]any{"name": "secret", "key": "token", "namespace": "production"}},
		"invalid nested map":     {"labels": map[string]any{"app": true}},
	} {
		t.Run(name, func(t *testing.T) {
			validInput := map[string]any{"token": parameters["token"]}
			for key, value := range input {
				validInput[key] = value
			}
			_, err := schema.ResolveParameters(defaults, validInput, nil, nil)
			if err == nil || strings.Contains(err.Error(), "do-not-print-this-secret") {
				t.Fatalf("expected a sanitized validation failure: %v", err)
			}
		})
	}
	if _, err := schema.ResolveParameters(defaults, parameters, map[string]any{"token": parameters["token"]}, nil); err == nil {
		t.Fatal("environment override bypassed the allowlist")
	}
}

func TestTemplateParameterSchemaRejectsInvalidConstraints(t *testing.T) {
	negative, low, high := -1, 1, 2
	minimum, maximum := 3.0, 1.0
	for name, schema := range map[string]ParameterSchema{
		"secret shape override":  {Type: "secret_reference", Properties: map[string]ParameterSchema{"password": {Type: "string"}}},
		"wrong numeric type":     {Type: "string", Minimum: &minimum},
		"reversed numeric range": {Type: "number", Minimum: &minimum, Maximum: &maximum},
		"negative length":        {Type: "string", MinLength: &negative},
		"wrong length type":      {Type: "boolean", MinLength: &low},
		"reversed item range":    {Type: "array", Items: &ParameterSchema{Type: "string"}, MinItems: &high, MaxItems: &low},
	} {
		t.Run(name, func(t *testing.T) {
			root := ParameterSchema{Type: "object", Properties: map[string]ParameterSchema{"value": schema}}
			if _, err := root.Compile(); err == nil {
				t.Fatal("accepted invalid parameter constraints")
			}
		})
	}
}
