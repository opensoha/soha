package delivery

import (
	"context"
	"encoding/json"
	"io/fs"
	"math"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"
)

const recipeYAML = `apiVersion: delivery.soha.io/v1alpha1
kind: WorkflowTemplate
metadata:
  name: build-release
  displayName: 联合构建发布
spec:
  definition:
    mode: delivery_batch
    schemaVersion: 1
    stages: [build, plan, deploy, health]
    executionMode: build_all_then_deploy
    stopOnFailure: true
    maxConcurrency: 4
`

func TestDocumentRoundTrip(t *testing.T) {
	fixtures := os.DirFS("../fixtures/openapi/DeliveryDocument/valid")
	files, err := fs.Glob(fixtures, "*.json")
	if err != nil || len(files) < 4 {
		t.Fatalf("expected fixtures for all four kinds: %v", err)
	}
	for _, file := range files {
		t.Run(file, func(t *testing.T) {
			data, err := fs.ReadFile(fixtures, file)
			if err != nil {
				t.Fatal(err)
			}
			first, invalid := Parse(file, data)
			if len(invalid) != 0 {
				t.Fatalf("parse: %+v", invalid)
			}
			for _, format := range []string{"yaml", "json"} {
				encoded, err := first.Encode(format)
				if err != nil {
					t.Fatal(err)
				}
				second, invalid := Parse("roundtrip."+format, encoded)
				if len(invalid) != 0 || !reflect.DeepEqual(first, second) {
					t.Fatalf("lossy %s roundtrip: %+v", format, invalid)
				}
			}
		})
	}
}

func TestEquivalentDocumentDigest(t *testing.T) {
	first, invalid := Parse("recipe.yaml", []byte(recipeYAML))
	if len(invalid) != 0 {
		t.Fatalf("parse: %+v", invalid)
	}
	data, _ := first.Encode("json")
	second, invalid := Parse("recipe.json", data)
	if len(invalid) != 0 {
		t.Fatal(invalid)
	}
	a, err := first.NormalizedSpecDigest()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := second.NormalizedSpecDigest()
	if a != b || Digest([]byte(recipeYAML)) == Digest(data) {
		t.Fatal("semantic and source digest scopes are incorrect")
	}
	second.Metadata.Description = "metadata is outside the spec digest"
	b, _ = second.NormalizedSpecDigest()
	if a != b {
		t.Fatal("metadata changed the spec digest")
	}
	second.Spec["enabled"] = false
	b, _ = second.NormalizedSpecDigest()
	if a == b {
		t.Fatal("definition change did not change the spec digest")
	}
}

func TestDocumentRejectsAmbiguousAndUnsafeInput(t *testing.T) {
	for _, test := range []struct{ name, content, code string }{
		{"duplicate", strings.Replace(recipeYAML, "  name: build-release", "  name: first\n  name: second", 1), "duplicate_key"},
		{"alias", strings.Replace(recipeYAML, "  name: build-release", "  name: &name build-release\n  description: *name", 1), "yaml_feature"},
		{"merge", strings.Replace(recipeYAML, "  name: build-release", "  <<: {name: build-release}", 1), "non_string_key"},
		{"tag", strings.Replace(recipeYAML, "name: build-release", "name: !!str build-release", 1), "yaml_feature"},
		{"key", strings.Replace(recipeYAML, "name: build-release", "7: build-release", 1), "non_string_key"},
		{"infinity", strings.Replace(recipeYAML, "maxConcurrency: 4", "maxConcurrency: .inf", 1), "invalid_number"},
		{"unsafe integer", strings.Replace(recipeYAML, "maxConcurrency: 4", "maxConcurrency: 9007199254740993", 1), "number_precision"},
		{"rounded unsafe boundary", strings.Replace(recipeYAML, "maxConcurrency: 4", "maxConcurrency: 9007199254740991.1", 1), "number_precision"},
		{"negative rounded unsafe boundary", strings.Replace(recipeYAML, "maxConcurrency: 4", "maxConcurrency: -9007199254740991.1", 1), "number_precision"},
		{"underflow", strings.Replace(recipeYAML, "maxConcurrency: 4", "maxConcurrency: 1e-9999", 1), "number_precision"},
		{"hex", strings.Replace(recipeYAML, "maxConcurrency: 4", "maxConcurrency: 0x4", 1), "invalid_number"},
		{"version", strings.Replace(recipeYAML, APIVersion, "delivery.soha.io/v9", 1), "unsupported_version"},
		{"kind", strings.Replace(recipeYAML, "kind: WorkflowTemplate", "kind: Deployment", 1), "unsupported_kind"},
		{"blueprint", strings.Replace(recipeYAML, "kind: WorkflowTemplate", "kind: Blueprint", 1), "unsupported_kind"},
		{"two documents", recipeYAML + "---\n" + recipeYAML, "multiple_documents"},
		{"server field", strings.Replace(recipeYAML, "  name: build-release", "  name: build-release\n  publicationState: published", 1), "schema_"},
		{"unknown field", strings.Replace(recipeYAML, "maxConcurrency: 4", "maxConcurrency: 4\n    typo: true", 1), "schema_"},
		{"invalid type", strings.Replace(recipeYAML, "maxConcurrency: 4", "maxConcurrency: private-value", 1), "schema_"},
		{"surrogate", `{"apiVersion":"\ud800"}`, "syntax"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, invalid := Parse("input.yaml", []byte(test.content))
			if len(invalid) == 0 || !strings.HasPrefix(invalid[0].Code, test.code) {
				t.Fatalf("expected %s, got %+v", test.code, invalid)
			}
			for _, item := range invalid {
				if strings.Contains(item.Message, "private-value") {
					t.Fatal("diagnostic disclosed input content")
				}
			}
		})
	}
	_, invalid := Parse("input.yaml", []byte(strings.Replace(recipeYAML, "  name: build-release", "  name: first\n  name: second", 1)))
	if invalid[0].Pointer != "/metadata/name" || invalid[0].Line != 5 || invalid[0].Column != 3 {
		t.Fatalf("missing duplicate key location: %+v", invalid)
	}
	for _, path := range []string{"../input.yaml", "/input.yaml", "a/../input.yaml", "a\\input.yaml", "a//input.yaml", "C:input.yaml"} {
		if _, invalid := Parse(path, []byte(recipeYAML)); len(invalid) == 0 || invalid[0].Code != "invalid_path" {
			t.Fatalf("accepted invalid path %q", path)
		}
	}
	for _, input := range [][]byte{[]byte(strings.Repeat(" ", MaxFileBytes+1)), {0xff}, []byte("x: " + strings.Repeat("[", 66) + "0" + strings.Repeat("]", 66))} {
		if _, invalid := Parse("input.yaml", input); len(invalid) == 0 {
			t.Fatal("accepted oversized, invalid UTF-8 or deeply nested document")
		}
	}
}

func TestCanonicalJSONVectors(t *testing.T) {
	for _, test := range []struct{ input, want string }{
		{`{"z":-0.0,"a":1e-7,"b":0.000001,"c":333333333.33333329}`, `{"a":1e-7,"b":0.000001,"c":333333333.3333333,"z":0}`},
		{`{"\ufb33":"letter","😀":"emoji","€":"euro","\r":"return","1":"one","ö":"o","\u0080":"control"}`, `{"\r":"return","1":"one","` + "\u0080" + `":"control","ö":"o","€":"euro","😀":"emoji","דּ":"letter"}`},
		{`{"string":"<>&\u2028\u2029\u000f\b\t\n\f\r\\\"/"}`, "{\"string\":\"<>&\u2028\u2029\\u000f\\b\\t\\n\\f\\r\\\\\\\"/\"}"},
	} {
		var value any
		if err := json.Unmarshal([]byte(test.input), &value); err != nil {
			t.Fatal(err)
		}
		encoded, err := CanonicalJSON(value)
		if err != nil || string(encoded) != test.want {
			t.Fatalf("canonical JSON = %q, want %q; %v", encoded, test.want, err)
		}
	}
	for _, value := range []any{math.NaN(), math.Inf(1), string([]byte{0xff})} {
		if _, err := CanonicalJSON(value); err == nil {
			t.Fatal("accepted non-I-JSON value")
		}
	}
}

func TestCanonicalJSONMatchesECMAScript(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node.js is required for the contracts npm test cross-language gate")
	}
	input := `{"numbers":[-0.0,1e-6,1e-7,1e20,1e21,1e23,5e-324,1.7976931348623157e308,333333333.33333329],"keys":{"10":"ten","2":"two","\ufb33":"letter","😀":"emoji","€":"euro","\r":"return","ö":"o","\u0080":"control"},"string":"<>&\u2028\u2029\u000f\b\t\n\f\r\\\"/"}`
	var value any
	if err := json.Unmarshal([]byte(input), &value); err != nil {
		t.Fatal(err)
	}
	got, err := CanonicalJSON(value)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "node", "-e", `
const canonical = value => Array.isArray(value) ? '[' + value.map(canonical).join(',') + ']'
  : value !== null && typeof value === 'object' ? '{' + Object.keys(value).sort().map(key => JSON.stringify(key) + ':' + canonical(value[key])).join(',') + '}'
  : JSON.stringify(value);
process.stdout.write(canonical(JSON.parse(require('node:fs').readFileSync(0, 'utf8'))));`)
	command.Stdin = strings.NewReader(input)
	want, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("Go canonical JSON differs from ECMAScript: got %q, want %q", got, want)
	}
}
