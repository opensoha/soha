package aigateway

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestWorkbenchImagesSurviveAllModelProtocols(t *testing.T) {
	for _, endpoint := range []string{"chat/completions", "responses", "messages"} {
		for _, withTools := range []bool{false, true} {
			input := WorkbenchRelayRequest{Messages: []WorkbenchRelayMessage{{Role: "user", Content: "Describe this", Images: []string{"data:image/png;base64,aGVsbG8="}}}}
			if withTools {
				input.Tools = []WorkbenchTool{{Name: "lookup", Parameters: map[string]any{"type": "object"}}}
			}
			body, err := workbenchRelayModelBody(endpoint, "fixture", input, true, false)
			if err != nil {
				t.Fatal(err)
			}
			var payload map[string]any
			if json.Unmarshal(body, &payload) != nil {
				t.Fatal("invalid wire JSON")
			}
			key := "messages"
			if endpoint == "responses" {
				key = "input"
			}
			messages, ok := payload[key].([]any)
			if !ok || len(messages) != 1 {
				t.Fatalf("lost user message: %s", body)
			}
			message, ok := messages[0].(map[string]any)
			if !ok {
				t.Fatalf("invalid user message: %s", body)
			}
			blocks, ok := message["content"].([]any)
			if !ok || len(blocks) != 2 {
				t.Fatalf("lost image/text blocks: %s", body)
			}
			image, ok := blocks[1].(map[string]any)
			if !ok {
				t.Fatalf("invalid image block: %s", body)
			}
			assertWorkbenchImageProtocol(t, endpoint, image, input.Messages[0].Images[0])
		}
	}
}

func assertWorkbenchImageProtocol(t *testing.T, endpoint string, image map[string]any, expected string) {
	t.Helper()
	switch endpoint {
	case "responses":
		if image["type"] != "input_image" || image["image_url"] != expected {
			t.Fatal("Responses image missing")
		}
	case "messages":
		source, ok := image["source"].(map[string]any)
		if !ok || image["type"] != "image" || source["data"] != "aGVsbG8=" {
			t.Fatal("Anthropic image missing")
		}
	default:
		url, ok := image["image_url"].(map[string]any)
		if !ok || image["type"] != "image_url" || url["url"] != expected {
			t.Fatal("Chat image missing")
		}
	}
}

func TestWorkbenchToolRoundTrip(t *testing.T) {
	call := WorkbenchToolCall{ID: "call-1", Name: "knowledge_search", Arguments: json.RawMessage(`{"query":"lease"}`)}
	input := WorkbenchRelayRequest{Tools: []WorkbenchTool{{Name: call.Name, Parameters: map[string]any{"type": "object"}}}, Messages: []WorkbenchRelayMessage{{Role: "user", Content: "Find lease rules"}, {Role: "assistant", ToolCalls: []WorkbenchToolCall{call}}, {Role: "tool", ToolCallID: call.ID, Content: "Evidence"}}}
	responses := map[string]string{
		"chat/completions": `{"choices":[{"message":{"content":null,"tool_calls":[{"id":"call-1","type":"function","function":{"name":"knowledge_search","arguments":"{\"query\":\"lease\"}"}}]}}]}`,
		"messages":         `{"content":[{"type":"tool_use","id":"call-1","name":"knowledge_search","input":{"query":"lease"}}]}`,
		"responses":        `{"output":[{"type":"function_call","call_id":"call-1","name":"knowledge_search","arguments":"{\"query\":\"lease\"}"}]}`,
	}
	for endpoint, response := range responses {
		t.Run(endpoint, func(t *testing.T) {
			body, err := workbenchRelayModelBody(endpoint, "test", input, false, false)
			if err != nil {
				t.Fatal(err)
			}
			var payload map[string]any
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatal(err)
			}
			tools, ok := payload["tools"].([]any)
			if !ok || len(tools) != 1 {
				t.Fatalf("missing tools: %s", body)
			}
			text, calls, err := workbenchRelayModelOutput(endpoint, []byte(response))
			if err != nil || text != "" || len(calls) != 1 || calls[0].ID != call.ID || calls[0].Name != call.Name || string(calls[0].Arguments) != string(call.Arguments) {
				t.Fatalf("output = %q %#v %v", text, calls, err)
			}
		})
	}
}

func TestWorkbenchOutputTokenLimitUsesProviderProtocol(t *testing.T) {
	for _, test := range []struct {
		endpoint, key string
		reasoning     bool
	}{
		{"chat/completions", "max_tokens", false}, {"chat/completions", "max_completion_tokens", true},
		{"messages", "max_tokens", false}, {"responses", "max_output_tokens", false},
	} {
		t.Run(test.endpoint+test.key, func(t *testing.T) {
			body, err := workbenchRelayModelBody(test.endpoint, "fixture", WorkbenchRelayRequest{MaxOutputTokens: 37}, true, test.reasoning)
			if err != nil {
				t.Fatal(err)
			}
			var payload map[string]any
			if err := json.Unmarshal(body, &payload); err != nil || payload[test.key] != float64(37) {
				t.Fatalf("token cap lost: %s %v", body, err)
			}
		})
	}
	for _, limit := range []int{-1, 32769} {
		if _, err := workbenchRelayModelBody("messages", "fixture", WorkbenchRelayRequest{MaxOutputTokens: limit}, false, false); err == nil {
			t.Fatalf("invalid cap accepted: %d", limit)
		}
	}
}

func TestWorkbenchStreamingToolArgumentsRequireCompleteObject(t *testing.T) {
	prefix := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-1","function":{"name":"knowledge_search","arguments":"{\"query\":"}}]}}]}` + "\n\n"
	suffix := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"lease\"}"}}]}}]}` + "\n\n" + `data: {"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}` + "\n\n" + "data: [DONE]\n\n"
	calls, usage, err := workbenchRelayStreamOutput("chat/completions", []byte(prefix+suffix))
	if err != nil || len(calls) != 1 || usage["totalTokens"] != 15 || string(calls[0].Arguments) != `{"query":"lease"}` {
		t.Fatalf("calls=%#v usage=%#v err=%v", calls, usage, err)
	}
	if _, _, err = workbenchRelayStreamOutput("chat/completions", []byte(prefix)); err == nil {
		t.Fatal("accepted truncated tool arguments")
	}
	if err = validateWorkbenchToolCalls([]WorkbenchToolCall{calls[0], calls[0]}); err == nil {
		t.Fatal("accepted duplicate call identity")
	}
	if _, _, err = workbenchRelayModelOutput("chat/completions", []byte(`{"choices":[{"message":{"tool_calls":[{"id":"x","function":{"name":"a","arguments":"null"}}]}}]}`)); err == nil {
		t.Fatal("accepted null tool arguments")
	}
}

func TestWorkbenchStreamingAnthropicAndResponses(t *testing.T) {
	tests := []struct {
		endpoint, frames, arguments string
		tokens                      float64
	}{
		{"messages", `{"type":"message_start","message":{"usage":{"input_tokens":12}}}` + "\n" + `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"a","name":"query","input":{}}}` + "\n" + `{"type":"message_delta","usage":{"output_tokens":4}}`, `{}`, 16},
		{"messages", `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"a","name":"query","input":{}}}` + "\n" + `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"key\":"}}` + "\n" + `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"\"value\"}"}}`, `{"key":"value"}`, 0},
		{"responses", `{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","call_id":"a","name":"query","arguments":""}}` + "\n" + `{"type":"response.function_call_arguments.delta","output_index":0,"delta":"{\"key\":"}` + "\n" + `{"type":"response.function_call_arguments.delta","output_index":0,"delta":"\"value\"}"}` + "\n" + `{"type":"response.completed","response":{"usage":{"input_tokens":9,"output_tokens":3,"total_tokens":12}}}`, `{"key":"value"}`, 12},
	}
	for _, tt := range tests {
		t.Run(tt.endpoint+tt.arguments, func(t *testing.T) {
			var body string
			for _, frame := range strings.Split(tt.frames, "\n") {
				body += "data: " + frame + "\n\n"
			}
			if tt.endpoint == "messages" {
				body += "data: {\"type\":\"message_stop\"}\n\n"
			}
			calls, usage, err := workbenchRelayStreamOutput(tt.endpoint, []byte(body))
			if err != nil || len(calls) != 1 || string(calls[0].Arguments) != tt.arguments || usage["totalTokens"] != tt.tokens {
				t.Fatalf("calls=%v usage=%v err=%v", calls, usage, err)
			}
		})
	}
}
