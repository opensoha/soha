package aigateway

import (
	"bytes"
	"slices"
	"sync"

	contractsopenapi "github.com/opensoha/soha-contracts/openapi"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
)

// The embedded public contract is immutable for the lifetime of this binary.
var capabilityTaskInputSchema = sync.OnceValues(func() (*jsonschema.Schema, error) {
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(contractsopenapi.JSON()))
	if err != nil {
		return nil, err
	}
	const location = "https://schemas.opensoha.invalid/platform-contracts"
	compiler := jsonschema.NewCompiler()
	compiler.UseLoader(capabilitySchemaLoader{})
	if err := compiler.AddResource(location, document); err != nil {
		return nil, err
	}
	return compiler.Compile(location + "#/components/schemas/CapabilityTaskInput")
})

func validPlannedCapabilityInput(tool domainaigateway.ToolCapability, step domainaigateway.CapabilityPlanStep) bool {
	if len(step.Bindings) == 0 {
		return validateCapabilityInput(tool, step.Call.Input) == nil
	}
	schema, err := compileCapabilityInputSchema(tool.InputSchema)
	if err != nil {
		return false
	}
	value, err := capabilityJSON(step.Call.Input)
	if err != nil {
		return false
	}
	err = schema.Validate(value)
	if err == nil {
		return true
	}
	validation, ok := err.(*jsonschema.ValidationError)
	return ok && capabilityErrorAwaitsBinding(validation, step.Bindings)
}

// Only missing properties supplied by a binding can be deferred. Validate all
// literal values now; the unmodified schema validates the complete input again
// after binding. Do not synthesize values or weaken the registered schema.
func capabilityErrorAwaitsBinding(err *jsonschema.ValidationError, bindings []domainaigateway.CapabilityInputBinding) bool {
	switch err.ErrorKind.(type) {
	case *kind.OneOf, *kind.AnyOf:
		// At least one alternative must be repairable solely by declared bindings.
		for _, cause := range err.Causes {
			if capabilityErrorAwaitsBinding(cause, bindings) {
				return true
			}
		}
		return false
	}
	if len(err.Causes) > 0 {
		for _, cause := range err.Causes {
			if !capabilityErrorAwaitsBinding(cause, bindings) {
				return false
			}
		}
		return true
	}
	required, ok := err.ErrorKind.(*kind.Required)
	if !ok {
		return false
	}
	for _, missing := range required.Missing {
		path := append(slices.Clone(err.InstanceLocation), missing)
		covered := false
		for _, binding := range bindings {
			tokens, tokenErr := capabilityPointerTokens(binding.InputPath)
			if tokenErr == nil && len(tokens) >= len(path) && slices.Equal(tokens[:len(path)], path) {
				covered = true
				break
			}
		}
		if !covered {
			return false
		}
	}
	return true
}
