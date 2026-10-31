package resource

import (
	"encoding/json"
	contractsopenapi "github.com/opensoha/soha-contracts/openapi"
	"regexp"
	"strings"
	"testing"
)

func TestGatewayPodTargetPatternsCompileInGo(t *testing.T) {
	var document struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]struct {
					Pattern string `json:"pattern"`
				} `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(contractsopenapi.JSON(), &document); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"KubernetesPodArgvToolInput", "KubernetesPodExecutionLookupInput"} {
		pattern := document.Components.Schemas[name].Properties["clusterId"].Pattern
		compiled, err := regexp.Compile(pattern)
		if err != nil || pattern == "" {
			t.Fatalf("%s cannot be registered in the Go Gateway: %v", name, err)
		}
		if !compiled.MatchString("cluster-a") {
			t.Fatal("valid cluster rejected")
		}
		for _, invalid := range []string{"", "with space", "line\nbreak", "nul\x00byte", "c1\u0085control"} {
			if compiled.MatchString(invalid) {
				t.Fatalf("%s accepted a disallowed cluster identifier", name)
			}
		}
	}
	var canonical map[string]any
	if err := json.Unmarshal(contractsopenapi.JSON(), &canonical); err != nil {
		t.Fatal(err)
	}
	components, ok := canonical["components"].(map[string]any)
	if !ok {
		t.Fatal("canonical OpenAPI components are missing")
	}
	schemas, ok := components["schemas"].(map[string]any)
	if !ok {
		t.Fatal("canonical OpenAPI schemas are missing")
	}
	visited := map[string]bool{}
	var visit func(any)
	visit = func(value any) {
		switch item := value.(type) {
		case map[string]any:
			if pattern, ok := item["pattern"].(string); ok {
				if _, err := regexp.Compile(pattern); err != nil {
					t.Fatalf("transitive Pod input regex is not portable to Gateway: %v", err)
				}
			}
			if ref, ok := item["$ref"].(string); ok {
				name := strings.TrimPrefix(ref, "#/components/schemas/")
				if !visited[name] {
					visited[name] = true
					visit(schemas[name])
				}
			}
			for _, child := range item {
				visit(child)
			}
		case []any:
			for _, child := range item {
				visit(child)
			}
		}
	}
	visit(schemas["KubernetesPodArgvToolInput"])
	visit(schemas["KubernetesPodExecutionLookupInput"])
}

func TestPodGatewayResultStartsWithMetadataOnly(t *testing.T) {
	receipt := PodExecutionReceipt{ID: "original", Status: "succeeded", OutputAvailable: true, Output: &PodExecutionOutput{Stdout: "PRIVATE_RAW_STDOUT", Stderr: "PRIVATE_RAW_STDERR"}}
	result := NewPodGatewayResult(receipt)
	raw, err := json.Marshal(result)
	if err != nil || result.Receipt.ID != receipt.ID || result.Receipt.Status != receipt.Status || result.Receipt.OutputAvailable || result.Output != nil || result.OutputState != "unavailable" || strings.Contains(string(raw), "PRIVATE_RAW_") {
		t.Fatal("Gateway metadata constructor copied raw output", err)
	}
	if !receipt.OutputAvailable || receipt.Output.Stdout != "PRIVATE_RAW_STDOUT" || receipt.Output.Stderr != "PRIVATE_RAW_STDERR" {
		t.Fatal("Gateway projection changed original native receipt")
	}
}

func TestPodArgvToolInputKeepsReviewedRequestAndCopiesArguments(t *testing.T) {
	input := PodArgvToolInput{ClusterID: "cluster-a", Namespace: "team-a", PodName: "app.v1", IdempotencyKey: "original-1", Execution: PodArgvInput{ExpectedUID: "reviewed", Container: "main", Argv: []string{"printf", "literal; $(value)"}}}
	actual, err := input.Normalized()
	if err != nil {
		t.Fatal(err)
	}
	if actual.ClusterID != input.ClusterID || actual.Namespace != input.Namespace || actual.PodName != input.PodName || actual.IdempotencyKey != input.IdempotencyKey || actual.Execution.ExpectedUID != "reviewed" || actual.Execution.Argv[1] != input.Execution.Argv[1] || actual.Execution.TimeoutSeconds != PodExecDefaultTimeoutSeconds || actual.Execution.MaxOutputBytes != PodExecDefaultOutputBytes {
		t.Fatal("reviewed intent or runtime defaults changed")
	}
	actual.Execution.Argv[0] = "changed"
	if input.Execution.Argv[0] != "printf" {
		t.Fatal("normalization retained caller argument storage")
	}
	for _, test := range []struct {
		name string
		edit func(*PodArgvToolInput)
	}{
		{"empty cluster", func(v *PodArgvToolInput) { v.ClusterID = "" }},
		{"cluster whitespace", func(v *PodArgvToolInput) { v.ClusterID = "cluster a" }},
		{"cluster BOM", func(v *PodArgvToolInput) { v.ClusterID = "cluster\ufeff" }},
		{"cluster bound", func(v *PodArgvToolInput) { v.ClusterID = strings.Repeat("集", 257) }},
		{"wildcard namespace", func(v *PodArgvToolInput) { v.Namespace = "*" }},
		{"invalid Pod", func(v *PodArgvToolInput) { v.PodName = "app/other" }},
		{"missing key", func(v *PodArgvToolInput) { v.IdempotencyKey = "" }},
		{"key whitespace", func(v *PodArgvToolInput) { v.IdempotencyKey = "old key" }},
		{"key bound", func(v *PodArgvToolInput) { v.IdempotencyKey = strings.Repeat("x", 129) }},
		{"missing UID", func(v *PodArgvToolInput) { v.Execution.ExpectedUID = "" }},
		{"missing argv", func(v *PodArgvToolInput) { v.Execution.Argv = nil }},
		{"bad stdin", func(v *PodArgvToolInput) { v.Execution.StdinBase64 = "not base64" }},
		{"timeout bound", func(v *PodArgvToolInput) { v.Execution.TimeoutSeconds = 301 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			copy := input
			test.edit(&copy)
			if _, err := copy.Normalized(); err == nil {
				t.Fatal("invalid original execution accepted")
			}
		})
	}
}

func TestPodExecutionLookupRequiresExactlyOneOriginalReference(t *testing.T) {
	input := PodExecutionLookupInput{ClusterID: "cluster-a", Namespace: "team-a", PodName: "app", ExecutionID: "11511151-1151-4151-8151-115111511151"}
	if input.Validate() != nil {
		t.Fatal("canonical execution ID rejected")
	}
	input.ExecutionID, input.IdempotencyKey = "", "original-1"
	if input.Validate() != nil {
		t.Fatal("original execution key rejected")
	}
	for _, refs := range [][2]string{{"", ""}, {"11511151-1151-4151-8151-115111511151", "original-1"}, {"11511151-1151-4151-8151-11511151115A", ""}, {"other", ""}, {"", "key\n"}} {
		input.ExecutionID, input.IdempotencyKey = refs[0], refs[1]
		if input.Validate() == nil {
			t.Fatal("invalid or ambiguous original reference accepted")
		}
	}
}
