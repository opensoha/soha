package aigateway

import (
	"encoding/json"
	"math"
	"testing"

	domainai "github.com/opensoha/soha/internal/domain/aigateway"
)

func TestApprovalMatchesFrozenJSONNumbersAcrossPersistence(t *testing.T) {
	// Approval JSON is decoded by the SQL repository, while a reopened sealed
	// CapabilityTask preserves json.Number. Their Go types are not authority.
	input := map[string]any{"replicas": json.Number("2"), "preconditions": map[string]any{"expectedUid": "original", "expectedResourceVersion": "388"}}
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var stored map[string]any
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatal(err)
	}
	request := domainai.ApprovalRequest{ToolName: "k8s.workloads.conditional.action", ToolInput: stored}
	if !approvalInputMatchesCall(request, input) {
		t.Fatal("same frozen JSON rejected after approval persistence")
	}
	for name, changed := range map[string]map[string]any{
		"replicas":        {"replicas": json.Number("3"), "preconditions": input["preconditions"]},
		"string":          {"replicas": "2", "preconditions": input["preconditions"]},
		"null":            {"replicas": nil, "preconditions": input["preconditions"]},
		"uid":             {"replicas": json.Number("2"), "preconditions": map[string]any{"expectedUid": "replacement", "expectedResourceVersion": "388"}},
		"revision":        {"replicas": json.Number("2"), "preconditions": map[string]any{"expectedUid": "original", "expectedResourceVersion": "389"}},
		"invalid-number":  {"replicas": json.Number("invalid"), "preconditions": input["preconditions"]},
		"non-json-number": {"replicas": math.Inf(1), "preconditions": input["preconditions"]},
	} {
		t.Run(name, func(t *testing.T) {
			if approvalInputMatchesCall(request, changed) {
				t.Fatal("changed or invalid approval input accepted")
			}
		})
	}
}

func TestApprovalDoesNotEquateRoundedLargeJSONNumber(t *testing.T) {
	request := domainai.ApprovalRequest{ToolName: "example.write", ToolInput: map[string]any{"value": float64(9007199254740992)}}
	if approvalInputMatchesCall(request, map[string]any{"value": json.Number("9007199254740993")}) {
		t.Fatal("different large integer accepted after floating point rounding")
	}
}
