package sohacli

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
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
				"args":    []string{"mcp", "start", "--profile", profileName(profileNameValue)},
			},
		},
	}
	return writePrettyJSON(rt.Out, config)
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
			"serverInfo": map[string]any{"name": "soha", "version": "0.1.0"},
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
	case "prompts/list":
		manifest, err := s.client.Capabilities(ctx, s.headers)
		if err != nil {
			resp.Error = &rpcError{Code: -32000, Message: err.Error()}
			return resp
		}
		resp.Result = map[string]any{"prompts": mcpPrompts(manifest.Prompts)}
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
		out = append(out, map[string]any{
			"name":        item.Name,
			"description": toolDescription(item),
			"inputSchema": inputSchema,
		})
	}
	return out
}

func mcpResources(items []ResourceCapability) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{
			"uri":         "soha://resource/" + item.Name,
			"name":        item.Name,
			"description": item.Description,
		})
	}
	return out
}

func mcpPrompts(items []PromptCapability) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{
			"name":        item.Name,
			"description": item.Description,
		})
	}
	return out
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
