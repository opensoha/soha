package aigateway

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/opensoha/soha/internal/platform/apperrors"
)

type WorkbenchTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type WorkbenchToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func workbenchRelayModelBody(endpoint, model string, input WorkbenchRelayRequest, stream, reasoning bool) ([]byte, error) {
	body, err := workbenchRelayRequestBody(endpoint, model, input.Messages, stream, input.ReasoningEffort, reasoning)
	if err != nil {
		return nil, err
	}
	if len(input.Tools) == 0 && !workbenchHasToolHistory(input.Messages) && input.MaxOutputTokens == 0 {
		return body, nil
	}
	if len(input.Tools) > 64 {
		return nil, fmt.Errorf("%w: too many model tools", apperrors.ErrInvalidArgument)
	}
	var payload map[string]any
	if err = json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	if err := setWorkbenchOutputTokenLimit(payload, endpoint, input.MaxOutputTokens, reasoning); err != nil {
		return nil, err
	}
	tools := make([]map[string]any, 0, len(input.Tools))
	for _, tool := range input.Tools {
		if tool.Name == "" || len(tool.Name) > 64 || tool.Parameters["type"] != "object" {
			return nil, fmt.Errorf("%w: invalid model tool definition", apperrors.ErrInvalidArgument)
		}
		switch endpoint {
		case "messages":
			tools = append(tools, map[string]any{"name": tool.Name, "description": tool.Description, "input_schema": tool.Parameters})
		case "responses":
			tools = append(tools, map[string]any{"type": "function", "name": tool.Name, "description": tool.Description, "parameters": tool.Parameters})
		default:
			tools = append(tools, map[string]any{"type": "function", "function": tool})
		}
	}
	if len(tools) > 0 {
		payload["tools"] = tools
	}
	switch endpoint {
	case "chat/completions":
		payload["messages"] = workbenchToolChatMessages(input.Messages)
		if stream {
			payload["stream_options"] = map[string]bool{"include_usage": true}
		}
	case "messages":
		payload["messages"] = workbenchToolAnthropicMessages(input.Messages)
	case "responses":
		payload["input"] = workbenchToolResponsesInput(input.Messages)
	}
	return json.Marshal(payload)
}

func setWorkbenchOutputTokenLimit(payload map[string]any, endpoint string, limit int, reasoning bool) error {
	if limit == 0 {
		return nil
	}
	if limit < 0 || limit > 32768 {
		return fmt.Errorf("%w: invalid output token limit", apperrors.ErrInvalidArgument)
	}
	payload[workbenchOutputTokenField(endpoint, reasoning)] = limit
	return nil
}

func workbenchOutputTokenField(endpoint string, reasoning bool) string {
	if endpoint == "responses" {
		return "max_output_tokens"
	}
	if endpoint == "chat/completions" && reasoning {
		return "max_completion_tokens"
	}
	return "max_tokens"
}

func workbenchHasToolHistory(messages []WorkbenchRelayMessage) bool {
	for _, message := range messages {
		if message.Role == "tool" || len(message.ToolCalls) > 0 {
			return true
		}
	}
	return false
}

func workbenchToolChatMessages(messages []WorkbenchRelayMessage) []map[string]any {
	out := make([]map[string]any, 0, len(messages))
	for _, m := range messages {
		item := map[string]any{"role": m.Role, "content": workbenchImageContent(m, "chat/completions")}
		if m.Role == "tool" {
			item["tool_call_id"] = m.ToolCallID
		}
		if len(m.ToolCalls) > 0 {
			calls := make([]map[string]any, 0, len(m.ToolCalls))
			for _, call := range m.ToolCalls {
				calls = append(calls, map[string]any{"id": call.ID, "type": "function", "function": map[string]any{"name": call.Name, "arguments": string(call.Arguments)}})
			}
			item["tool_calls"] = calls
		}
		out = append(out, item)
	}
	return out
}

func workbenchToolAnthropicMessages(messages []WorkbenchRelayMessage) []map[string]any {
	out := []map[string]any{}
	for _, m := range messages {
		if m.Role == "system" {
			continue
		}
		role := m.Role
		blocks := []map[string]any{}
		if m.Role == "tool" {
			role = "user"
			blocks = append(blocks, map[string]any{"type": "tool_result", "tool_use_id": m.ToolCallID, "content": m.Content})
		} else {
			blocks = workbenchImageBlocks(m, "messages")
			for _, call := range m.ToolCalls {
				blocks = append(blocks, map[string]any{"type": "tool_use", "id": call.ID, "name": call.Name, "input": call.Arguments})
			}
		}
		out = append(out, map[string]any{"role": role, "content": blocks})
	}
	return out
}

func workbenchToolResponsesInput(messages []WorkbenchRelayMessage) []map[string]any {
	out := []map[string]any{}
	for _, m := range messages {
		if m.Role == "tool" {
			out = append(out, map[string]any{"type": "function_call_output", "call_id": m.ToolCallID, "output": m.Content})
			continue
		}
		if m.Content != "" || len(m.Images) > 0 {
			content := any(m.Content)
			if len(m.Images) > 0 {
				content = workbenchImageBlocks(m, "responses")
			}
			out = append(out, map[string]any{"role": m.Role, "content": content})
		}
		for _, call := range m.ToolCalls {
			out = append(out, map[string]any{"type": "function_call", "call_id": call.ID, "name": call.Name, "arguments": string(call.Arguments)})
		}
	}
	return out
}

func workbenchImageContent(m WorkbenchRelayMessage, endpoint string) any {
	if len(m.Images) == 0 && endpoint != "responses" {
		return strings.TrimSpace(m.Content)
	}
	return workbenchImageBlocks(m, endpoint)
}

func workbenchImageBlocks(m WorkbenchRelayMessage, endpoint string) []map[string]any {
	textType := "text"
	if endpoint == "responses" {
		textType = "input_text"
	}
	blocks := []map[string]any{}
	if m.Content != "" {
		blocks = append(blocks, map[string]any{"type": textType, "text": m.Content})
	}
	for _, url := range m.Images {
		switch endpoint {
		case "responses":
			blocks = append(blocks, map[string]any{"type": "input_image", "image_url": url})
		case "messages":
			header, encoded, _ := strings.Cut(url, ";base64,")
			blocks = append(blocks, map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": strings.TrimPrefix(header, "data:"), "data": encoded}})
		default:
			blocks = append(blocks, map[string]any{"type": "image_url", "image_url": map[string]string{"url": url}})
		}
	}
	return blocks
}

func workbenchRelayModelOutput(endpoint string, body []byte) (string, []WorkbenchToolCall, error) {
	var payload map[string]any
	if json.Unmarshal(body, &payload) != nil {
		return "", nil, fmt.Errorf("%w: invalid model JSON", apperrors.ErrClusterUnready)
	}
	calls := []WorkbenchToolCall{}
	switch endpoint {
	case "chat/completions":
		choices, _ := payload["choices"].([]any)
		if len(choices) > 0 {
			choice := workbenchMap(choices[0])
			message := workbenchMap(choice["message"])
			raw, _ := message["tool_calls"].([]any)
			for _, v := range raw {
				item := workbenchMap(v)
				function := workbenchMap(item["function"])
				calls = append(calls, WorkbenchToolCall{ID: workbenchString(item["id"]), Name: workbenchString(function["name"]), Arguments: json.RawMessage(workbenchString(function["arguments"]))})
			}
		}
	case "messages":
		blocks, _ := payload["content"].([]any)
		for _, v := range blocks {
			item := workbenchMap(v)
			if item["type"] == "tool_use" {
				args, _ := json.Marshal(item["input"])
				calls = append(calls, WorkbenchToolCall{ID: workbenchString(item["id"]), Name: workbenchString(item["name"]), Arguments: args})
			}
		}
	case "responses":
		items, _ := payload["output"].([]any)
		for _, v := range items {
			item := workbenchMap(v)
			if item["type"] == "function_call" {
				calls = append(calls, WorkbenchToolCall{ID: workbenchString(item["call_id"]), Name: workbenchString(item["name"]), Arguments: json.RawMessage(workbenchString(item["arguments"]))})
			}
		}
	}
	if err := validateWorkbenchToolCalls(calls); err != nil {
		return "", nil, err
	}
	text, err := workbenchRelayResponseText(endpoint, body)
	if err != nil && len(calls) == 0 {
		return "", nil, err
	}
	return text, calls, nil
}

func validateWorkbenchToolCalls(calls []WorkbenchToolCall) error {
	if len(calls) > 8 {
		return fmt.Errorf("%w: model tool-call limit exceeded", apperrors.ErrInvalidArgument)
	}
	seen := map[string]bool{}
	for _, call := range calls {
		var args map[string]any
		if call.ID == "" || len(call.ID) > 128 || call.Name == "" || len(call.Name) > 64 || len(call.Arguments) > 32768 || json.Unmarshal(call.Arguments, &args) != nil || args == nil || seen[call.ID] {
			return fmt.Errorf("%w: invalid or incomplete model tool call", apperrors.ErrInvalidArgument)
		}
		seen[call.ID] = true
	}
	return nil
}

type workbenchToolFragment struct {
	ID, Name, Arguments string
	InitialInput        json.RawMessage
}

func workbenchRelayStreamOutput(endpoint string, body []byte) ([]WorkbenchToolCall, map[string]float64, error) {
	fragments := map[int]*workbenchToolFragment{}
	usage := workbenchReportedUsage(body)
	completed := false
	frames := strings.Split(strings.ReplaceAll(string(body), "\r\n", "\n"), "\n\n")
	for _, frame := range frames {
		var data []string
		for _, line := range strings.Split(frame, "\n") {
			if strings.HasPrefix(line, "data:") {
				data = append(data, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			}
		}
		raw := []byte(strings.Join(data, "\n"))
		var payload map[string]any
		if len(raw) == 0 {
			continue
		}
		if string(raw) == "[DONE]" {
			completed = true
			continue
		}
		if json.Unmarshal(raw, &payload) != nil {
			return nil, nil, fmt.Errorf("%w: malformed model stream frame", apperrors.ErrClusterUnready)
		}
		finished, err := workbenchStreamCompletion(endpoint, payload)
		if err != nil {
			return nil, nil, err
		}
		completed = completed || finished
		switch endpoint {
		case "chat/completions":
			workbenchOpenAIToolFragments(fragments, payload)
		case "messages":
			workbenchAnthropicToolFragments(fragments, payload)
		case "responses":
			workbenchResponsesToolFragments(fragments, payload)
		}
	}
	if !completed {
		return nil, nil, fmt.Errorf("%w: model stream ended without a completion signal", apperrors.ErrClusterUnready)
	}
	indexes := make([]int, 0, len(fragments))
	for i := range fragments {
		indexes = append(indexes, i)
	}
	sort.Ints(indexes)
	calls := make([]WorkbenchToolCall, 0, len(indexes))
	for _, i := range indexes {
		f := fragments[i]
		if f.Arguments == "" && len(f.InitialInput) > 0 {
			f.Arguments = string(f.InitialInput)
		}
		calls = append(calls, WorkbenchToolCall{ID: f.ID, Name: f.Name, Arguments: json.RawMessage(f.Arguments)})
	}
	return calls, usage, validateWorkbenchToolCalls(calls)
}

func workbenchFragment(items map[int]*workbenchToolFragment, index int) *workbenchToolFragment {
	if items[index] == nil {
		items[index] = &workbenchToolFragment{}
	}
	return items[index]
}
func workbenchOpenAIToolFragments(items map[int]*workbenchToolFragment, payload map[string]any) {
	choices, _ := payload["choices"].([]any)
	if len(choices) == 0 {
		return
	}
	calls, _ := workbenchMap(workbenchMap(choices[0])["delta"])["tool_calls"].([]any)
	for _, raw := range calls {
		call := workbenchMap(raw)
		f := workbenchFragment(items, int(workbenchNumber(call["index"])))
		fn := workbenchMap(call["function"])
		f.ID += workbenchString(call["id"])
		f.Name += workbenchString(fn["name"])
		f.Arguments += workbenchString(fn["arguments"])
	}
}
func workbenchAnthropicToolFragments(items map[int]*workbenchToolFragment, payload map[string]any) {
	index := int(workbenchNumber(payload["index"]))
	block := workbenchMap(payload["content_block"])
	if block["type"] == "tool_use" {
		f := workbenchFragment(items, index)
		f.ID = workbenchString(block["id"])
		f.Name = workbenchString(block["name"])
		if input, ok := block["input"].(map[string]any); ok {
			f.InitialInput, _ = json.Marshal(input)
		}
	}
	delta := workbenchMap(payload["delta"])
	if delta["type"] == "input_json_delta" {
		workbenchFragment(items, index).Arguments += workbenchString(delta["partial_json"])
	}
}
func workbenchResponsesToolFragments(items map[int]*workbenchToolFragment, payload map[string]any) {
	index := int(workbenchNumber(payload["output_index"]))
	item := workbenchMap(payload["item"])
	if item["type"] == "function_call" {
		f := workbenchFragment(items, index)
		f.ID = workbenchString(item["call_id"])
		f.Name = workbenchString(item["name"])
		if args := workbenchString(item["arguments"]); args != "" {
			f.Arguments = args
		}
	}
	if payload["type"] == "response.function_call_arguments.delta" {
		workbenchFragment(items, index).Arguments += workbenchString(payload["delta"])
	}
}
func workbenchMap(value any) map[string]any { out, _ := value.(map[string]any); return out }
func workbenchString(value any) string      { out, _ := value.(string); return out }
func workbenchNumber(value any) float64     { out, _ := value.(float64); return out }
