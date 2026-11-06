package sohaapi

import (
	"encoding/json"
	"strings"
	"testing"
)

// These assignments protect source compatibility for existing SDK consumers.
var (
	_ string = NetworkProxyConfigurationInput{}.Content
	_ string = UpdateNetworkProxyConfigurationJSONRequestBody{}.Content
)

func TestProxyPartialInputKeepsOmissionSeparateFromEmptyContent(t *testing.T) {
	var input NetworkProxyConfigurationPatchInput
	if err := json.Unmarshal([]byte(`{"expectedRevision":4,"enabled":false,"clearBinding":true}`), &input); err != nil {
		t.Fatal(err)
	}
	if input.Content != nil || !input.ClearBinding {
		t.Fatalf("partial input = %#v", input)
	}
	payload, err := json.Marshal(input)
	if err != nil || strings.Contains(string(payload), `"content"`) {
		t.Fatalf("omitted content encoded as %s, %v", payload, err)
	}
	if err := json.Unmarshal([]byte(`{"expectedRevision":4,"enabled":true,"content":""}`), &input); err != nil {
		t.Fatal(err)
	}
	if input.Content == nil || *input.Content != "" {
		t.Fatal("explicit empty content lost its presence for server-side rejection")
	}
	legacy := UpdateNetworkProxyConfigurationJSONRequestBody{ExpectedRevision: 4, Enabled: true, Content: "mode: rule"}
	payload, err = json.Marshal(legacy)
	if err != nil || !strings.Contains(string(payload), `"content":"mode: rule"`) {
		t.Fatalf("legacy full input = %s, %v", payload, err)
	}
}
