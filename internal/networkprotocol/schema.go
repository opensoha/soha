package networkprotocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	contractsnetwork "github.com/opensoha/soha-contracts/network"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

const (
	runtimeSchemaURL          = "https://contracts.opensoha.dev/network/network-runtime-protocol.schema.json"
	ingestSchemaURL           = "https://contracts.opensoha.dev/network/network-ingest-event.schema.json"
	radiusAccountingSchemaURL = "https://contracts.opensoha.dev/network/network-radius-accounting.schema.json"
)

type Schemas struct {
	runtime          *jsonschema.Schema
	ingest           *jsonschema.Schema
	radiusAccounting *jsonschema.Schema
}

func CompileSchemas() (*Schemas, error) {
	runtime, err := compileSchema(runtimeSchemaURL, contractsnetwork.RuntimeProtocolSchema())
	if err != nil {
		return nil, fmt.Errorf("compile network runtime schema: %w", err)
	}
	ingest, err := compileSchema(ingestSchemaURL, contractsnetwork.IngestEventSchema())
	if err != nil {
		return nil, fmt.Errorf("compile network ingest schema: %w", err)
	}
	radiusAccounting, err := compileSchema(radiusAccountingSchemaURL, contractsnetwork.RadiusAccountingSchema())
	if err != nil {
		return nil, fmt.Errorf("compile RADIUS accounting schema: %w", err)
	}
	return &Schemas{runtime: runtime, ingest: ingest, radiusAccounting: radiusAccounting}, nil
}

func (s *Schemas) ValidateRuntime(raw []byte) error {
	return validateJSON(s.runtime, raw)
}

func (s *Schemas) ValidateIngest(raw []byte) error {
	return validateJSON(s.ingest, raw)
}

func (s *Schemas) ValidateRadiusAccounting(raw []byte) error {
	return validateJSON(s.radiusAccounting, raw)
}

func compileSchema(location string, raw []byte) (*jsonschema.Schema, error) {
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	compiler := jsonschema.NewCompiler()
	compiler.AssertFormat()
	if err := compiler.AddResource(location, document); err != nil {
		return nil, err
	}
	return compiler.Compile(location)
}

func validateJSON(schema *jsonschema.Schema, raw []byte) error {
	if schema == nil {
		return fmt.Errorf("schema is not initialized")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return fmt.Errorf("decode JSON: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("decode JSON: trailing data")
		}
		return fmt.Errorf("decode JSON: %w", err)
	}
	if err := schema.Validate(value); err != nil {
		return fmt.Errorf("validate JSON schema: %w", err)
	}
	return nil
}
