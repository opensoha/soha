// Package delivery implements the versioned delivery document boundary shared
// by the server and CLI. It does not resolve references or authorize imports.
package delivery

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"go.yaml.in/yaml/v3"
)

const APIVersion = "delivery.soha.io/v1alpha1"
const MaxFileBytes = 1 << 20
const MaxTotalBytes = 2 << 20
const MaxFiles = 100

//go:embed document.schema.json
var documentSchema []byte

func Schema() []byte { return bytes.Clone(documentSchema) }

type Metadata struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName,omitempty"`
	Description string `json:"description,omitempty"`
}

type Document struct {
	APIVersion string         `json:"apiVersion"`
	Kind       string         `json:"kind"`
	Metadata   Metadata       `json:"metadata"`
	Spec       map[string]any `json:"spec"`
}

type Diagnostic struct {
	Path     string `json:"path"`
	Document int    `json:"document"`
	Pointer  string `json:"pointer"`
	Line     int    `json:"line,omitempty"`
	Column   int    `json:"column,omitempty"`
	Code     string `json:"code"`
	Message  string `json:"message"`
}

func (d Diagnostic) Error() string { return fmt.Sprintf("%s%s: %s", d.Path, d.Pointer, d.Message) }

var compiledSchemas = sync.OnceValues(func() (map[string]*jsonschema.Schema, error) {
	resource, err := jsonschema.UnmarshalJSON(bytes.NewReader(documentSchema))
	if err != nil {
		return nil, err
	}
	const location = "https://contracts.opensoha.dev/delivery/document.schema.json"
	compiler := jsonschema.NewCompiler()
	compiler.AssertFormat()
	if err := compiler.AddResource(location, resource); err != nil {
		return nil, err
	}
	schemas := map[string]*jsonschema.Schema{}
	for kind, name := range map[string]string{
		"BuildTemplate": "DeliveryBuildTemplateDocument", "DeploymentTemplate": "DeliveryDeploymentTemplateDocument",
		"WorkflowTemplate": "DeliveryWorkflowTemplateDocument", "Workflow": "DeliveryWorkflowDocument",
	} {
		schemas[kind], err = compiler.Compile(location + "#/$defs/" + name)
		if err != nil {
			return nil, err
		}
	}
	return schemas, nil
})

func validateDocument(path string, value any, locations map[string]*yaml.Node) (Document, []Diagnostic) {
	root, ok := value.(map[string]any)
	if !ok {
		return Document{}, []Diagnostic{diagnostic(path, "", "document_object", "document must be an object", locations[""])}
	}
	if root["apiVersion"] != APIVersion {
		return Document{}, []Diagnostic{diagnostic(path, "/apiVersion", "unsupported_version", "unsupported or missing apiVersion", locations["/apiVersion"])}
	}
	schemas, err := compiledSchemas()
	if err != nil {
		return Document{}, []Diagnostic{diagnostic(path, "", "schema_unavailable", "document schema could not be loaded", nil)}
	}
	kind, _ := root["kind"].(string)
	schema, supported := schemas[kind]
	if !supported {
		return Document{}, []Diagnostic{diagnostic(path, "/kind", "unsupported_kind", "unsupported document kind", locations["/kind"])}
	}
	if err := schema.Validate(root); err != nil {
		return Document{}, schemaDiagnostics(path, err, locations)
	}
	data, err := json.Marshal(root)
	if err != nil {
		return Document{}, []Diagnostic{diagnostic(path, "", "invalid_json", "document must contain JSON values", nil)}
	}
	var document Document
	if err := json.Unmarshal(data, &document); err != nil {
		return Document{}, []Diagnostic{diagnostic(path, "", "invalid_document", "document fields are invalid", nil)}
	}
	normalizeDocument(&document)
	return document, nil
}

func schemaDiagnostics(path string, err error, locations map[string]*yaml.Node) []Diagnostic {
	var invalid *jsonschema.ValidationError
	if !errors.As(err, &invalid) {
		return []Diagnostic{diagnostic(path, "", "schema_invalid", "document does not satisfy its schema", nil)}
	}
	var results []Diagnostic
	var visit func(*jsonschema.ValidationError)
	visit = func(item *jsonschema.ValidationError) {
		if len(results) >= 20 {
			return
		}
		if len(item.Causes) > 0 {
			for _, cause := range item.Causes {
				visit(cause)
			}
			return
		}
		pointer := ""
		for _, part := range item.InstanceLocation {
			pointer += "/" + escapePointer(part)
		}
		keyword := strings.Join(item.ErrorKind.KeywordPath(), ".")
		// The library's formatted errors can contain raw values. Keep diagnostics
		// at the field/constraint boundary, including for legacy executor config.
		results = append(results, diagnostic(path, pointer, "schema_"+keyword, "field does not satisfy "+keyword, locations[pointer]))
	}
	visit(invalid)
	return results
}

func normalizeDocument(document *Document) {
	spec := document.Spec
	if document.Kind != "Workflow" {
		setDefault(spec, "enabled", true)
		if document.Metadata.DisplayName == "" {
			document.Metadata.DisplayName = document.Metadata.Name
		}
	}
	switch document.Kind {
	case "BuildTemplate":
		setDefault(spec, "builderKind", "custom")
		setDefault(spec, "variableSchema", map[string]any{})
		setDefault(spec, "defaultVariables", map[string]any{})
	case "WorkflowTemplate":
		setDefault(spec, "category", "release")
		definition, _ := spec["definition"].(map[string]any) // Schema-validated before normalization.
		if definition["mode"] != "delivery_batch" {
			setDefault(definition, "mode", "release_dag")
		}
	case "Workflow":
		definition, _ := spec["definition"].(map[string]any) // Schema-validated before normalization.
		// Template defaults are resolved by the server against the pinned version.
		if definition["workflowTemplateId"] == nil {
			setDefault(definition, "mode", "service_serial")
			setDefault(definition, "stopOnFailure", true)
			setDefault(definition, "maxConcurrency", float64(4))
		}
	}
}

func setDefault(object map[string]any, key string, value any) {
	if _, exists := object[key]; !exists {
		object[key] = value
	}
}

func (d Document) Value() map[string]any {
	metadata := map[string]any{"name": d.Metadata.Name}
	if d.Metadata.DisplayName != "" {
		metadata["displayName"] = d.Metadata.DisplayName
	}
	if d.Metadata.Description != "" {
		metadata["description"] = d.Metadata.Description
	}
	return map[string]any{"apiVersion": d.APIVersion, "kind": d.Kind, "metadata": metadata, "spec": d.Spec}
}

func (d Document) NormalizedSpecDigest() (string, error) {
	data, err := CanonicalJSON(map[string]any{"apiVersion": d.APIVersion, "kind": d.Kind, "spec": d.Spec})
	if err != nil {
		return "", err
	}
	return Digest(data), nil
}

func (d Document) Encode(format string) ([]byte, error) {
	switch format {
	case "json":
		return json.MarshalIndent(d, "", "  ")
	case "yaml":
		return yaml.Marshal(d.Value())
	default:
		return nil, fmt.Errorf("unsupported document format")
	}
}
