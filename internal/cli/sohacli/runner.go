package sohacli

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

type Runtime struct {
	In         io.Reader
	Out        io.Writer
	Err        io.Writer
	ConfigPath string
	HTTPClient *http.Client
}

func Run(ctx context.Context, args []string, rt Runtime) int {
	if rt.In == nil {
		rt.In = os.Stdin
	}
	if rt.Out == nil {
		rt.Out = os.Stdout
	}
	if rt.Err == nil {
		rt.Err = os.Stderr
	}
	if rt.ConfigPath == "" {
		rt.ConfigPath = defaultConfigPath()
	}
	if len(args) == 0 {
		printUsage(rt.Err)
		return 2
	}
	cmd := args[0]
	var err error
	switch cmd {
	case "login":
		err = runLogin(ctx, args[1:], rt)
	case "capabilities":
		err = runCapabilities(ctx, args[1:], rt)
	case "tool":
		err = runTool(ctx, args[1:], rt)
	case "resource":
		err = runResource(ctx, args[1:], rt)
	case "prompt":
		err = runPrompt(ctx, args[1:], rt)
	case "token":
		err = runToken(ctx, args[1:], rt)
	case "service-account":
		err = runServiceAccount(ctx, args[1:], rt)
	case "audit":
		err = runAudit(ctx, args[1:], rt)
	case "approval":
		err = runApproval(ctx, args[1:], rt)
	case "governance":
		err = runGovernance(ctx, args[1:], rt)
	case "profile":
		err = runProfile(args[1:], rt)
	case "context":
		err = runContext(args[1:], rt)
	case "mcp":
		err = runMCP(ctx, args[1:], rt)
	case "skill":
		err = runSkill(args[1:], rt)
	case "diagnose":
		err = runDiagnose(ctx, args[1:], rt)
	case "completion":
		err = runCompletion(args[1:], rt)
	case "help", "-h", "--help":
		printUsage(rt.Out)
		return 0
	default:
		err = fmt.Errorf("unknown command %q", cmd)
	}
	if err != nil {
		fmt.Fprintln(rt.Err, "error:", err)
		return 1
	}
	return 0
}

func printUsage(out io.Writer) {
	fmt.Fprintln(out, "Usage: soha-cli <command> [options]")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Commands:")
	fmt.Fprintln(out, "  login          Authenticate and store a local profile")
	fmt.Fprintln(out, "  capabilities   Print the current AI Gateway manifest")
	fmt.Fprintln(out, "  tool call      Invoke an AI Gateway tool with JSON input")
	fmt.Fprintln(out, "  resource read  Read an AI Gateway MCP resource")
	fmt.Fprintln(out, "  prompt get     Get an AI Gateway MCP prompt")
	fmt.Fprintln(out, "  token          Manage personal access tokens")
	fmt.Fprintln(out, "  service-account Manage AI Gateway service accounts and tokens")
	fmt.Fprintln(out, "  audit list     Query AI Gateway audit logs")
	fmt.Fprintln(out, "  approval       List, trace, or decide AI Gateway approval requests")
	fmt.Fprintln(out, "  governance status Show AI Gateway governance health and metrics")
	fmt.Fprintln(out, "  profile        List, show, or switch profiles")
	fmt.Fprintln(out, "  context        Show or update AI client context headers")
	fmt.Fprintln(out, "  mcp start      Run the soha MCP stdio server")
	fmt.Fprintln(out, "  mcp install    Print MCP client configuration")
	fmt.Fprintln(out, "  skill list     List local soha AI Gateway skill files")
	fmt.Fprintln(out, "  skill install  Install local soha AI Gateway skill files")
	fmt.Fprintln(out, "  diagnose       Check profile and Gateway connectivity")
	fmt.Fprintln(out, "  completion     Print shell completion script")
}

func runLogin(ctx context.Context, args []string, rt Runtime) error {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	fs.SetOutput(rt.Err)
	server := fs.String("server", env("SOHA_SERVER"), "soha server URL")
	login := fs.String("login", env("SOHA_LOGIN"), "login name")
	password := fs.String("password", env("SOHA_PASSWORD"), "login password")
	profile := fs.String("profile", defaultProfile, "profile name")
	aiClientID := fs.String("ai-client-id", env("SOHA_AI_CLIENT_ID"), "AI client id")
	aiClientName := fs.String("ai-client", env("SOHA_AI_CLIENT"), "AI client display name")
	source := fs.String("source", "soha-cli", "request source label")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*server) == "" {
		return fmt.Errorf("--server is required")
	}
	if strings.TrimSpace(*login) == "" {
		return fmt.Errorf("--login is required")
	}
	if strings.TrimSpace(*password) == "" {
		fmt.Fprint(rt.Err, "Password: ")
		line, err := bufio.NewReader(rt.In).ReadString('\n')
		if err != nil && err != io.EOF {
			return err
		}
		*password = strings.TrimSpace(line)
	}
	if strings.TrimSpace(*password) == "" {
		return fmt.Errorf("password is required")
	}
	client := APIClient{ServerURL: *server, Client: rt.HTTPClient}
	result, err := client.Login(ctx, *login, *password)
	if err != nil {
		return err
	}
	if strings.TrimSpace(result.Data.Tokens.AccessToken) == "" {
		return fmt.Errorf("login response did not include an access token")
	}
	cfg, err := loadConfig(rt.ConfigPath)
	if err != nil {
		return err
	}
	name := profileName(*profile)
	cfg.CurrentProfile = name
	cfg.Profiles[name] = ProfileConfig{
		ServerURL:    strings.TrimRight(strings.TrimSpace(*server), "/"),
		AccessToken:  result.Data.Tokens.AccessToken,
		RefreshToken: result.Data.Tokens.RefreshToken,
		ExpiresAt:    result.Data.Tokens.ExpiresAt,
		UserID:       result.Data.User.UserID,
		UserName:     result.Data.User.UserName,
		AIClientID:   strings.TrimSpace(*aiClientID),
		AIClientName: strings.TrimSpace(*aiClientName),
		Source:       strings.TrimSpace(*source),
	}
	if err := saveConfig(rt.ConfigPath, cfg); err != nil {
		return err
	}
	fmt.Fprintf(rt.Out, "Logged in to %s as %s (profile %s)\n", cfg.Profiles[name].ServerURL, result.Data.User.UserName, name)
	return nil
}

func runCapabilities(ctx context.Context, args []string, rt Runtime) error {
	fs := flag.NewFlagSet("capabilities", flag.ContinueOnError)
	fs.SetOutput(rt.Err)
	profileFlag := fs.String("profile", "", "profile name")
	format := fs.String("output", "json", "output format: json, names, or inputs")
	jsonOutput := fs.Bool("json", false, "print JSON output")
	aiClientID := fs.String("ai-client-id", "", "override AI client id")
	aiClientName := fs.String("ai-client", "", "override AI client display name")
	skillID := fs.String("skill-id", "", "override skill id")
	source := fs.String("source", "", "override source label")
	if err := fs.Parse(args); err != nil {
		return err
	}
	_, name, profile, err := loadRuntimeProfile(rt, *profileFlag)
	if err != nil {
		return err
	}
	manifest, err := gatewayClient(rt, profile).Capabilities(ctx, gatewayHeaders(profile, *aiClientID, *aiClientName, *skillID, *source))
	if err != nil {
		return err
	}
	if *jsonOutput {
		*format = "json"
	}
	switch strings.TrimSpace(*format) {
	case "", "json":
		return writePrettyJSON(rt.Out, manifest)
	case "names":
		fmt.Fprintf(rt.Out, "profile: %s\n", name)
		for _, tool := range manifest.Tools {
			fmt.Fprintf(rt.Out, "tool\t%s\t%s\t%s\n", tool.Name, tool.RiskLevel, approvalText(tool.RequiresApproval))
		}
		for _, item := range manifest.Resources {
			fmt.Fprintf(rt.Out, "resource\t%s\n", item.Name)
		}
		for _, item := range manifest.Prompts {
			fmt.Fprintf(rt.Out, "prompt\t%s\n", item.Name)
		}
		for _, item := range manifest.Skills {
			fmt.Fprintf(rt.Out, "skill\t%s\t%s\n", item.ID, item.Name)
		}
		return nil
	case "inputs":
		fmt.Fprintf(rt.Out, "profile: %s\n", name)
		for _, tool := range manifest.Tools {
			required, fields := toolSchemaSummary(tool.InputSchema)
			outputRequired, outputFields := toolSchemaSummary(tool.OutputSchema)
			fmt.Fprintf(rt.Out, "tool\t%s\trequired=%s\tfields=%s\toutputRequired=%s\toutputFields=%s\n", tool.Name, strings.Join(required, ","), strings.Join(fields, ","), strings.Join(outputRequired, ","), strings.Join(outputFields, ","))
		}
		return nil
	default:
		return fmt.Errorf("unsupported output format %q", *format)
	}
}

func runTool(ctx context.Context, args []string, rt Runtime) error {
	if len(args) == 0 {
		return fmt.Errorf("tool requires a subcommand: call")
	}
	switch args[0] {
	case "call":
		return runToolCall(ctx, args[1:], rt)
	default:
		return fmt.Errorf("unknown tool command %q", args[0])
	}
}

func runToolCall(ctx context.Context, args []string, rt Runtime) error {
	toolName, args := extractLeadingToolName(args)
	fs := flag.NewFlagSet("tool call", flag.ContinueOnError)
	fs.SetOutput(rt.Err)
	profileFlag := fs.String("profile", "", "profile name")
	inputPath := fs.String("input", "", "JSON input file path, or - for stdin")
	inputJSON := fs.String("input-json", "", "inline JSON tool input")
	aiClientID := fs.String("ai-client-id", "", "override AI client id")
	aiClientName := fs.String("ai-client", "", "override AI client display name")
	skillID := fs.String("skill-id", "", "override skill id")
	source := fs.String("source", "", "override source label")
	if err := fs.Parse(args); err != nil {
		return err
	}
	toolName = firstNonEmptyString(toolName, fs.Arg(0))
	if toolName == "" {
		return fmt.Errorf("tool call requires a tool name")
	}
	if *inputPath != "" && *inputJSON != "" {
		return fmt.Errorf("use either --input or --input-json, not both")
	}
	_, _, profile, err := loadRuntimeProfile(rt, *profileFlag)
	if err != nil {
		return err
	}
	input, err := readToolInput(rt, *inputPath, *inputJSON)
	if err != nil {
		return err
	}
	result, err := gatewayClient(rt, profile).InvokeTool(ctx, toolName, input, gatewayHeaders(profile, *aiClientID, *aiClientName, *skillID, *source))
	if err != nil {
		return err
	}
	return writePrettyJSON(rt.Out, sanitizeCLIValue(result))
}

func runResource(ctx context.Context, args []string, rt Runtime) error {
	if len(args) == 0 {
		return fmt.Errorf("resource requires a subcommand: read")
	}
	switch args[0] {
	case "read":
		return runResourceRead(ctx, args[1:], rt)
	default:
		return fmt.Errorf("unknown resource command %q", args[0])
	}
}

func runResourceRead(ctx context.Context, args []string, rt Runtime) error {
	uri, args := extractLeadingValue(args)
	fs := flag.NewFlagSet("resource read", flag.ContinueOnError)
	fs.SetOutput(rt.Err)
	profileFlag := fs.String("profile", "", "profile name")
	contextPath := fs.String("context", "", "JSON context file path, or - for stdin")
	contextJSON := fs.String("context-json", "", "inline JSON context")
	aiClientID := fs.String("ai-client-id", "", "override AI client id")
	aiClientName := fs.String("ai-client", "", "override AI client display name")
	skillID := fs.String("skill-id", "", "override skill id")
	source := fs.String("source", "", "override source label")
	if err := fs.Parse(args); err != nil {
		return err
	}
	uri = firstNonEmptyString(uri, fs.Arg(0))
	if uri == "" {
		return fmt.Errorf("resource read requires a resource URI")
	}
	if *contextPath != "" && *contextJSON != "" {
		return fmt.Errorf("use either --context or --context-json, not both")
	}
	_, _, profile, err := loadRuntimeProfile(rt, *profileFlag)
	if err != nil {
		return err
	}
	contextValues, err := readJSONInput(rt, *contextPath, *contextJSON)
	if err != nil {
		return err
	}
	result, err := gatewayClient(rt, profile).ReadResource(ctx, uri, contextValues, gatewayHeaders(profile, *aiClientID, *aiClientName, *skillID, *source))
	if err != nil {
		return err
	}
	return writePrettyJSON(rt.Out, sanitizeCLIValue(result))
}

func runPrompt(ctx context.Context, args []string, rt Runtime) error {
	if len(args) == 0 {
		return fmt.Errorf("prompt requires a subcommand: get")
	}
	switch args[0] {
	case "get":
		return runPromptGet(ctx, args[1:], rt)
	default:
		return fmt.Errorf("unknown prompt command %q", args[0])
	}
}

func runPromptGet(ctx context.Context, args []string, rt Runtime) error {
	name, args := extractLeadingValue(args)
	fs := flag.NewFlagSet("prompt get", flag.ContinueOnError)
	fs.SetOutput(rt.Err)
	profileFlag := fs.String("profile", "", "profile name")
	argumentsPath := fs.String("arguments", "", "JSON arguments file path, or - for stdin")
	argumentsJSON := fs.String("arguments-json", "", "inline JSON arguments")
	contextPath := fs.String("context", "", "JSON context file path")
	contextJSON := fs.String("context-json", "", "inline JSON context")
	aiClientID := fs.String("ai-client-id", "", "override AI client id")
	aiClientName := fs.String("ai-client", "", "override AI client display name")
	skillID := fs.String("skill-id", "", "override skill id")
	source := fs.String("source", "", "override source label")
	if err := fs.Parse(args); err != nil {
		return err
	}
	name = firstNonEmptyString(name, fs.Arg(0))
	if name == "" {
		return fmt.Errorf("prompt get requires a prompt name")
	}
	if *argumentsPath != "" && *argumentsJSON != "" {
		return fmt.Errorf("use either --arguments or --arguments-json, not both")
	}
	if *contextPath != "" && *contextJSON != "" {
		return fmt.Errorf("use either --context or --context-json, not both")
	}
	if strings.TrimSpace(*argumentsPath) == "-" && strings.TrimSpace(*contextPath) == "-" {
		return fmt.Errorf("only one of --arguments or --context can read from stdin")
	}
	_, _, profile, err := loadRuntimeProfile(rt, *profileFlag)
	if err != nil {
		return err
	}
	arguments, err := readJSONInput(rt, *argumentsPath, *argumentsJSON)
	if err != nil {
		return err
	}
	contextValues, err := readJSONInput(rt, *contextPath, *contextJSON)
	if err != nil {
		return err
	}
	result, err := gatewayClient(rt, profile).GetPrompt(ctx, name, arguments, contextValues, gatewayHeaders(profile, *aiClientID, *aiClientName, *skillID, *source))
	if err != nil {
		return err
	}
	return writePrettyJSON(rt.Out, sanitizeCLIValue(result))
}

func runToken(ctx context.Context, args []string, rt Runtime) error {
	if len(args) == 0 {
		return fmt.Errorf("token requires a subcommand: list, create, or revoke")
	}
	switch args[0] {
	case "list":
		return runTokenList(ctx, args[1:], rt)
	case "create":
		return runTokenCreate(ctx, args[1:], rt)
	case "revoke":
		return runTokenRevoke(ctx, args[1:], rt)
	default:
		return fmt.Errorf("unknown token command %q", args[0])
	}
}

func runTokenList(ctx context.Context, args []string, rt Runtime) error {
	fs := flag.NewFlagSet("token list", flag.ContinueOnError)
	fs.SetOutput(rt.Err)
	profileFlag := fs.String("profile", "", "profile name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	_, _, profile, err := loadRuntimeProfile(rt, *profileFlag)
	if err != nil {
		return err
	}
	items, err := gatewayClient(rt, profile).ListPersonalAccessTokens(ctx, gatewayHeaders(profile, "", "", "", ""))
	if err != nil {
		return err
	}
	return writePrettyJSON(rt.Out, sanitizeCLIValue(items))
}

func runTokenCreate(ctx context.Context, args []string, rt Runtime) error {
	fs := flag.NewFlagSet("token create", flag.ContinueOnError)
	fs.SetOutput(rt.Err)
	profileFlag := fs.String("profile", "", "profile name")
	name := fs.String("name", "", "token name")
	scopes := fs.String("scopes", "", "comma-separated token scopes")
	permissionKeys := fs.String("permission-keys", "", "comma-separated permission keys")
	expiresAt := fs.String("expires-at", "", "RFC3339 expiration time")
	metadata := fs.String("metadata-json", "", "metadata JSON object")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*name) == "" {
		return fmt.Errorf("--name is required")
	}
	_, _, profile, err := loadRuntimeProfile(rt, *profileFlag)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"name":           strings.TrimSpace(*name),
		"scopes":         splitCSV(*scopes),
		"permissionKeys": splitCSV(*permissionKeys),
	}
	if *expiresAt != "" {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*expiresAt))
		if err != nil {
			return fmt.Errorf("invalid --expires-at; use RFC3339")
		}
		payload["expiresAt"] = parsed.Format(time.RFC3339)
	}
	if *metadata != "" {
		parsed, err := parseJSONObject([]byte(*metadata))
		if err != nil {
			return fmt.Errorf("invalid --metadata-json: %w", err)
		}
		payload["metadata"] = parsed
	}
	created, err := gatewayClient(rt, profile).CreatePersonalAccessToken(ctx, payload, gatewayHeaders(profile, "", "", "", ""))
	if err != nil {
		return err
	}
	return writePrettyJSON(rt.Out, sanitizeCreatedToken(created))
}

func runTokenRevoke(ctx context.Context, args []string, rt Runtime) error {
	fs := flag.NewFlagSet("token revoke", flag.ContinueOnError)
	fs.SetOutput(rt.Err)
	profileFlag := fs.String("profile", "", "profile name")
	tokenID := fs.String("id", "", "token id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	id := firstNonEmptyString(*tokenID, fs.Arg(0))
	if id == "" {
		return fmt.Errorf("token revoke requires a token id")
	}
	_, _, profile, err := loadRuntimeProfile(rt, *profileFlag)
	if err != nil {
		return err
	}
	if err := gatewayClient(rt, profile).RevokePersonalAccessToken(ctx, id, gatewayHeaders(profile, "", "", "", "")); err != nil {
		return err
	}
	fmt.Fprintf(rt.Out, "Revoked token %s\n", id)
	return nil
}

func runServiceAccount(ctx context.Context, args []string, rt Runtime) error {
	if len(args) == 0 {
		return fmt.Errorf("service-account requires a subcommand: list, create, token-list, token-create, or token-revoke")
	}
	switch args[0] {
	case "list":
		return runServiceAccountList(ctx, args[1:], rt)
	case "create":
		return runServiceAccountCreate(ctx, args[1:], rt)
	case "token-list":
		return runServiceAccountTokenList(ctx, args[1:], rt)
	case "token-create":
		return runServiceAccountTokenCreate(ctx, args[1:], rt)
	case "token-revoke":
		return runServiceAccountTokenRevoke(ctx, args[1:], rt)
	default:
		return fmt.Errorf("unknown service-account command %q", args[0])
	}
}

func runServiceAccountList(ctx context.Context, args []string, rt Runtime) error {
	fs := flag.NewFlagSet("service-account list", flag.ContinueOnError)
	fs.SetOutput(rt.Err)
	profileFlag := fs.String("profile", "", "profile name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	_, _, profile, err := loadRuntimeProfile(rt, *profileFlag)
	if err != nil {
		return err
	}
	items, err := gatewayClient(rt, profile).ListServiceAccounts(ctx, gatewayHeaders(profile, "", "", "", ""))
	if err != nil {
		return err
	}
	return writePrettyJSON(rt.Out, sanitizeCLIValue(items))
}

func runServiceAccountTokenList(ctx context.Context, args []string, rt Runtime) error {
	fs := flag.NewFlagSet("service-account token-list", flag.ContinueOnError)
	fs.SetOutput(rt.Err)
	profileFlag := fs.String("profile", "", "profile name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	_, _, profile, err := loadRuntimeProfile(rt, *profileFlag)
	if err != nil {
		return err
	}
	items, err := gatewayClient(rt, profile).ListServiceAccountTokens(ctx, gatewayHeaders(profile, "", "", "", ""))
	if err != nil {
		return err
	}
	return writePrettyJSON(rt.Out, sanitizeCLIValue(items))
}

func runServiceAccountCreate(ctx context.Context, args []string, rt Runtime) error {
	fs := flag.NewFlagSet("service-account create", flag.ContinueOnError)
	fs.SetOutput(rt.Err)
	profileFlag := fs.String("profile", "", "profile name")
	id := fs.String("id", "", "service account id")
	name := fs.String("name", "", "service account name")
	description := fs.String("description", "", "service account description")
	status := fs.String("status", "active", "service account status")
	roleIDs := fs.String("role-ids", "", "comma-separated role ids")
	teamIDs := fs.String("team-ids", "", "comma-separated team ids")
	scopeGrantIDs := fs.String("scope-grant-ids", "", "comma-separated scope grant ids")
	metadata := fs.String("metadata-json", "", "metadata JSON object")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*name) == "" {
		return fmt.Errorf("--name is required")
	}
	_, _, profile, err := loadRuntimeProfile(rt, *profileFlag)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"id":            strings.TrimSpace(*id),
		"name":          strings.TrimSpace(*name),
		"description":   strings.TrimSpace(*description),
		"status":        strings.TrimSpace(*status),
		"roleIds":       splitCSV(*roleIDs),
		"teamIds":       splitCSV(*teamIDs),
		"scopeGrantIds": splitCSV(*scopeGrantIDs),
	}
	if *metadata != "" {
		parsed, err := parseJSONObject([]byte(*metadata))
		if err != nil {
			return fmt.Errorf("invalid --metadata-json: %w", err)
		}
		payload["metadata"] = parsed
	}
	item, err := gatewayClient(rt, profile).CreateServiceAccount(ctx, payload, gatewayHeaders(profile, "", "", "", ""))
	if err != nil {
		return err
	}
	return writePrettyJSON(rt.Out, sanitizeCLIValue(item))
}

func runServiceAccountTokenCreate(ctx context.Context, args []string, rt Runtime) error {
	fs := flag.NewFlagSet("service-account token-create", flag.ContinueOnError)
	fs.SetOutput(rt.Err)
	profileFlag := fs.String("profile", "", "profile name")
	serviceAccountID := fs.String("service-account-id", "", "service account id")
	name := fs.String("name", "", "token name")
	scopes := fs.String("scopes", "", "comma-separated token scopes")
	permissionKeys := fs.String("permission-keys", "", "comma-separated permission keys")
	expiresAt := fs.String("expires-at", "", "RFC3339 expiration time")
	metadata := fs.String("metadata-json", "", "metadata JSON object")
	if err := fs.Parse(args); err != nil {
		return err
	}
	saID := firstNonEmptyString(*serviceAccountID, fs.Arg(0))
	if saID == "" {
		return fmt.Errorf("service-account token-create requires --service-account-id or positional id")
	}
	if strings.TrimSpace(*name) == "" {
		return fmt.Errorf("--name is required")
	}
	_, _, profile, err := loadRuntimeProfile(rt, *profileFlag)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"name":           strings.TrimSpace(*name),
		"scopes":         splitCSV(*scopes),
		"permissionKeys": splitCSV(*permissionKeys),
	}
	if *expiresAt != "" {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*expiresAt))
		if err != nil {
			return fmt.Errorf("invalid --expires-at; use RFC3339")
		}
		payload["expiresAt"] = parsed.Format(time.RFC3339)
	}
	if *metadata != "" {
		parsed, err := parseJSONObject([]byte(*metadata))
		if err != nil {
			return fmt.Errorf("invalid --metadata-json: %w", err)
		}
		payload["metadata"] = parsed
	}
	created, err := gatewayClient(rt, profile).CreateServiceAccountToken(ctx, saID, payload, gatewayHeaders(profile, "", "", "", ""))
	if err != nil {
		return err
	}
	return writePrettyJSON(rt.Out, sanitizeCreatedToken(created))
}

func runServiceAccountTokenRevoke(ctx context.Context, args []string, rt Runtime) error {
	fs := flag.NewFlagSet("service-account token-revoke", flag.ContinueOnError)
	fs.SetOutput(rt.Err)
	profileFlag := fs.String("profile", "", "profile name")
	tokenID := fs.String("id", "", "token id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	id := firstNonEmptyString(*tokenID, fs.Arg(0))
	if id == "" {
		return fmt.Errorf("service-account token-revoke requires a token id")
	}
	_, _, profile, err := loadRuntimeProfile(rt, *profileFlag)
	if err != nil {
		return err
	}
	if err := gatewayClient(rt, profile).RevokeServiceAccountToken(ctx, id, gatewayHeaders(profile, "", "", "", "")); err != nil {
		return err
	}
	fmt.Fprintf(rt.Out, "Revoked service account token %s\n", id)
	return nil
}

func runAudit(ctx context.Context, args []string, rt Runtime) error {
	if len(args) == 0 {
		return fmt.Errorf("audit requires a subcommand: list")
	}
	switch args[0] {
	case "list":
		return runAuditList(ctx, args[1:], rt)
	default:
		return fmt.Errorf("unknown audit command %q", args[0])
	}
}

func runAuditList(ctx context.Context, args []string, rt Runtime) error {
	fs := flag.NewFlagSet("audit list", flag.ContinueOnError)
	fs.SetOutput(rt.Err)
	profileFlag := fs.String("profile", "", "profile name")
	actor := fs.String("actor", "", "actor id")
	actorType := fs.String("actor-type", "", "actor type")
	aiClientID := fs.String("ai-client-id", "", "AI client id")
	skillID := fs.String("skill-id", "", "skill id")
	toolName := fs.String("tool-name", "", "tool name")
	riskLevel := fs.String("risk-level", "", "risk level")
	result := fs.String("result", "", "result")
	action := fs.String("action", "", "action")
	approvalRequestID := fs.String("approval-request-id", "", "approval request id")
	from := fs.String("from", "", "RFC3339 start time")
	to := fs.String("to", "", "RFC3339 end time")
	limit := fs.Int("limit", 100, "result limit")
	if err := fs.Parse(args); err != nil {
		return err
	}
	_, _, profile, err := loadRuntimeProfile(rt, *profileFlag)
	if err != nil {
		return err
	}
	query := url.Values{}
	setQuery(query, "actor", *actor)
	setQuery(query, "actorType", *actorType)
	setQuery(query, "aiClientId", *aiClientID)
	setQuery(query, "skillId", *skillID)
	setQuery(query, "toolName", *toolName)
	setQuery(query, "riskLevel", *riskLevel)
	setQuery(query, "result", *result)
	setQuery(query, "action", *action)
	setQuery(query, "approvalRequestId", *approvalRequestID)
	setQuery(query, "from", *from)
	setQuery(query, "to", *to)
	if *limit > 0 {
		query.Set("limit", fmt.Sprint(*limit))
	}
	items, err := gatewayClient(rt, profile).ListAuditLogs(ctx, query, gatewayHeaders(profile, "", "", "", ""))
	if err != nil {
		return err
	}
	return writePrettyJSON(rt.Out, sanitizeCLIValue(items))
}

func runApproval(ctx context.Context, args []string, rt Runtime) error {
	if len(args) == 0 {
		return fmt.Errorf("approval requires a subcommand: list, timeline, approve, reject, or cancel")
	}
	switch args[0] {
	case "list":
		return runApprovalList(ctx, args[1:], rt)
	case "timeline":
		return runApprovalTimeline(ctx, args[1:], rt)
	case "approve", "reject", "cancel":
		return runApprovalDecision(ctx, args[0], args[1:], rt)
	default:
		return fmt.Errorf("unknown approval command %q", args[0])
	}
}

func runApprovalList(ctx context.Context, args []string, rt Runtime) error {
	fs := flag.NewFlagSet("approval list", flag.ContinueOnError)
	fs.SetOutput(rt.Err)
	profileFlag := fs.String("profile", "", "profile name")
	id := fs.String("id", "", "approval request id")
	status := fs.String("status", "", "approval request status")
	actor := fs.String("actor", "", "actor id")
	actorType := fs.String("actor-type", "", "actor type")
	aiClientID := fs.String("ai-client-id", "", "AI client id")
	skillID := fs.String("skill-id", "", "skill id")
	toolName := fs.String("tool-name", "", "tool name")
	riskLevel := fs.String("risk-level", "", "risk level")
	strategy := fs.String("strategy", "", "approval strategy")
	from := fs.String("from", "", "RFC3339 start time")
	to := fs.String("to", "", "RFC3339 end time")
	limit := fs.Int("limit", 100, "result limit")
	if err := fs.Parse(args); err != nil {
		return err
	}
	_, _, profile, err := loadRuntimeProfile(rt, *profileFlag)
	if err != nil {
		return err
	}
	query := url.Values{}
	setQuery(query, "approvalRequestId", *id)
	setQuery(query, "status", *status)
	setQuery(query, "actor", *actor)
	setQuery(query, "actorType", *actorType)
	setQuery(query, "aiClientId", *aiClientID)
	setQuery(query, "skillId", *skillID)
	setQuery(query, "toolName", *toolName)
	setQuery(query, "riskLevel", *riskLevel)
	setQuery(query, "strategy", *strategy)
	setQuery(query, "from", *from)
	setQuery(query, "to", *to)
	if *limit > 0 {
		query.Set("limit", fmt.Sprint(*limit))
	}
	items, err := gatewayClient(rt, profile).ListApprovalRequests(ctx, query, gatewayHeaders(profile, "", "", "", ""))
	if err != nil {
		return err
	}
	return writePrettyJSON(rt.Out, sanitizeCLIValue(items))
}

func runApprovalTimeline(ctx context.Context, args []string, rt Runtime) error {
	fs := flag.NewFlagSet("approval timeline", flag.ContinueOnError)
	fs.SetOutput(rt.Err)
	profileFlag := fs.String("profile", "", "profile name")
	requestID := ""
	if len(args) > 0 && !strings.HasPrefix(strings.TrimSpace(args[0]), "-") {
		requestID = strings.TrimSpace(args[0])
		args = args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if requestID == "" && fs.NArg() > 0 {
		requestID = strings.TrimSpace(fs.Arg(0))
	}
	if requestID == "" {
		return fmt.Errorf("approval timeline requires an approval request id")
	}
	_, _, profile, err := loadRuntimeProfile(rt, *profileFlag)
	if err != nil {
		return err
	}
	item, err := gatewayClient(rt, profile).GetApprovalTimeline(ctx, requestID, gatewayHeaders(profile, "", "", "", ""))
	if err != nil {
		return err
	}
	return writePrettyJSON(rt.Out, sanitizeCLIValue(item))
}

func runApprovalDecision(ctx context.Context, action string, args []string, rt Runtime) error {
	fs := flag.NewFlagSet("approval "+action, flag.ContinueOnError)
	fs.SetOutput(rt.Err)
	profileFlag := fs.String("profile", "", "profile name")
	comment := fs.String("comment", "", "decision comment")
	requestID := ""
	if len(args) > 0 && !strings.HasPrefix(strings.TrimSpace(args[0]), "-") {
		requestID = strings.TrimSpace(args[0])
		args = args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if requestID == "" && fs.NArg() > 0 {
		requestID = strings.TrimSpace(fs.Arg(0))
	}
	if requestID == "" {
		return fmt.Errorf("approval %s requires an approval request id", action)
	}
	_, _, profile, err := loadRuntimeProfile(rt, *profileFlag)
	if err != nil {
		return err
	}
	result, err := gatewayClient(rt, profile).DecideApprovalRequest(ctx, requestID, action, *comment, gatewayHeaders(profile, "", "", "", ""))
	if err != nil {
		return err
	}
	return writePrettyJSON(rt.Out, sanitizeCLIValue(result))
}

func runGovernance(ctx context.Context, args []string, rt Runtime) error {
	if len(args) == 0 {
		return fmt.Errorf("governance requires a subcommand: status")
	}
	switch args[0] {
	case "status":
		return runGovernanceStatus(ctx, args[1:], rt)
	default:
		return fmt.Errorf("unknown governance command %q", args[0])
	}
}

func runGovernanceStatus(ctx context.Context, args []string, rt Runtime) error {
	fs := flag.NewFlagSet("governance status", flag.ContinueOnError)
	fs.SetOutput(rt.Err)
	profileFlag := fs.String("profile", "", "profile name")
	windowHours := fs.Int("window-hours", 24, "audit window in hours")
	jsonOut := fs.Bool("json", false, "print full JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *windowHours < 1 || *windowHours > 168 {
		return fmt.Errorf("window-hours must be between 1 and 168")
	}
	_, _, profile, err := loadRuntimeProfile(rt, *profileFlag)
	if err != nil {
		return err
	}
	status, err := gatewayClient(rt, profile).GovernanceStatus(ctx, *windowHours, gatewayHeaders(profile, "", "", "", ""))
	if err != nil {
		return err
	}
	if *jsonOut {
		return writePrettyJSON(rt.Out, sanitizeCLIValue(status))
	}
	fmt.Fprintf(rt.Out, "health: %s\t%s\n", status.Health.Status, redactSensitiveText(status.Health.Message))
	for _, check := range status.Health.Checks {
		fmt.Fprintf(rt.Out, "healthCheck: %s\t%s\tcount=%d\t%s\n", redactSensitiveText(check.Status), redactSensitiveText(check.Name), check.Count, redactSensitiveText(check.Message))
	}
	fmt.Fprintf(rt.Out, "windowHours: %d\n", status.WindowHours)
	fmt.Fprintf(rt.Out, "calls: total=%d success=%d deny=%d failure=%d pendingApproval=%d dryRun=%d\n",
		status.Metrics.TotalCalls,
		status.Metrics.SuccessCount,
		status.Metrics.DenyCount,
		status.Metrics.FailureCount,
		status.Metrics.PendingApprovalCount,
		status.Metrics.DryRunCount,
	)
	fmt.Fprintf(rt.Out, "tokens: personalActive=%d serviceActive=%d expiringSoon=%d expiredActive=%d stale=%d neverUsed=%d lastUsed=%s\n",
		status.Tokens.PersonalAccessTokens.Active,
		status.Tokens.ServiceAccountTokens.Active,
		len(status.Tokens.ExpiringSoon),
		len(status.Tokens.ExpiredActive),
		len(status.Tokens.Stale),
		len(status.Tokens.NeverUsed),
		status.Tokens.LastUsedTrackingState,
	)
	fmt.Fprintf(rt.Out, "clients: total=%d active=%d pendingApproval=%d registrationApproval=%s\n",
		status.Clients.Total,
		status.Clients.Active,
		status.Clients.PendingApproval,
		status.Clients.RegistrationApproval,
	)
	nextDue := ""
	if status.Approvals.NextDueAt != nil {
		nextDue = status.Approvals.NextDueAt.Format(time.RFC3339)
	}
	fmt.Fprintf(rt.Out, "approvals: pending=%d dueSoon=%d stale=%d overdue=%d oldestPendingHours=%d nextDue=%s\n",
		status.Approvals.Pending,
		status.Approvals.DueSoon,
		status.Approvals.StalePending,
		status.Approvals.Overdue,
		status.Approvals.OldestPendingHours,
		nextDue,
	)
	fmt.Fprintf(rt.Out, "policyCoverage: access=%d activeAccess=%d grants=%d activeGrants=%d skills=%d activeSkills=%d budget=%s rateLimit=%s redaction=%s resourceScopes=%s scopedAccess=%d scopedGrants=%d\n",
		status.PolicyCoverage.AccessPolicies,
		status.PolicyCoverage.ActiveAccessPolicies,
		status.PolicyCoverage.ToolGrants,
		status.PolicyCoverage.ActiveToolGrants,
		status.PolicyCoverage.SkillBindings,
		status.PolicyCoverage.ActiveSkillBindings,
		status.PolicyCoverage.BudgetState,
		status.PolicyCoverage.RateLimitState,
		status.PolicyCoverage.RedactionPolicyState,
		status.PolicyCoverage.ResourceScopeState,
		status.PolicyCoverage.ResourceScopedAccessPolicies,
		status.PolicyCoverage.ResourceScopedToolGrants,
	)
	fmt.Fprintf(rt.Out, "redaction: matches=%d audits=%d inputAudits=%d outputAudits=%d field=%d sensitiveKey=%d sensitiveText=%d valuePattern=%d classifier=%d structured=%d\n",
		status.Redaction.TotalMatches,
		status.Redaction.AuditsWithRedaction,
		status.Redaction.InputAudits,
		status.Redaction.OutputAudits,
		status.Redaction.FieldMatches,
		status.Redaction.SensitiveKeyMatches,
		status.Redaction.SensitiveTextMatches,
		status.Redaction.ValuePatternMatches,
		status.Redaction.SecretClassifierMatches,
		status.Redaction.StructuredSecretMatches,
	)
	if summary := governanceMetricCountsSummary(status.Redaction.TopTargets); summary != "" {
		fmt.Fprintf(rt.Out, "redactionTargets: %s\n", summary)
	}
	if summary := governanceMetricCountsSummary(status.Redaction.TopMatchTypes); summary != "" {
		fmt.Fprintf(rt.Out, "redactionMatchTypes: %s\n", summary)
	}
	if summary := governanceMetricCountsSummary(status.Redaction.TopClassifiers); summary != "" {
		fmt.Fprintf(rt.Out, "redactionClassifiers: %s\n", summary)
	}
	if summary := governanceMetricCountsSummary(status.Redaction.TopFieldPaths); summary != "" {
		fmt.Fprintf(rt.Out, "redactionFieldPaths: %s\n", summary)
	}
	if summary := governanceMetricCountsSummary(status.Redaction.TopPolicies); summary != "" {
		fmt.Fprintf(rt.Out, "redactionPolicies: %s\n", summary)
	}
	if summary := governanceMetricCountsSummary(status.Redaction.TopTools); summary != "" {
		fmt.Fprintf(rt.Out, "redactionTools: %s\n", summary)
	}
	for _, finding := range status.Anomalies {
		fmt.Fprintf(rt.Out, "finding: %s\t%s\t%d%s\t%s\n", finding.Severity, finding.Type, finding.Count, governanceFindingDetailSuffix(finding), redactSensitiveText(finding.Summary))
	}
	for _, recommendation := range status.Recommendations {
		fmt.Fprintf(rt.Out, "recommendation: %s\n", redactSensitiveText(recommendation))
	}
	for _, action := range status.RecommendationActions {
		fmt.Fprintf(rt.Out, "recommendationAction: %s\t%s\taction=%s%s\t%s\n", action.Severity, action.Type, action.Action, governanceRecommendationActionSuffix(action), redactSensitiveText(action.Summary))
	}
	return nil
}

func governanceMetricCountsSummary(items []GovernanceMetricCount) string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		key := strings.TrimSpace(redactSensitiveText(item.Key))
		if key == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s=%d", key, item.Count))
	}
	return strings.Join(parts, ",")
}

func governanceFindingDetailSuffix(finding GovernanceFinding) string {
	parts := make([]string, 0)
	if finding.ActorType != "" || finding.ActorID != "" {
		parts = append(parts, "actor="+redactSensitiveText(strings.Trim(strings.TrimSpace(finding.ActorType)+":"+strings.TrimSpace(finding.ActorID), ":")))
	}
	if finding.SubjectType != "" || finding.SubjectID != "" {
		parts = append(parts, "subject="+redactSensitiveText(strings.Trim(strings.TrimSpace(finding.SubjectType)+":"+strings.TrimSpace(finding.SubjectID), ":")))
	}
	if finding.AIClientID != "" {
		parts = append(parts, "client="+redactSensitiveText(finding.AIClientID))
	}
	if finding.PolicyID != "" {
		parts = append(parts, "policy="+redactSensitiveText(finding.PolicyID))
	}
	if finding.ApprovalRequestID != "" {
		parts = append(parts, "approval="+redactSensitiveText(finding.ApprovalRequestID))
	}
	if finding.GrantID != "" {
		parts = append(parts, "grant="+redactSensitiveText(finding.GrantID))
	}
	if finding.ToolName != "" {
		parts = append(parts, "tool="+redactSensitiveText(finding.ToolName))
	}
	if finding.RiskLevel != "" {
		parts = append(parts, "risk="+redactSensitiveText(finding.RiskLevel))
	}
	if len(parts) == 0 {
		return ""
	}
	return "\t" + strings.Join(parts, " ")
}

func governanceRecommendationActionSuffix(action GovernanceRecommendationAction) string {
	parts := make([]string, 0)
	if strings.TrimSpace(action.TargetKind) != "" {
		parts = append(parts, "target="+redactSensitiveText(strings.TrimSpace(action.TargetKind)))
	}
	if strings.TrimSpace(action.TargetID) != "" {
		parts = append(parts, "id="+redactSensitiveText(strings.TrimSpace(action.TargetID)))
	}
	if action.Count > 0 {
		parts = append(parts, fmt.Sprintf("count=%d", action.Count))
	}
	if len(action.Refs) > 0 {
		refs := make([]string, 0, len(action.Refs))
		for _, ref := range action.Refs {
			refs = append(refs, redactSensitiveText(ref))
		}
		parts = append(parts, "refs="+strings.Join(refs, ","))
	}
	if len(parts) == 0 {
		return ""
	}
	return "\t" + strings.Join(parts, "\t")
}

func runProfile(args []string, rt Runtime) error {
	if len(args) == 0 {
		args = []string{"show"}
	}
	cfg, err := loadConfig(rt.ConfigPath)
	if err != nil {
		return err
	}
	switch args[0] {
	case "list":
		names := make([]string, 0, len(cfg.Profiles))
		for name := range cfg.Profiles {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			marker := " "
			if name == cfg.CurrentProfile {
				marker = "*"
			}
			fmt.Fprintf(rt.Out, "%s %s\t%s\n", marker, name, cfg.Profiles[name].ServerURL)
		}
	case "use":
		if len(args) < 2 {
			return fmt.Errorf("profile use requires a profile name")
		}
		name := profileName(args[1])
		if _, ok := cfg.Profiles[name]; !ok {
			return fmt.Errorf("profile %q is not configured", name)
		}
		cfg.CurrentProfile = name
		if err := saveConfig(rt.ConfigPath, cfg); err != nil {
			return err
		}
		fmt.Fprintf(rt.Out, "Current profile: %s\n", name)
	case "show":
		name := profileName(firstArg(args[1:], cfg.CurrentProfile))
		profile, ok := cfg.Profiles[name]
		if !ok {
			return fmt.Errorf("profile %q is not configured", name)
		}
		view := profile
		view.AccessToken = redactToken(view.AccessToken)
		view.RefreshToken = redactToken(view.RefreshToken)
		return writePrettyJSON(rt.Out, map[string]any{"name": name, "profile": view})
	default:
		return fmt.Errorf("unknown profile command %q", args[0])
	}
	return nil
}

func runContext(args []string, rt Runtime) error {
	if len(args) == 0 {
		args = []string{"show"}
	}
	switch args[0] {
	case "show":
		fs := flag.NewFlagSet("context show", flag.ContinueOnError)
		fs.SetOutput(rt.Err)
		profileFlag := fs.String("profile", "", "profile name")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		_, name, profile, err := loadRuntimeProfile(rt, *profileFlag)
		if err != nil {
			return err
		}
		return writePrettyJSON(rt.Out, map[string]any{
			"profile":      name,
			"serverUrl":    profile.ServerURL,
			"aiClientId":   profile.AIClientID,
			"aiClientName": profile.AIClientName,
			"skillId":      profile.SkillID,
			"source":       profile.Source,
		})
	case "set":
		fs := flag.NewFlagSet("context set", flag.ContinueOnError)
		fs.SetOutput(rt.Err)
		profileFlag := fs.String("profile", "", "profile name")
		aiClientID := fs.String("ai-client-id", "", "AI client id")
		aiClientName := fs.String("ai-client", "", "AI client display name")
		skillID := fs.String("skill-id", "", "skill id")
		source := fs.String("source", "", "request source label")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		cfg, name, profile, err := loadRuntimeProfile(rt, *profileFlag)
		if err != nil {
			return err
		}
		if *aiClientID != "" {
			profile.AIClientID = strings.TrimSpace(*aiClientID)
		}
		if *aiClientName != "" {
			profile.AIClientName = strings.TrimSpace(*aiClientName)
		}
		if *skillID != "" {
			profile.SkillID = strings.TrimSpace(*skillID)
		}
		if *source != "" {
			profile.Source = strings.TrimSpace(*source)
		}
		cfg.Profiles[name] = profile
		if err := saveConfig(rt.ConfigPath, cfg); err != nil {
			return err
		}
		fmt.Fprintf(rt.Out, "Updated context for profile %s\n", name)
		return nil
	default:
		return fmt.Errorf("unknown context command %q", args[0])
	}
}

func runDiagnose(ctx context.Context, args []string, rt Runtime) error {
	fs := flag.NewFlagSet("diagnose", flag.ContinueOnError)
	fs.SetOutput(rt.Err)
	profileFlag := fs.String("profile", "", "profile name")
	toolName := fs.String("tool", "", "tool name to inspect")
	resourceName := fs.String("resource", "", "resource URI to inspect")
	promptName := fs.String("prompt", "", "prompt name to inspect")
	aiClientID := fs.String("ai-client-id", "", "override AI client id for this diagnostic request")
	aiClientName := fs.String("ai-client", "", "override AI client display name for this diagnostic request")
	skillID := fs.String("skill-id", "", "override skill id for this diagnostic request")
	source := fs.String("source", "", "override source label for this diagnostic request")
	if err := fs.Parse(args); err != nil {
		return err
	}
	_, name, profile, err := loadRuntimeProfile(rt, *profileFlag)
	if err != nil {
		return err
	}
	manifest, err := gatewayClient(rt, profile).Capabilities(ctx, gatewayHeaders(profile, *aiClientID, *aiClientName, *skillID, *source))
	if err != nil {
		return err
	}
	fmt.Fprintf(rt.Out, "profile: %s\nserver: %s\nuser: %s\n", name, profile.ServerURL, profile.UserName)
	fmt.Fprintf(rt.Out, "tools: %d\nresources: %d\nprompts: %d\nskills: %d\n", len(manifest.Tools), len(manifest.Resources), len(manifest.Prompts), len(manifest.Skills))
	fmt.Fprintf(rt.Out, "permissionKeys: %d\n", len(manifest.PermissionKeys))
	fmt.Fprintf(rt.Out, "aiClientId: %s\naiClient: %s\nskillId: %s\nsource: %s\n", firstNonEmptyString(*aiClientID, profile.AIClientID), firstNonEmptyString(*aiClientName, profile.AIClientName), firstNonEmptyString(*skillID, profile.SkillID), firstNonEmptyString(*source, profile.Source, "soha-cli"))
	if strings.TrimSpace(*toolName) != "" {
		diagnoseTool(rt.Out, manifest, strings.TrimSpace(*toolName))
	}
	if strings.TrimSpace(*resourceName) != "" {
		diagnoseResource(rt.Out, manifest, strings.TrimSpace(*resourceName))
	}
	if strings.TrimSpace(*promptName) != "" {
		diagnosePrompt(rt.Out, manifest, strings.TrimSpace(*promptName))
	}
	if len(manifest.Tools) == 0 {
		fmt.Fprintln(rt.Out, "hint: no tools visible; check ai.gateway.invoke, MCP tool grants, access policies, and skill bindings.")
	}
	return nil
}

func runCompletion(args []string, rt Runtime) error {
	shell := "bash"
	if len(args) > 0 && strings.TrimSpace(args[0]) != "" {
		shell = strings.TrimSpace(args[0])
	}
	switch shell {
	case "bash":
		_, err := fmt.Fprint(rt.Out, bashCompletionScript)
		return err
	case "zsh":
		_, err := fmt.Fprint(rt.Out, "#compdef soha-cli\n"+bashCompletionScript)
		return err
	default:
		return fmt.Errorf("unsupported shell %q", shell)
	}
}

func loadRuntimeProfile(rt Runtime, requested string) (Config, string, ProfileConfig, error) {
	cfg, err := loadConfig(rt.ConfigPath)
	if err != nil {
		return Config{}, "", ProfileConfig{}, err
	}
	name, profile, err := resolveProfile(cfg, requested)
	if err != nil {
		return Config{}, "", ProfileConfig{}, err
	}
	if token := strings.TrimSpace(env("SOHA_TOKEN")); token != "" {
		profile.AccessToken = token
	}
	if server := strings.TrimSpace(env("SOHA_SERVER")); server != "" {
		profile.ServerURL = server
	}
	if strings.TrimSpace(profile.Source) == "" {
		profile.Source = "soha-cli"
	}
	return cfg, name, profile, nil
}

func gatewayClient(rt Runtime, profile ProfileConfig) APIClient {
	return APIClient{ServerURL: profile.ServerURL, Token: profile.AccessToken, Client: rt.HTTPClient}
}

func gatewayHeaders(profile ProfileConfig, aiClientID, aiClientName, skillID, source string) map[string]string {
	return map[string]string{
		"X-Soha-AI-Client-ID": firstNonEmptyString(aiClientID, profile.AIClientID),
		"X-Soha-AI-Client":    firstNonEmptyString(aiClientName, profile.AIClientName),
		"X-Soha-Skill-ID":     firstNonEmptyString(skillID, profile.SkillID),
		"X-Soha-Source":       firstNonEmptyString(source, profile.Source, "soha-cli"),
	}
}

func writePrettyJSON(out io.Writer, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, string(raw))
	return err
}

func readToolInput(rt Runtime, path, inline string) (map[string]any, error) {
	return readJSONInput(rt, path, inline)
}

func readJSONInput(rt Runtime, path, inline string) (map[string]any, error) {
	switch {
	case strings.TrimSpace(inline) != "":
		return parseJSONObject([]byte(inline))
	case strings.TrimSpace(path) == "":
		return map[string]any{}, nil
	case strings.TrimSpace(path) == "-":
		raw, err := io.ReadAll(io.LimitReader(rt.In, 10<<20))
		if err != nil {
			return nil, err
		}
		return parseJSONObject(raw)
	default:
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		return parseJSONObject(raw)
	}
}

func parseJSONObject(raw []byte) (map[string]any, error) {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return map[string]any{}, nil
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	if out == nil {
		return map[string]any{}, nil
	}
	return out, nil
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		item := strings.TrimSpace(part)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func setQuery(values url.Values, key, value string) {
	value = strings.TrimSpace(value)
	if value != "" {
		values.Set(key, value)
	}
}

func sanitizeCreatedToken(value any) any {
	return sanitizeCLIValue(value)
}

func sanitizeCLIValue(value any) any {
	switch typed := value.(type) {
	case CreatedPersonalAccessToken:
		return map[string]any{"token": sanitizeCLIValue(typed.Token), "value": typed.Value}
	case CreatedServiceAccountToken:
		return map[string]any{"token": sanitizeCLIValue(typed.Token), "value": typed.Value}
	case ToolInvocationResult:
		return ToolInvocationResult{
			ToolName:         typed.ToolName,
			RiskLevel:        typed.RiskLevel,
			RequiresApproval: typed.RequiresApproval,
			Result:           typed.Result,
			Output:           sanitizeCLIValue(typed.Output),
			RelatedIDs:       sanitizeCLIMap(typed.RelatedIDs),
			Audit:            sanitizeCLIMap(typed.Audit),
		}
	case ResourceReadResult:
		typed.Text = redactSensitiveText(typed.Text)
		typed.Data = sanitizeCLIValue(typed.Data)
		typed.RelatedIDs = sanitizeCLIMap(typed.RelatedIDs)
		typed.Audit = sanitizeCLIMap(typed.Audit)
		return typed
	case PromptGetResult:
		typed.Description = redactSensitiveText(typed.Description)
		for index := range typed.Messages {
			typed.Messages[index].Content = redactSensitiveText(typed.Messages[index].Content)
		}
		typed.RelatedIDs = sanitizeCLIMap(typed.RelatedIDs)
		typed.Audit = sanitizeCLIMap(typed.Audit)
		return typed
	case PersonalAccessToken:
		return typed
	case ServiceAccountToken:
		return typed
	case AuditLog:
		typed.Metadata = sanitizeCLIMap(typed.Metadata)
		typed.ResourceScope = sanitizeCLIMap(typed.ResourceScope)
		typed.Summary = redactSensitiveText(typed.Summary)
		return typed
	case ApprovalRequest:
		typed.ResourceScope = sanitizeCLIMap(typed.ResourceScope)
		typed.ToolInput = sanitizeCLIMap(typed.ToolInput)
		typed.RelatedIDs = sanitizeCLIMap(typed.RelatedIDs)
		if typed.ApprovalTrace != nil {
			trace := sanitizeCLIValue(*typed.ApprovalTrace).(ApprovalTrace)
			typed.ApprovalTrace = &trace
		}
		typed.Output = sanitizeCLIValue(typed.Output)
		typed.Summary = redactSensitiveText(typed.Summary)
		typed.DecisionComment = redactSensitiveText(typed.DecisionComment)
		return typed
	case ApprovalDecisionResult:
		typed.Request = sanitizeCLIValue(typed.Request).(ApprovalRequest)
		if typed.Invocation != nil {
			invocation := sanitizeCLIValue(*typed.Invocation).(ToolInvocationResult)
			typed.Invocation = &invocation
		}
		return typed
	case ApprovalTrace:
		for index := range typed.Decisions {
			typed.Decisions[index].Comment = redactSensitiveText(typed.Decisions[index].Comment)
		}
		return typed
	case ApprovalTimelineEvent:
		typed.Summary = redactSensitiveText(typed.Summary)
		typed.Metadata = sanitizeCLIMap(typed.Metadata)
		return typed
	case ApprovalTimeline:
		typed.Request = sanitizeCLIValue(typed.Request).(ApprovalRequest)
		if typed.Trace != nil {
			trace := sanitizeCLIValue(*typed.Trace).(ApprovalTrace)
			typed.Trace = &trace
		}
		for index := range typed.Events {
			typed.Events[index] = sanitizeCLIValue(typed.Events[index]).(ApprovalTimelineEvent)
		}
		return typed
	case GovernanceStatus:
		return sanitizeGovernanceStatus(typed)
	case []PersonalAccessToken:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = sanitizeCLIValue(item)
		}
		return out
	case []ServiceAccountToken:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = sanitizeCLIValue(item)
		}
		return out
	case []ServiceAccount:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = sanitizeCLIValue(item)
		}
		return out
	case []AuditLog:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = sanitizeCLIValue(item)
		}
		return out
	case []ApprovalRequest:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = sanitizeCLIValue(item)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = sanitizeCLIValue(item)
		}
		return out
	case map[string]any:
		return sanitizeCLIMap(typed)
	case string:
		return redactSensitiveText(typed)
	default:
		return typed
	}
}

func sanitizeGovernanceStatus(status GovernanceStatus) GovernanceStatus {
	status.Health.Message = redactSensitiveText(status.Health.Message)
	for index := range status.Health.Checks {
		status.Health.Checks[index].Message = redactSensitiveText(status.Health.Checks[index].Message)
	}
	for index := range status.Tokens.ExpiringSoon {
		status.Tokens.ExpiringSoon[index] = sanitizeGovernanceTokenFinding(status.Tokens.ExpiringSoon[index])
	}
	for index := range status.Tokens.ExpiredActive {
		status.Tokens.ExpiredActive[index] = sanitizeGovernanceTokenFinding(status.Tokens.ExpiredActive[index])
	}
	for index := range status.Tokens.Stale {
		status.Tokens.Stale[index] = sanitizeGovernanceTokenFinding(status.Tokens.Stale[index])
	}
	for index := range status.Tokens.NeverUsed {
		status.Tokens.NeverUsed[index] = sanitizeGovernanceTokenFinding(status.Tokens.NeverUsed[index])
	}
	for index := range status.Anomalies {
		status.Anomalies[index].Summary = redactSensitiveText(status.Anomalies[index].Summary)
		status.Anomalies[index].ActorID = redactSensitiveText(status.Anomalies[index].ActorID)
		status.Anomalies[index].SubjectID = redactSensitiveText(status.Anomalies[index].SubjectID)
		status.Anomalies[index].AIClientID = redactSensitiveText(status.Anomalies[index].AIClientID)
		status.Anomalies[index].PolicyID = redactSensitiveText(status.Anomalies[index].PolicyID)
		status.Anomalies[index].ApprovalRequestID = redactSensitiveText(status.Anomalies[index].ApprovalRequestID)
		status.Anomalies[index].GrantID = redactSensitiveText(status.Anomalies[index].GrantID)
		status.Anomalies[index].ToolName = redactSensitiveText(status.Anomalies[index].ToolName)
	}
	status.Approvals.OldestPendingRequestID = redactSensitiveText(status.Approvals.OldestPendingRequestID)
	status.Approvals.NextDueRequestID = redactSensitiveText(status.Approvals.NextDueRequestID)
	for index := range status.Approvals.DueSoonRequestIDs {
		status.Approvals.DueSoonRequestIDs[index] = redactSensitiveText(status.Approvals.DueSoonRequestIDs[index])
	}
	for index := range status.Approvals.StalePendingRequestIDs {
		status.Approvals.StalePendingRequestIDs[index] = redactSensitiveText(status.Approvals.StalePendingRequestIDs[index])
	}
	for index := range status.Approvals.OverdueRequestIDs {
		status.Approvals.OverdueRequestIDs[index] = redactSensitiveText(status.Approvals.OverdueRequestIDs[index])
	}
	for index := range status.Recommendations {
		status.Recommendations[index] = redactSensitiveText(status.Recommendations[index])
	}
	for index := range status.RecommendationActions {
		status.RecommendationActions[index] = sanitizeGovernanceRecommendationAction(status.RecommendationActions[index])
	}
	status.Metadata = sanitizeCLIMap(status.Metadata)
	return status
}

func sanitizeGovernanceRecommendationAction(action GovernanceRecommendationAction) GovernanceRecommendationAction {
	action.Summary = redactSensitiveText(action.Summary)
	action.TargetID = redactSensitiveText(action.TargetID)
	for index := range action.Refs {
		action.Refs[index] = redactSensitiveText(action.Refs[index])
	}
	action.Metadata = sanitizeCLIMap(action.Metadata)
	return action
}

func sanitizeGovernanceTokenFinding(finding GovernanceTokenFinding) GovernanceTokenFinding {
	finding.Name = redactSensitiveText(finding.Name)
	finding.OwnerID = redactSensitiveText(finding.OwnerID)
	finding.Message = redactSensitiveText(finding.Message)
	return finding
}

func sanitizeCLIMap(values map[string]any) map[string]any {
	if values == nil {
		return nil
	}
	out := make(map[string]any, len(values))
	for key, value := range values {
		if sensitiveCLIKey(key) {
			out[key] = "[REDACTED]"
			continue
		}
		out[key] = sanitizeCLIValue(value)
	}
	return out
}

func sensitiveCLIKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	for _, needle := range []string{"token", "password", "passwd", "secret", "credential", "apikey", "api_key", "authorization", "kubeconfig", "envvar", "environmentvariable"} {
		if strings.Contains(normalized, needle) {
			return true
		}
	}
	return false
}

func redactSensitiveText(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return value
	}
	replacements := []string{"token=", "password=", "passwd=", "secret=", "authorization=", "api_key=", "apikey="}
	lower := strings.ToLower(value)
	for _, marker := range replacements {
		if index := strings.Index(lower, marker); index >= 0 {
			end := index + len(marker)
			tail := value[end:]
			if stop := strings.IndexAny(tail, " \t\n,;"); stop >= 0 {
				return value[:end] + "[REDACTED]" + tail[stop:]
			}
			return value[:end] + "[REDACTED]"
		}
	}
	return value
}

func diagnoseTool(out io.Writer, manifest Manifest, toolName string) {
	for _, tool := range manifest.Tools {
		if tool.Name == toolName {
			fmt.Fprintf(out, "tool: %s\nriskLevel: %s\nrequiresApproval: %t\n", tool.Name, tool.RiskLevel, tool.RequiresApproval)
			fmt.Fprintf(out, "domain: %s\naction: %s\nmcpAdapterId: %s\nmcpToolName: %s\n", tool.Domain, tool.Action, tool.MCPAdapterID, tool.MCPToolName)
			fmt.Fprintf(out, "requiredPermissionKeys: %s\n", strings.Join(tool.PermissionKeys, ","))
			fmt.Fprintf(out, "requiredScopes: %s\n", strings.Join(tool.RequiredScopes, ","))
			required, fields := toolSchemaSummary(tool.InputSchema)
			fmt.Fprintf(out, "inputRequired: %s\n", strings.Join(required, ","))
			fmt.Fprintf(out, "inputFields: %s\n", strings.Join(fields, ","))
			outputRequired, outputFields := toolSchemaSummary(tool.OutputSchema)
			fmt.Fprintf(out, "outputRequired: %s\n", strings.Join(outputRequired, ","))
			fmt.Fprintf(out, "outputFields: %s\n", strings.Join(outputFields, ","))
			fmt.Fprintln(out, "hint: if invocation is denied, inspect MCP tool grants, AI access policies, skill bindings, resource scopes, and domain permission keys.")
			return
		}
	}
	fmt.Fprintf(out, "tool: %s not visible\n", toolName)
	fmt.Fprintln(out, "hint: check ai.gateway.invoke, domain permission keys, MCP tool grants, AI access policies, skill bindings, AI client context, and resource scopes.")
}

func toolSchemaSummary(schema map[string]any) ([]string, []string) {
	if len(schema) == 0 {
		return nil, nil
	}
	required := stringSliceFromAny(schema["required"])
	properties, _ := schema["properties"].(map[string]any)
	fields := make([]string, 0, len(properties))
	for key := range properties {
		key = strings.TrimSpace(key)
		if key != "" {
			fields = append(fields, key)
		}
	}
	sort.Strings(required)
	sort.Strings(fields)
	return required, fields
}

func stringSliceFromAny(value any) []string {
	switch typed := value.(type) {
	case []string:
		out := append([]string(nil), typed...)
		sort.Strings(out)
		return out
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			text := strings.TrimSpace(fmt.Sprint(item))
			if text != "" {
				out = append(out, text)
			}
		}
		sort.Strings(out)
		return out
	default:
		return nil
	}
}

func diagnoseResource(out io.Writer, manifest Manifest, resourceName string) {
	for _, resource := range manifest.Resources {
		if resource.Name == resourceName {
			fmt.Fprintf(out, "resource: %s\n", resource.Name)
			fmt.Fprintf(out, "description: %s\n", resource.Description)
			fmt.Fprintf(out, "requiredPermissionKeys: %s\n", strings.Join(resource.PermissionKeys, ","))
			fmt.Fprintf(out, "requiredScopes: %s\n", strings.Join(resource.RequiredScopes, ","))
			contextRequired, contextFields := toolSchemaSummary(resource.ContextSchema)
			fmt.Fprintf(out, "contextRequired: %s\n", strings.Join(contextRequired, ","))
			fmt.Fprintf(out, "contextFields: %s\n", strings.Join(contextFields, ","))
			fmt.Fprintln(out, "hint: resource reads proxy to Gateway resources/read; check ai.gateway.invoke, resource permission keys, skill bindings, AI client context, and context scope fields.")
			return
		}
	}
	fmt.Fprintf(out, "resource: %s not visible\n", resourceName)
	fmt.Fprintln(out, "hint: check ai.gateway.invoke, resource permission keys, skill bindings, AI client context, and the manifest resource URI.")
}

func diagnosePrompt(out io.Writer, manifest Manifest, promptName string) {
	for _, prompt := range manifest.Prompts {
		if prompt.Name == promptName {
			fmt.Fprintf(out, "prompt: %s\n", prompt.Name)
			fmt.Fprintf(out, "description: %s\n", prompt.Description)
			fmt.Fprintf(out, "requiredPermissionKeys: %s\n", strings.Join(prompt.PermissionKeys, ","))
			fmt.Fprintf(out, "requiredScopes: %s\n", strings.Join(prompt.RequiredScopes, ","))
			argumentRequired, argumentFields := toolSchemaSummary(prompt.ArgumentSchema)
			contextRequired, contextFields := toolSchemaSummary(prompt.ContextSchema)
			fmt.Fprintf(out, "argumentRequired: %s\n", strings.Join(argumentRequired, ","))
			fmt.Fprintf(out, "argumentFields: %s\n", strings.Join(argumentFields, ","))
			fmt.Fprintf(out, "contextRequired: %s\n", strings.Join(contextRequired, ","))
			fmt.Fprintf(out, "contextFields: %s\n", strings.Join(contextFields, ","))
			fmt.Fprintln(out, "hint: prompt reads proxy to Gateway prompts/get; check ai.gateway.invoke, prompt permission keys, skill bindings, AI client context, and prompt arguments/context.")
			return
		}
	}
	fmt.Fprintf(out, "prompt: %s not visible\n", promptName)
	fmt.Fprintln(out, "hint: check ai.gateway.invoke, prompt permission keys, skill bindings, AI client context, and the manifest prompt name.")
}

func extractLeadingToolName(args []string) (string, []string) {
	return extractLeadingValue(args)
}

func extractLeadingValue(args []string) (string, []string) {
	if len(args) == 0 || strings.HasPrefix(strings.TrimSpace(args[0]), "-") {
		return "", args
	}
	out := append([]string(nil), args[1:]...)
	return strings.TrimSpace(args[0]), out
}

func firstArg(values []string, fallback string) string {
	if len(values) == 0 {
		return fallback
	}
	return values[0]
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func approvalText(value bool) string {
	if value {
		return "approval"
	}
	return "direct"
}

func env(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

const bashCompletionScript = `#!/usr/bin/env bash
_soha_cli_completion() {
  local cur prev commands
  COMPREPLY=()
  cur="${COMP_WORDS[COMP_CWORD]}"
  prev="${COMP_WORDS[COMP_CWORD-1]}"
  commands="login capabilities tool resource prompt token service-account audit approval governance profile context mcp skill diagnose completion help"
  case "${COMP_WORDS[1]}" in
    tool)
      COMPREPLY=($(compgen -W "call" -- "$cur"))
      return 0
      ;;
    resource)
      COMPREPLY=($(compgen -W "read" -- "$cur"))
      return 0
      ;;
    prompt)
      COMPREPLY=($(compgen -W "get" -- "$cur"))
      return 0
      ;;
    token)
      COMPREPLY=($(compgen -W "list create revoke" -- "$cur"))
      return 0
      ;;
    service-account)
      COMPREPLY=($(compgen -W "list create token-list token-create token-revoke" -- "$cur"))
      return 0
      ;;
    audit)
      COMPREPLY=($(compgen -W "list" -- "$cur"))
      return 0
      ;;
    approval)
      COMPREPLY=($(compgen -W "list timeline approve reject cancel" -- "$cur"))
      return 0
      ;;
    governance)
      COMPREPLY=($(compgen -W "status" -- "$cur"))
      return 0
      ;;
    profile)
      COMPREPLY=($(compgen -W "list show use" -- "$cur"))
      return 0
      ;;
    context)
      COMPREPLY=($(compgen -W "show set" -- "$cur"))
      return 0
      ;;
    mcp)
      COMPREPLY=($(compgen -W "start install" -- "$cur"))
      return 0
      ;;
    skill)
      COMPREPLY=($(compgen -W "list install" -- "$cur"))
      return 0
      ;;
  esac
  COMPREPLY=($(compgen -W "$commands" -- "$cur"))
}
complete -F _soha_cli_completion soha-cli
`
