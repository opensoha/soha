package sohacli

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func runMCP(ctx context.Context, args []string, rt Runtime) error {
	if len(args) == 0 {
		return fmt.Errorf("mcp requires a subcommand: start or install")
	}
	switch args[0] {
	case "start":
		return runMCPStart(ctx, args[1:], rt)
	case "install":
		return runMCPInstall(args[1:], rt)
	default:
		return fmt.Errorf("unknown mcp command %q", args[0])
	}
}

func runMCPStart(ctx context.Context, args []string, rt Runtime) error {
	fs := newFlagSet("mcp start", rt.Err)
	profileFlag := fs.String("profile", "", "profile name")
	aiClientID := fs.String("ai-client-id", "", "override AI client id")
	aiClientName := fs.String("ai-client", "", "override AI client display name")
	skillID := fs.String("skill-id", "", "override skill id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	_, _, profile, err := loadRuntimeProfile(rt, *profileFlag)
	if err != nil {
		return err
	}
	headers := gatewayHeaders(profile, *aiClientID, *aiClientName, *skillID, "soha-mcp")
	server := mcpServer{
		client:  gatewayClient(rt, profile),
		headers: headers,
		in:      rt.In,
		out:     rt.Out,
		err:     rt.Err,
	}
	return server.serve(ctx)
}

func runMCPInstall(args []string, rt Runtime) error {
	fs := newFlagSet("mcp install", rt.Err)
	profile := fs.String("profile", "", "profile name")
	command := fs.String("command", "soha-cli", "soha-cli executable path")
	aiClientID := fs.String("ai-client-id", "", "AI client id to include in generated args")
	aiClientName := fs.String("ai-client", "", "AI client display name to include in generated args")
	skillID := fs.String("skill-id", "", "skill id to include in generated args")
	if err := fs.Parse(args); err != nil {
		return err
	}
	profileNameValue := strings.TrimSpace(*profile)
	if profileNameValue == "" {
		cfg, err := loadConfig(rt.ConfigPath)
		if err != nil {
			return err
		}
		profileNameValue = cfg.CurrentProfile
	}
	config := map[string]any{
		"mcpServers": map[string]any{
			"soha": map[string]any{
				"command": *command,
				"args":    mcpInstallArgs(profileName(profileNameValue), *aiClientID, *aiClientName, *skillID),
			},
		},
	}
	return writePrettyJSON(rt.Out, config)
}

func mcpInstallArgs(profileNameValue, aiClientID, aiClientName, skillID string) []string {
	args := []string{"mcp", "start", "--profile", profileNameValue}
	if strings.TrimSpace(aiClientID) != "" {
		args = append(args, "--ai-client-id", strings.TrimSpace(aiClientID))
	}
	if strings.TrimSpace(aiClientName) != "" {
		args = append(args, "--ai-client", strings.TrimSpace(aiClientName))
	}
	if strings.TrimSpace(skillID) != "" {
		args = append(args, "--skill-id", strings.TrimSpace(skillID))
	}
	return args
}

type mcpServer struct {
	client  APIClient
	headers map[string]string
	in      io.Reader
	out     io.Writer
	err     io.Writer
}

func (s mcpServer) serve(ctx context.Context) error {
	reader := bufio.NewReader(s.in)
	for {
		msg, err := readRPCMessage(reader)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		if msg.ID == nil && strings.HasPrefix(msg.Method, "notifications/") {
			continue
		}
		resp := s.handle(ctx, msg)
		if msg.ID == nil {
			continue
		}
		if err := writeRPCMessage(s.out, resp); err != nil {
			return err
		}
	}
}

func (s mcpServer) handle(ctx context.Context, msg rpcMessage) rpcMessage {
	resp := rpcMessage{JSONRPC: "2.0", ID: msg.ID}
	switch msg.Method {
	case "initialize":
		resp.Result = map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]any{
				"tools":     map[string]any{},
				"resources": map[string]any{},
				"prompts":   map[string]any{},
			},
			"serverInfo":   map[string]any{"name": "soha", "version": "0.1.0"},
			"instructions": mcpInstructions(),
		}
	case "tools/list":
		manifest, err := s.client.Capabilities(ctx, s.headers)
		if err != nil {
			resp.Error = &rpcError{Code: -32000, Message: err.Error()}
			return resp
		}
		resp.Result = map[string]any{"tools": mcpTools(manifest.Tools)}
	case "tools/call":
		var params struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			resp.Error = &rpcError{Code: -32602, Message: "invalid tools/call params"}
			return resp
		}
		if strings.TrimSpace(params.Name) == "" {
			resp.Error = &rpcError{Code: -32602, Message: "tools/call requires name"}
			return resp
		}
		result, err := s.client.InvokeTool(ctx, params.Name, params.Arguments, s.headers)
		if err != nil {
			resp.Result = mcpTextResult(err.Error(), true)
			return resp
		}
		raw, _ := json.MarshalIndent(result, "", "  ")
		resp.Result = mcpTextResult(string(raw), false)
	case "resources/list":
		manifest, err := s.client.Capabilities(ctx, s.headers)
		if err != nil {
			resp.Error = &rpcError{Code: -32000, Message: err.Error()}
			return resp
		}
		resp.Result = map[string]any{"resources": mcpResources(manifest.Resources)}
	case "resources/read":
		var params struct {
			URI     string         `json:"uri"`
			Name    string         `json:"name"`
			Context map[string]any `json:"context"`
		}
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			resp.Error = &rpcError{Code: -32602, Message: "invalid resources/read params"}
			return resp
		}
		resourceName := firstNonEmptyString(params.URI, params.Name)
		if strings.TrimSpace(resourceName) == "" {
			resp.Error = &rpcError{Code: -32602, Message: "resources/read requires uri or name"}
			return resp
		}
		result, err := s.client.ReadResource(ctx, resourceName, params.Context, s.headers)
		if err != nil {
			resp.Error = &rpcError{Code: -32000, Message: err.Error()}
			return resp
		}
		resp.Result = mcpResourceReadResult(result)
	case "prompts/list":
		manifest, err := s.client.Capabilities(ctx, s.headers)
		if err != nil {
			resp.Error = &rpcError{Code: -32000, Message: err.Error()}
			return resp
		}
		resp.Result = map[string]any{"prompts": mcpPrompts(manifest.Prompts)}
	case "prompts/get":
		var params struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
			Context   map[string]any `json:"context"`
		}
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			resp.Error = &rpcError{Code: -32602, Message: "invalid prompts/get params"}
			return resp
		}
		if strings.TrimSpace(params.Name) == "" {
			resp.Error = &rpcError{Code: -32602, Message: "prompts/get requires name"}
			return resp
		}
		result, err := s.client.GetPrompt(ctx, params.Name, params.Arguments, params.Context, s.headers)
		if err != nil {
			resp.Error = &rpcError{Code: -32000, Message: err.Error()}
			return resp
		}
		resp.Result = map[string]any{
			"description": result.Description,
			"messages":    mcpPromptMessages(result.Messages),
		}
	default:
		resp.Error = &rpcError{Code: -32601, Message: "method not found: " + msg.Method}
	}
	return resp
}

func mcpTools(items []ToolCapability) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		inputSchema := item.InputSchema
		if inputSchema == nil {
			inputSchema = map[string]any{"type": "object", "additionalProperties": true}
		}
		tool := map[string]any{
			"name":        item.Name,
			"description": toolDescription(item),
			"inputSchema": inputSchema,
			"annotations": mcpToolAnnotations(item),
		}
		if len(item.OutputSchema) > 0 {
			tool["outputSchema"] = item.OutputSchema
		}
		if meta := mcpSohaToolMeta(item); meta != nil {
			tool["_meta"] = meta
		}
		out = append(out, tool)
	}
	return out
}

func mcpInstructions() string {
	return "soha MCP is a Gateway proxy. Tools, resources, and prompts are listed from the AI Gateway manifest, and calls/read/get requests are sent back to soha AI Gateway for permission checks, skill bindings, AI client context, redaction, risk policy, approval, and audit. This local MCP process does not access PostgreSQL, Kubernetes, runner workspaces, kubeconfigs, Docker, or privileged prompt/resource content directly."
}

func mcpResources(items []ResourceCapability) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		resource := map[string]any{
			"uri":         item.Name,
			"name":        item.Name,
			"description": item.Description,
		}
		if len(item.ContextSchema) > 0 {
			resource["contextSchema"] = item.ContextSchema
		}
		if meta := mcpSohaCapabilityMeta(item.PermissionKeys, item.RequiredScopes); meta != nil {
			resource["_meta"] = meta
		}
		out = append(out, resource)
	}
	return out
}

func mcpPrompts(items []PromptCapability) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		prompt := map[string]any{
			"name":        item.Name,
			"description": item.Description,
		}
		if len(item.ArgumentSchema) > 0 {
			prompt["argumentSchema"] = item.ArgumentSchema
			prompt["arguments"] = mcpPromptArguments(item.ArgumentSchema)
		}
		if len(item.ContextSchema) > 0 {
			prompt["contextSchema"] = item.ContextSchema
		}
		if meta := mcpSohaCapabilityMeta(item.PermissionKeys, item.RequiredScopes); meta != nil {
			prompt["_meta"] = meta
		}
		out = append(out, prompt)
	}
	return out
}

func mcpPromptArguments(schema map[string]any) []map[string]any {
	if len(schema) == 0 {
		return nil
	}
	required := map[string]bool{}
	for _, item := range stringSliceFromAny(schema["required"]) {
		required[item] = true
	}
	properties, _ := schema["properties"].(map[string]any)
	names := make([]string, 0, len(properties))
	for name := range properties {
		if strings.TrimSpace(name) != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	out := make([]map[string]any, 0, len(names))
	for _, name := range names {
		item := map[string]any{
			"name":     name,
			"required": required[name],
		}
		if property, _ := properties[name].(map[string]any); len(property) > 0 {
			if description, _ := property["description"].(string); strings.TrimSpace(description) != "" {
				item["description"] = strings.TrimSpace(description)
			}
		}
		out = append(out, item)
	}
	return out
}

func mcpSohaCapabilityMeta(permissionKeys, requiredScopes []string) map[string]any {
	soha := map[string]any{}
	if len(permissionKeys) > 0 {
		soha["permissionKeys"] = append([]string(nil), permissionKeys...)
	}
	if len(requiredScopes) > 0 {
		soha["requiredScopes"] = append([]string(nil), requiredScopes...)
	}
	if len(soha) == 0 {
		return nil
	}
	return map[string]any{"soha": soha}
}

func mcpSohaToolMeta(item ToolCapability) map[string]any {
	meta := mcpSohaCapabilityMeta(item.PermissionKeys, item.RequiredScopes)
	if meta == nil {
		meta = map[string]any{"soha": map[string]any{}}
	}
	soha, _ := meta["soha"].(map[string]any)
	if item.Domain != "" {
		soha["domain"] = item.Domain
	}
	if item.Action != "" {
		soha["action"] = item.Action
	}
	if item.MCPAdapterID != "" {
		soha["mcpAdapterId"] = item.MCPAdapterID
	}
	if item.MCPToolName != "" {
		soha["mcpToolName"] = item.MCPToolName
	}
	if item.RiskLevel != "" {
		soha["riskLevel"] = item.RiskLevel
	}
	soha["requiresApproval"] = item.RequiresApproval
	if len(soha) == 0 {
		return nil
	}
	return meta
}

func mcpToolAnnotations(item ToolCapability) map[string]any {
	riskLevel := strings.TrimSpace(item.RiskLevel)
	readOnly := riskLevel == "read"
	destructive := riskLevel == "mutate" || riskLevel == "execute" || riskLevel == "high"
	annotations := map[string]any{
		"title":           firstNonEmptyString(strings.TrimSpace(item.Title), item.Name),
		"readOnlyHint":    readOnly,
		"destructiveHint": destructive,
		"idempotentHint":  readOnly,
		"openWorldHint":   true,
	}
	return annotations
}

func toolDescription(item ToolCapability) string {
	parts := []string{item.Description}
	if item.RiskLevel != "" {
		parts = append(parts, "risk="+item.RiskLevel)
	}
	if item.RequiresApproval {
		parts = append(parts, "requiresApproval=true")
	}
	return strings.Join(parts, "\n")
}

func mcpTextResult(text string, isError bool) map[string]any {
	return map[string]any{
		"content": []map[string]string{{"type": "text", "text": text}},
		"isError": isError,
	}
}

func mcpResourceReadResult(result ResourceReadResult) map[string]any {
	text := result.Text
	if strings.TrimSpace(text) == "" && result.Data != nil {
		raw, _ := json.MarshalIndent(result.Data, "", "  ")
		text = string(raw)
	}
	if strings.TrimSpace(text) == "" {
		text = "{}"
	}
	mimeType := strings.TrimSpace(result.MIMEType)
	if mimeType == "" {
		mimeType = "application/json"
	}
	uri := firstNonEmptyString(result.URI, result.Name)
	return map[string]any{
		"contents": []map[string]any{{
			"uri":      uri,
			"mimeType": mimeType,
			"text":     text,
		}},
	}
}

func mcpPromptMessages(items []PromptMessage) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		role := strings.TrimSpace(item.Role)
		if role != "assistant" {
			role = "user"
		}
		out = append(out, map[string]any{
			"role": role,
			"content": map[string]string{
				"type": "text",
				"text": item.Content,
			},
		})
	}
	return out
}

func readRPCMessage(reader *bufio.Reader) (rpcMessage, error) {
	header, err := reader.Peek(1)
	if err != nil {
		return rpcMessage{}, err
	}
	if len(header) > 0 && header[0] == '{' {
		line, err := reader.ReadBytes('\n')
		if err != nil && err != io.EOF {
			return rpcMessage{}, err
		}
		var msg rpcMessage
		return msg, json.Unmarshal(line, &msg)
	}
	contentLength := -1
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return rpcMessage{}, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			contentLength, err = strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return rpcMessage{}, err
			}
		}
	}
	if contentLength < 0 {
		return rpcMessage{}, fmt.Errorf("missing Content-Length header")
	}
	raw := make([]byte, contentLength)
	if _, err := io.ReadFull(reader, raw); err != nil {
		return rpcMessage{}, err
	}
	var msg rpcMessage
	return msg, json.Unmarshal(raw, &msg)
}

func writeRPCMessage(out io.Writer, msg rpcMessage) error {
	raw, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "Content-Length: %d\r\n\r\n%s", len(raw), raw)
	return err
}

func newFlagSet(name string, out io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(out)
	return fs
}
