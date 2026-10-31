package sohacli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const mcpBackendErrorCode int64 = -32000

func runMCP(ctx context.Context, args []string, rt Runtime) error {
	if len(args) == 0 {
		return runMCPStart(ctx, nil, rt)
	}
	switch args[0] {
	case "start":
		return runMCPStart(ctx, args[1:], rt)
	case "install":
		return runMCPInstall(args[1:], rt)
	default:
		if strings.HasPrefix(strings.TrimSpace(args[0]), "-") {
			return runMCPStart(ctx, args, rt)
		}
		return fmt.Errorf("unknown mcp command %q", args[0])
	}
}

func runMCPStart(ctx context.Context, args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("mcp", args, rt)
	profileFlag := fs.String("profile", "", "profile name")
	baseURL := fs.String("base-url", "", "Soha server URL; defaults to the current profile or official SaaS")
	serverAlias := fs.String("server", "", "alias for --base-url")
	aiClientID := fs.String("ai-client-id", "", "override AI client id")
	aiClientName := fs.String("ai-client", "", "override AI client display name")
	skillID := fs.String("skill-id", "", "override skill id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	profile, err := loadMCPRuntimeProfile(ctx, rt, *profileFlag, *baseURL, *serverAlias)
	if err != nil {
		return err
	}
	headers := gatewayHeaders(profile, *aiClientID, *aiClientName, *skillID, "soha-mcp")
	server := mcpServer{
		client:  gatewayClient(rt, profile),
		headers: headers,
		in:      rt.In,
		out:     rt.Out,
	}
	return server.serve(ctx)
}

func loadMCPRuntimeProfile(ctx context.Context, rt Runtime, requested, baseURL, serverAlias string) (ProfileConfig, error) {
	if normalizeServerURL(baseURL) != "" && normalizeServerURL(serverAlias) != "" && !sameServerURL(baseURL, serverAlias) {
		return ProfileConfig{}, fmt.Errorf("--base-url and --server must refer to the same server when both are set")
	}
	explicitServer := normalizeServerURL(firstNonEmptyString(baseURL, serverAlias, env("SOHA_SERVER")))
	if strings.TrimSpace(env("SOHA_TOKEN")) != "" && explicitServer != "" {
		_, _, profile, err := loadRuntimeProfileForServer(ctx, rt, requested, explicitServer)
		return profile, err
	}
	cfg, err := loadConfig(rt.ConfigPath)
	if err != nil {
		return ProfileConfig{}, err
	}
	name := profileName(firstNonEmptyString(requested, cfg.CurrentProfile))
	stored, configured := cfg.Profiles[name]
	if configured && explicitServer == "" && env("SOHA_TOKEN") == "" && !sameServerURL(stored.ServerURL, defaultServerURL) {
		return ProfileConfig{}, fmt.Errorf("profile %q targets %s; pass --base-url %s to use that self-hosted deployment", name, stored.ServerURL, normalizeServerURL(stored.ServerURL))
	}
	target := firstNonEmptyString(explicitServer, stored.ServerURL, defaultServerURL)
	_, _, profile, err := loadRuntimeProfileForServer(ctx, rt, name, target)
	if err != nil {
		return ProfileConfig{}, err
	}
	profile.ServerURL = normalizeServerURL(target)
	return profile, nil
}

func runMCPInstall(args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("mcp install", args, rt)
	profile := fs.String("profile", "", "profile name")
	baseURL := fs.String("base-url", "", "Soha server URL for a self-hosted deployment")
	serverAlias := fs.String("server", "", "alias for --base-url")
	command := fs.String("command", "soha", "soha executable path")
	aiClientID := fs.String("ai-client-id", "", "AI client id to include in generated args")
	aiClientName := fs.String("ai-client", "", "AI client display name to include in generated args")
	skillID := fs.String("skill-id", "", "skill id to include in generated args")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if normalizeServerURL(*baseURL) != "" && normalizeServerURL(*serverAlias) != "" && normalizeServerURL(*baseURL) != normalizeServerURL(*serverAlias) {
		return fmt.Errorf("--base-url and --server must refer to the same server when both are set")
	}
	profileNameValue := strings.TrimSpace(*profile)
	if profileNameValue == "" {
		cfg, err := loadConfig(rt.ConfigPath)
		if err != nil {
			return err
		}
		profileNameValue = cfg.CurrentProfile
	}
	effectiveBaseURL := firstNonEmptyString(*baseURL, *serverAlias)
	if strings.TrimSpace(effectiveBaseURL) == "" {
		cfg, err := loadConfig(rt.ConfigPath)
		if err != nil {
			return err
		}
		if profileConfig, ok := cfg.Profiles[profileName(profileNameValue)]; ok && normalizeServerURL(profileConfig.ServerURL) != defaultServerURL {
			effectiveBaseURL = profileConfig.ServerURL
		}
	}
	config := map[string]any{
		"mcpServers": map[string]any{
			"soha": map[string]any{
				"command": *command,
				"args": mcpInstallArgs(
					profileName(profileNameValue),
					effectiveBaseURL,
					*aiClientID,
					*aiClientName,
					*skillID,
				),
			},
		},
	}
	return writePrettyJSON(rt.Out, config)
}

func mcpInstallArgs(profileNameValue, baseURL, aiClientID, aiClientName, skillID string) []string {
	args := []string{"mcp", "--profile", profileNameValue}
	if strings.TrimSpace(baseURL) != "" {
		args = append(args, "--base-url", normalizeServerURL(baseURL))
	}
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
}

func (s mcpServer) serve(ctx context.Context) error {
	manifest, err := s.client.Capabilities(ctx, s.headers)
	if err != nil {
		return fmt.Errorf("load MCP capabilities: %s", redactSensitiveText(err.Error()))
	}
	server := mcpsdk.NewServer(
		&mcpsdk.Implementation{Name: "soha", Version: BuildInfo().Version},
		&mcpsdk.ServerOptions{
			Instructions: mcpInstructions(),
			Capabilities: &mcpsdk.ServerCapabilities{},
		},
	)
	s.addCapabilityTaskTools(server)
	s.addInspectionTools(server)
	for _, item := range manifest.Tools {
		if err := s.addTool(server, item); err != nil {
			return err
		}
	}
	for _, item := range manifest.Resources {
		s.addResource(server, item)
	}
	for _, item := range manifest.Prompts {
		s.addPrompt(server, item)
	}
	err = server.Run(ctx, &mcpsdk.IOTransport{
		Reader: io.NopCloser(s.in),
		Writer: nopWriteCloser{s.out},
	})
	if err != nil && strings.HasSuffix(err.Error(), ": EOF") {
		return nil
	}
	return err
}

func (s mcpServer) addTool(server *mcpsdk.Server, item ToolCapability) error {
	inputSchema := item.InputSchema
	if inputSchema == nil {
		inputSchema = map[string]any{"type": "object", "additionalProperties": true}
	}
	if schemaType, _ := inputSchema["type"].(string); schemaType != "object" {
		return fmt.Errorf("MCP tool %q input schema must have type object", item.Name)
	}
	inputSchema = mcpToolSchemaWithSecretRefs(inputSchema)
	tool := &mcpsdk.Tool{
		Name:        item.Name,
		Title:       strings.TrimSpace(item.Title),
		Description: toolDescription(item),
		InputSchema: inputSchema,
		Meta:        mcpsdk.Meta(mcpSohaToolMeta(item)),
		Annotations: mcpSDKToolAnnotations(item),
	}
	if len(item.OutputSchema) > 0 {
		tool.OutputSchema = mcpInvocationOutputSchema()
	}
	server.AddTool(tool, func(ctx context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		arguments := map[string]any{}
		if len(req.Params.Arguments) > 0 {
			if err := json.Unmarshal(req.Params.Arguments, &arguments); err != nil {
				return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: "invalid tool arguments"}
			}
		}
		secretRefs, err := extractMCPSecretRefs(arguments)
		if err != nil {
			return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: err.Error()}
		}
		result, err := s.client.InvokeCapability(ctx, item, arguments, "", secretRefs, s.headers)
		if err != nil {
			return mcpTextResult(redactSensitiveText(err.Error()), true), nil
		}
		raw, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return nil, mcpBackendError(err)
		}
		response := mcpTextResult(string(raw), false)
		var structured map[string]any
		if err := json.Unmarshal(raw, &structured); err != nil {
			return nil, mcpBackendError(err)
		}
		response.StructuredContent = structured
		return response, nil
	})
	return nil
}

func mcpToolSchemaWithSecretRefs(input map[string]any) map[string]any {
	schema := make(map[string]any, len(input))
	for key, value := range input {
		schema[key] = value
	}
	properties := map[string]any{}
	if existing, ok := input["properties"].(map[string]any); ok {
		for key, value := range existing {
			properties[key] = value
		}
	}
	properties["_sohaSecretRefs"] = map[string]any{
		"type": "object", "maxProperties": 64,
		"propertyNames":        map[string]any{"pattern": secretAliasPattern.String()},
		"additionalProperties": map[string]any{"type": "string", "pattern": secretURIPattern.String()},
	}
	schema["properties"] = properties
	return schema
}

func extractMCPSecretRefs(arguments map[string]any) (map[string]string, error) {
	raw, exists := arguments["_sohaSecretRefs"]
	if !exists {
		return nil, nil
	}
	delete(arguments, "_sohaSecretRefs")
	values, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("_sohaSecretRefs must be an object")
	}
	refs := make(map[string]string, len(values))
	for alias, rawURI := range values {
		uri, ok := rawURI.(string)
		if !ok {
			return nil, fmt.Errorf("_sohaSecretRefs values must be strings")
		}
		refs[alias] = uri
	}
	if err := validateSecretRefs(refs); err != nil {
		return nil, err
	}
	return refs, nil
}

func (s mcpServer) addResource(server *mcpsdk.Server, item ResourceCapability) {
	meta := mcpSohaCapabilityMeta(item.PermissionKeys, item.RequiredScopes)
	if len(item.ContextSchema) > 0 {
		meta = withSohaMeta(meta, "contextSchema", item.ContextSchema)
	}
	server.AddResource(&mcpsdk.Resource{
		URI:         item.Name,
		Name:        item.Name,
		Description: item.Description,
		Meta:        mcpsdk.Meta(meta),
	}, func(ctx context.Context, req *mcpsdk.ReadResourceRequest) (*mcpsdk.ReadResourceResult, error) {
		result, err := s.client.ReadResource(ctx, req.Params.URI, mcpContext(req.Params.Meta), s.headers)
		if err != nil {
			return nil, mcpBackendError(err)
		}
		return mcpResourceReadResult(result), nil
	})
}

func (s mcpServer) addPrompt(server *mcpsdk.Server, item PromptCapability) {
	meta := mcpSohaCapabilityMeta(item.PermissionKeys, item.RequiredScopes)
	if len(item.ArgumentSchema) > 0 {
		meta = withSohaMeta(meta, "argumentSchema", item.ArgumentSchema)
	}
	if len(item.ContextSchema) > 0 {
		meta = withSohaMeta(meta, "contextSchema", item.ContextSchema)
	}
	server.AddPrompt(&mcpsdk.Prompt{
		Name:        item.Name,
		Description: item.Description,
		Arguments:   mcpSDKPromptArguments(item.ArgumentSchema),
		Meta:        mcpsdk.Meta(meta),
	}, func(ctx context.Context, req *mcpsdk.GetPromptRequest) (*mcpsdk.GetPromptResult, error) {
		arguments := make(map[string]any, len(req.Params.Arguments))
		for name, value := range req.Params.Arguments {
			arguments[name] = value
		}
		result, err := s.client.GetPrompt(ctx, item.Name, arguments, mcpContext(req.Params.Meta), s.headers)
		if err != nil {
			return nil, mcpBackendError(err)
		}
		return &mcpsdk.GetPromptResult{
			Description: result.Description,
			Messages:    mcpPromptMessages(result.Messages),
		}, nil
	})
}

func mcpInstructions() string {
	return "soha MCP is a Gateway proxy. Tools, resources, and prompts are listed from the AI Gateway manifest, and calls/read/get requests are sent back to soha AI Gateway for permission checks, skill bindings, AI client context, redaction, risk policy, approval, and audit. This local MCP process does not access PostgreSQL, Kubernetes, runner workspaces, kubeconfigs, Docker, or privileged prompt/resource content directly."
}

func mcpSDKPromptArguments(schema map[string]any) []*mcpsdk.PromptArgument {
	items := mcpPromptArguments(schema)
	out := make([]*mcpsdk.PromptArgument, 0, len(items))
	for _, item := range items {
		name, ok := item["name"].(string)
		if !ok || strings.TrimSpace(name) == "" {
			continue
		}
		description, _ := item["description"].(string)
		required, _ := item["required"].(bool)
		out = append(out, &mcpsdk.PromptArgument{
			Name:        name,
			Description: description,
			Required:    required,
		})
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
		item := map[string]any{"name": name, "required": required[name]}
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

func withSohaMeta(meta map[string]any, key string, value any) map[string]any {
	if meta == nil {
		meta = map[string]any{"soha": map[string]any{}}
	}
	soha, _ := meta["soha"].(map[string]any)
	soha[key] = value
	meta["soha"] = soha
	return meta
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
	if item.Version != "" {
		soha["capabilityVersion"] = item.Version
	}
	if item.Execution != nil {
		soha["execution"] = item.Execution
	}
	soha["inputSemantics"], soha["outputSemantics"] = item.InputSemantics, item.OutputSemantics
	soha["effects"], soha["producesAssessment"] = item.Effects, item.ProducesAssessment
	if len(item.OutputSchema) > 0 {
		soha["outputSchema"] = item.OutputSchema
	}
	return meta
}

func mcpInvocationOutputSchema() map[string]any {
	// MCP returns the Gateway envelope. Domain output may be held for approval or
	// redacted by policy, so its original schema belongs in capability metadata.
	return map[string]any{
		"type": "object", "required": []string{"toolName", "riskLevel", "requiresApproval", "result"},
		"properties": map[string]any{
			"toolName": map[string]any{"type": "string"}, "riskLevel": map[string]any{"type": "string"},
			"requiresApproval": map[string]any{"type": "boolean"}, "result": map[string]any{"type": "string"},
			"output": map[string]any{}, "task": map[string]any{"type": "object"},
		},
	}
}

func mcpSDKToolAnnotations(item ToolCapability) *mcpsdk.ToolAnnotations {
	annotations := mcpToolAnnotations(item)
	readOnly, _ := annotations["readOnlyHint"].(bool)
	destructive, _ := annotations["destructiveHint"].(bool)
	idempotent, _ := annotations["idempotentHint"].(bool)
	openWorld, _ := annotations["openWorldHint"].(bool)
	title, _ := annotations["title"].(string)
	return &mcpsdk.ToolAnnotations{
		Title:           title,
		ReadOnlyHint:    readOnly,
		DestructiveHint: &destructive,
		IdempotentHint:  idempotent,
		OpenWorldHint:   &openWorld,
	}
}

func mcpToolAnnotations(item ToolCapability) map[string]any {
	riskLevel := strings.TrimSpace(item.RiskLevel)
	readOnly := riskLevel == "read"
	destructive := riskLevel == "mutate" || riskLevel == "execute" || riskLevel == "high"
	idempotent := item.Execution != nil && item.Execution.Idempotent
	return map[string]any{
		"title":           firstNonEmptyString(strings.TrimSpace(item.Title), item.Name),
		"readOnlyHint":    readOnly,
		"destructiveHint": destructive,
		"idempotentHint":  idempotent,
		"openWorldHint":   true,
	}
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

func mcpTextResult(text string, isError bool) *mcpsdk.CallToolResult {
	return &mcpsdk.CallToolResult{
		Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: text}},
		IsError: isError,
	}
}

func mcpResourceReadResult(result ResourceReadResult) *mcpsdk.ReadResourceResult {
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
	return &mcpsdk.ReadResourceResult{Contents: []*mcpsdk.ResourceContents{{
		URI:      firstNonEmptyString(result.URI, result.Name),
		MIMEType: mimeType,
		Text:     text,
	}}}
}

func mcpPromptMessages(items []PromptMessage) []*mcpsdk.PromptMessage {
	out := make([]*mcpsdk.PromptMessage, 0, len(items))
	for _, item := range items {
		role := strings.TrimSpace(item.Role)
		if role != "assistant" {
			role = "user"
		}
		out = append(out, &mcpsdk.PromptMessage{
			Role:    mcpsdk.Role(role),
			Content: &mcpsdk.TextContent{Text: item.Content},
		})
	}
	return out
}

func mcpContext(meta mcpsdk.Meta) map[string]any {
	if contextValues, ok := meta["soha/context"].(map[string]any); ok {
		return contextValues
	}
	soha, _ := meta["soha"].(map[string]any)
	contextValues, _ := soha["context"].(map[string]any)
	return contextValues
}

func mcpBackendError(err error) error {
	return &jsonrpc.Error{Code: mcpBackendErrorCode, Message: redactSensitiveText(err.Error())}
}

type nopWriteCloser struct {
	io.Writer
}

func (nopWriteCloser) Close() error { return nil }

func newFlagSet(name string, out io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(out)
	return fs
}
