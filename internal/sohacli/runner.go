package sohacli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	sensitiveAuthorizationTextPattern = regexp.MustCompile(`(?i)(["']?)(authorization)(["']?\s*[:=]\s*)(?:"[^"\r\n]*"|'[^'\r\n]*'|Bearer\s+[^\s,;]+|[^\s,;]+)`)
	sensitiveAssignmentTextPattern    = regexp.MustCompile(`(?i)(["']?)(token|password|passwd|secret|api[_-]?key|device[_-]?code|user[_-]?code|verification[_-]?handle|code[_-]?verifier|confirmation[_-]?id)(["']?\s*[:=]\s*)(?:"[^"\r\n]*"|'[^'\r\n]*'|[^\s,;]+)`)
	bearerCredentialTextPattern       = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]+`)
)

type Runtime struct {
	In          io.Reader
	Out         io.Writer
	Err         io.Writer
	ConfigPath  string
	HTTPClient  *http.Client
	HTTPTimeout time.Duration
	Keyring     SecretKeyring
	OpenBrowser func(string) error
	// Process signals are installed only while device login can cancel a grant.
	DeviceLoginContext func(context.Context) (context.Context, context.CancelFunc)
	// Terminal signals are scoped to an active native terminal so raw mode can restore.
	TerminalContext func(context.Context) (context.Context, context.CancelFunc)
	// Stream signals detach the reader without implicitly canceling a durable run.
	StreamingContext func(context.Context) (context.Context, context.CancelFunc)
	// Scoped fault injection for credential commit durability; production uses saveConfig.
	credentialConfigWrite func(string, Config) error
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
	var err error
	args, rt.HTTPTimeout, err = resolveRuntimeTimeout(args, rt.HTTPTimeout)
	if err != nil {
		_, _ = fmt.Fprintln(rt.Err, "error:", terminalDisplayText(redactSensitiveText(err.Error())))
		return 2
	}
	if len(args) == 0 {
		if err := printUsage(rt.Err); err != nil {
			return 1
		}
		return 2
	}
	cmd := args[0]
	if cmd == "help" {
		if len(args) == 1 {
			if err := printUsage(rt.Out); err != nil {
				return 1
			}
			return 0
		}
		if printCommandHelp(args[1:], rt.Out) {
			return 0
		}
		_, _ = fmt.Fprintf(rt.Err, "error: unknown help topic %q\n", strings.Join(args[1:], " "))
		return 2
	}
	if hasHelpArg(args) && printCommandHelp(args, rt.Out) {
		return 0
	}
	switch cmd {
	case "-h", "--help":
		if err := printUsage(rt.Out); err != nil {
			return 1
		}
		return 0
	default:
		err = dispatchTopLevelCommand(ctx, cmd, args[1:], rt)
	}
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		if errors.Is(err, context.Canceled) {
			_, _ = fmt.Fprintln(rt.Err, "error: interrupted")
			return 130
		}
		_, _ = fmt.Fprintln(rt.Err, "error:", terminalDisplayText(redactSensitiveText(err.Error())))
		if isUsageError(err) {
			return 2
		}
		var remoteExit remoteProcessExitError
		if errors.As(err, &remoteExit) && remoteExit.code > 0 && remoteExit.code <= 255 {
			return int(remoteExit.code)
		}
		return 1
	}
	return 0
}

type usageError struct{ message string }

func (e usageError) Error() string { return e.message }

func isUsageError(err error) bool {
	var usage usageError
	if errors.As(err, &usage) {
		return true
	}
	message := err.Error()
	return strings.HasPrefix(message, "flag provided but not defined:") ||
		strings.HasPrefix(message, "invalid value ") ||
		strings.HasPrefix(message, "bad flag syntax:")
}

func printUsage(destination io.Writer) error {
	out := newCheckedWriter(destination)
	out.Println("Usage: soha <command> [options]")
	out.Println()
	out.Println("Global options:")
	out.Println("  --timeout <duration>  HTTP request timeout, e.g. 10s or 1m (default 30s)")
	out.Println()
	out.Println("Commands:")
	for _, spec := range topLevelCommandSpecs {
		out.Printf("  %-15s %s\n", spec.Name, spec.Summary)
	}
	return out.Err()
}

func printCommandHelp(path []string, out io.Writer) bool {
	spec, ok := findCommandSpec(path)
	if !ok {
		return false
	}
	writer := newCheckedWriter(out)
	writer.Println("Usage:", spec.Usage)
	writer.Println()
	writer.Println(spec.Summary)
	if len(spec.Subcommands) > 0 {
		writer.Println()
		writer.Println("Subcommands:")
		for _, subcommand := range spec.Subcommands {
			writer.Printf("  %-15s %s\n", subcommand.Name, subcommand.Summary)
		}
	}
	if len(spec.Examples) > 0 {
		writer.Println()
		writer.Println("Examples:")
		for _, example := range spec.Examples {
			writer.Println(" ", example)
		}
	}
	return true
}

func newRuntimeFlagSet(name string, args []string, rt Runtime) *flag.FlagSet {
	return newFlagSet(name, flagOutput(args, rt))
}

func flagOutput(args []string, rt Runtime) io.Writer {
	if hasHelpArg(args) {
		return rt.Out
	}
	return rt.Err
}

func hasHelpArg(args []string) bool {
	for _, arg := range args {
		if arg == "--" {
			return false
		}
		if isHelpArg(arg) {
			return true
		}
	}
	return false
}

func isHelpArg(arg string) bool {
	arg = strings.TrimSpace(arg)
	return arg == "-h" || arg == "--help"
}

func runVersion(args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("version", args, rt)
	jsonOutput := fs.Bool("json", false, "print JSON output")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return writeVersion(rt.Out, *jsonOutput)
}

func runCapabilities(ctx context.Context, args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("capabilities", args, rt)
	profileFlag := fs.String("profile", "", "profile name")
	query := fs.String("query", "", "match capability names, descriptions and effects")
	toolDomain := fs.String("tool-domain", "", "owning domain such as docker or delivery")
	action := fs.String("action", "", "capability action")
	resourceKind := fs.String("resource-kind", "", "required input resource kind")
	limit := fs.Int("limit", 0, "gateway page size, from 1 to 100; 0 preserves the legacy full catalog")
	cursor := fs.String("cursor", "", "opaque gateway catalog cursor")
	domain := fs.String("domain", "gateway", "capability domain: gateway or platform")
	format := fs.String("output", "json", "output format: json, yaml, names, or inputs")
	jsonOutput := fs.Bool("json", false, "print JSON output")
	aiClientID := fs.String("ai-client-id", "", "override AI client id")
	aiClientName := fs.String("ai-client", "", "override AI client display name")
	skillID := fs.String("skill-id", "", "override skill id")
	source := fs.String("source", "", "override source label")
	if err := fs.Parse(args); err != nil {
		return err
	}
	domainValue, err := normalizeCapabilityDomain(*domain)
	if err != nil {
		return err
	}
	if *jsonOutput {
		*format = "json"
	}
	formatValue, err := normalizeOutputFormat(*format, "json", "yaml", "names", "inputs")
	if err != nil {
		return err
	}
	_, name, profile, err := loadRuntimeProfile(ctx, rt, *profileFlag)
	if err != nil {
		return err
	}
	if domainValue == "platform" {
		if *query != "" || *toolDomain != "" || *action != "" || *resourceKind != "" || *cursor != "" || *limit != 0 {
			return fmt.Errorf("capability search filters require --domain gateway")
		}
		return runPlatformCapabilities(
			ctx,
			rt,
			name,
			profile,
			formatValue,
			gatewayHeaders(profile, *aiClientID, *aiClientName, *skillID, *source),
		)
	}
	if *limit < 0 || *limit > 100 {
		return fmt.Errorf("--limit must be between 0 and 100")
	}
	filters := url.Values{}
	for key, value := range map[string]string{"query": *query, "toolDomain": *toolDomain, "action": *action, "resourceKind": *resourceKind, "cursor": *cursor} {
		if value != "" {
			filters.Set(key, value)
		}
	}
	if *limit > 0 {
		filters.Set("limit", strconv.Itoa(*limit))
	}
	manifest, err := gatewayClient(rt, profile).SearchCapabilities(ctx, gatewayHeaders(profile, *aiClientID, *aiClientName, *skillID, *source), filters)
	if err != nil {
		return err
	}
	switch formatValue {
	case "", "json":
		return writePrettyJSON(rt.Out, manifest)
	case "yaml":
		return writeYAML(rt.Out, manifest)
	case "names":
		out := newCheckedWriter(rt.Out)
		out.Printf("profile: %s\n", name)
		for _, tool := range manifest.Tools {
			out.Printf("tool\t%s\t%s\t%s\n", tool.Name, tool.RiskLevel, approvalText(tool.RequiresApproval))
		}
		for _, item := range manifest.Resources {
			out.Printf("resource\t%s\n", item.Name)
		}
		for _, item := range manifest.Prompts {
			out.Printf("prompt\t%s\n", item.Name)
		}
		for _, item := range manifest.Skills {
			out.Printf("skill\t%s\t%s\n", item.ID, item.Name)
		}
		return out.Err()
	case "inputs":
		out := newCheckedWriter(rt.Out)
		out.Printf("profile: %s\n", name)
		for _, tool := range manifest.Tools {
			required, fields := toolSchemaSummary(tool.InputSchema)
			outputRequired, outputFields := toolSchemaSummary(tool.OutputSchema)
			out.Printf("tool\t%s\trequired=%s\tfields=%s\toutputRequired=%s\toutputFields=%s\n", tool.Name, strings.Join(required, ","), strings.Join(fields, ","), strings.Join(outputRequired, ","), strings.Join(outputFields, ","))
		}
		return out.Err()
	default:
		return fmt.Errorf("unsupported output format %q", *format)
	}
}

func runPlatformCapabilities(ctx context.Context, rt Runtime, profileName string, profile ProfileConfig, formatValue string, headers map[string]string) error {
	items, err := gatewayClient(rt, profile).ClusterCapabilities(ctx, headers)
	if err != nil {
		return err
	}
	sortClusterCapabilities(items)
	switch formatValue {
	case "", "json":
		return writePrettyJSON(rt.Out, items)
	case "yaml":
		return writeYAML(rt.Out, items)
	case "names":
		out := newCheckedWriter(rt.Out)
		out.Printf("profile: %s\n", profileName)
		for _, item := range items {
			out.Printf("capability\t%s\t%s\t%s\tscopes=%s\tdirect=%s\tagent=%s",
				item.Key,
				item.RiskLevel,
				approvalText(item.RequiresApproval),
				strings.Join(item.RequiredScopes, ","),
				clusterCapabilitySupportText(item.Direct),
				clusterCapabilitySupportText(item.Agent),
			)
			if reason := strings.TrimSpace(item.Agent.Reason); reason != "" {
				out.Printf("\treason=%s", redactSensitiveText(reason))
			}
			out.Println()
		}
		return out.Err()
	case "inputs":
		return fmt.Errorf("platform capabilities do not expose input schemas; use --output names, json, or yaml")
	default:
		return fmt.Errorf("unsupported output format %q", formatValue)
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
	fs := newRuntimeFlagSet("tool call", args, rt)
	profileFlag := fs.String("profile", "", "profile name")
	inputPath := fs.String("input", "", "JSON input file path, or - for stdin")
	inputJSON := fs.String("input-json", "", "inline JSON tool input")
	aiClientID := fs.String("ai-client-id", "", "override AI client id")
	aiClientName := fs.String("ai-client", "", "override AI client display name")
	skillID := fs.String("skill-id", "", "override skill id")
	source := fs.String("source", "", "override source label")
	yes := fs.Bool("yes", false, "skip confirmation for protected tools")
	preview := fs.Bool("preview", false, "print a redacted request preview without invoking the tool")
	version := fs.String("capability-version", "", "require a previously discovered capability version")
	secretRefFlags := repeatableFlag{}
	fs.Var(&secretRefFlags, "secret-ref", "secret reference as ALIAS=soha://secrets/ID[/versions/N], repeatable")
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
	_, _, profile, err := loadRuntimeProfile(ctx, rt, *profileFlag)
	if err != nil {
		return err
	}
	input, err := readToolInput(rt, *inputPath, *inputJSON)
	if err != nil {
		return err
	}
	secretRefs, err := parseSecretRefFlags(secretRefFlags)
	if err != nil {
		return err
	}
	client := gatewayClient(rt, profile)
	headers := gatewayHeaders(profile, *aiClientID, *aiClientName, *skillID, *source)
	manifest, err := client.Capabilities(ctx, headers)
	if err != nil {
		return err
	}
	tool, ok := findToolCapability(manifest, toolName)
	if !ok {
		return fmt.Errorf("tool %q is not available in the Gateway manifest", toolName)
	}
	if *version != "" && *version != tool.Version {
		return fmt.Errorf("capability version changed or is unavailable for %q; rediscover before executing", toolName)
	}
	requestPreview := map[string]any{
		"tool": toolName, "riskLevel": tool.RiskLevel,
		"requiresApproval": tool.RequiresApproval, "input": sanitizeCLIValue(input),
	}
	if len(secretRefs) > 0 {
		requestPreview["secretRefs"] = secretRefs
	}
	if tool.Version != "" {
		requestPreview["capabilityVersion"] = tool.Version
	}
	if *preview {
		return writePrettyJSON(rt.Out, requestPreview)
	}
	if protectedTool(tool) && !*yes {
		confirmed, err := confirmAction(rt, fmt.Sprintf("Invoke protected tool %s (risk=%s)?", toolName, tool.RiskLevel))
		if err != nil {
			return err
		}
		if !confirmed {
			return fmt.Errorf("tool invocation declined; pass --yes for non-interactive use")
		}
	}
	result, err := client.InvokeCapability(ctx, tool, input, "", secretRefs, headers)
	if err != nil {
		return err
	}
	return writePrettyJSON(rt.Out, sanitizeCLIValue(result))
}

func findToolCapability(manifest Manifest, name string) (ToolCapability, bool) {
	for _, tool := range manifest.Tools {
		if tool.Name == name {
			return tool, true
		}
	}
	return ToolCapability{}, false
}

func protectedTool(tool ToolCapability) bool {
	if tool.RequiresApproval {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(tool.RiskLevel)) {
	case "read", "analyze":
		return false
	default:
		return true
	}
}

func confirmAction(rt Runtime, prompt string) (bool, error) {
	if _, err := fmt.Fprint(rt.Err, strings.TrimSpace(prompt)+" [y/N]: "); err != nil {
		return false, err
	}
	line, err := bufio.NewReader(rt.In).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
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
	fs := newRuntimeFlagSet("resource read", args, rt)
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
	_, _, profile, err := loadRuntimeProfile(ctx, rt, *profileFlag)
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
	fs := newRuntimeFlagSet("prompt get", args, rt)
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
	_, _, profile, err := loadRuntimeProfile(ctx, rt, *profileFlag)
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

func resolveRuntimeTimeout(args []string, runtimeTimeout time.Duration) ([]string, time.Duration, error) {
	stripped, flagValue, err := extractTimeoutFlag(args)
	if err != nil {
		return nil, 0, err
	}
	if flagValue != "" {
		timeout, err := parseHTTPTimeout("--timeout", flagValue)
		if err != nil {
			return nil, 0, err
		}
		return stripped, timeout, nil
	}
	if runtimeTimeout > 0 {
		return stripped, runtimeTimeout, nil
	}
	if value := env("SOHA_HTTP_TIMEOUT"); value != "" {
		timeout, err := parseHTTPTimeout("SOHA_HTTP_TIMEOUT", value)
		if err != nil {
			return nil, 0, err
		}
		return stripped, timeout, nil
	}
	return stripped, 0, nil
}

func extractTimeoutFlag(args []string) ([]string, string, error) {
	out := make([]string, 0, len(args))
	value := ""
	for index := 0; index < len(args); index++ {
		arg := strings.TrimSpace(args[index])
		switch {
		case arg == "--":
			out = append(out, args[index:]...)
			return out, value, nil
		case arg == "--timeout":
			if index+1 >= len(args) {
				return nil, "", fmt.Errorf("--timeout requires a duration")
			}
			index++
			value = strings.TrimSpace(args[index])
		case strings.HasPrefix(arg, "--timeout="):
			value = strings.TrimSpace(strings.TrimPrefix(arg, "--timeout="))
		default:
			out = append(out, args[index])
		}
	}
	return out, value, nil
}

func parseHTTPTimeout(source, value string) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("%s requires a duration", source)
	}
	timeout, err := time.ParseDuration(value)
	if err != nil {
		seconds, secondsErr := strconv.ParseFloat(value, 64)
		if secondsErr != nil {
			return 0, fmt.Errorf("invalid %s %q; use a Go duration like 30s or a number of seconds", source, value)
		}
		timeout = time.Duration(seconds * float64(time.Second))
	}
	if timeout <= 0 {
		return 0, fmt.Errorf("%s must be greater than 0", source)
	}
	return timeout, nil
}

func loadRuntimeProfile(ctx context.Context, rt Runtime, requested string) (Config, string, ProfileConfig, error) {
	return loadRuntimeProfileForServer(ctx, rt, requested, env("SOHA_SERVER"))
}

func loadRuntimeProfileForServer(ctx context.Context, rt Runtime, requested, serverOverride string) (Config, string, ProfileConfig, error) {
	return loadRuntimeProfileWithToken(ctx, rt, requested, serverOverride, env("SOHA_TOKEN"))
}
func loadRuntimeProfileWithToken(ctx context.Context, rt Runtime, requested, serverOverride, overrideToken string) (Config, string, ProfileConfig, error) {
	token := strings.TrimSpace(overrideToken)
	server := normalizeServerURL(serverOverride)
	if server != "" && !sameServerURL(server, server) {
		return Config{}, "", ProfileConfig{}, fmt.Errorf("invalid_server_url: use an HTTP(S) server URL without credentials, query or fragment")
	}
	if token != "" && server != "" {
		name := profileName(requested)
		return Config{CurrentProfile: name, Profiles: map[string]ProfileConfig{}}, name, ProfileConfig{ServerURL: server, AccessToken: token, Source: "soha", runtimeName: name}, nil
	}
	cfg, err := loadConfig(rt.ConfigPath)
	if err != nil {
		return Config{}, "", ProfileConfig{}, err
	}
	name := profileName(firstNonEmptyString(requested, cfg.CurrentProfile))
	profile := cfg.Profiles[name]
	if token != "" {
		profile.ServerURL = firstNonEmptyString(server, profile.ServerURL, defaultServerURL)
		profile.AccessToken = token
		profile.RefreshToken = ""
		profile.ExpiresAt = time.Time{}
		profile.UserID, profile.UserName = "", ""
		profile.CredentialRef = ""
		profile.CredentialStore = ""
		profile.CredentialKind = ""
		profile.ProtocolVersion = 0
		profile.credential = nil
	} else {
		name, profile, err = resolveProfile(cfg, requested)
		if err != nil {
			return Config{}, "", ProfileConfig{}, err
		}
		if !sameServerURL(profile.ServerURL, profile.ServerURL) {
			return Config{}, "", ProfileConfig{}, fmt.Errorf("invalid_server_url: profile server URL is not a valid credential origin")
		}
		if server != "" && !sameServerURL(server, profile.ServerURL) {
			return Config{}, "", ProfileConfig{}, fmt.Errorf("server_mismatch: profile %q belongs to another server; select a matching profile or log in to the requested server", name)
		}
		profile, err = readStableProfile(ctx, rt, name, profile.ServerURL)
		if err != nil {
			return Config{}, "", ProfileConfig{}, err
		}
		if accessTokenExpired(profile, time.Now()) {
			profile, err = refreshRuntimeProfile(ctx, rt, cfg, name, profile, true)
			if err != nil {
				return Config{}, "", ProfileConfig{}, err
			}
		}
	}
	if strings.TrimSpace(profile.Source) == "" {
		profile.Source = "soha"
	}
	profile.runtimeName = name
	profile.refreshEnabled = token == "" && strings.TrimSpace(profile.RefreshToken) != ""
	profile.refreshPersist = token == ""
	return cfg, name, profile, nil
}

func accessTokenExpired(profile ProfileConfig, now time.Time) bool {
	return !profile.ExpiresAt.IsZero() && !profile.ExpiresAt.After(now)
}

func refreshRuntimeProfile(ctx context.Context, rt Runtime, cfg Config, name string, profile ProfileConfig, persist bool) (ProfileConfig, error) {
	if persist {
		return refreshStoredProfile(ctx, rt, name, profile, false)
	}
	result, err := (APIClient{ServerURL: profile.ServerURL, Client: rt.HTTPClient, Timeout: rt.HTTPTimeout}).Refresh(ctx, profile.RefreshToken)
	if err != nil {
		return ProfileConfig{}, refreshFailure(err)
	}
	return profileFromAuthResult(profile, result, time.Now())
}

func profileFromAuthResult(profile ProfileConfig, result loginResponse, now time.Time) (ProfileConfig, error) {
	tokens := result.Data.Tokens
	if strings.TrimSpace(tokens.AccessToken) == "" {
		return ProfileConfig{}, fmt.Errorf("auth response did not include an access token")
	}
	profile.AccessToken = tokens.AccessToken
	profile.RefreshToken = firstNonEmptyString(tokens.RefreshToken, profile.RefreshToken)
	profile.ExpiresAt = tokenExpiresAt(tokens.ExpiresAt, tokens.ExpiresIn, now)
	profile.UserID = firstNonEmptyString(result.Data.User.UserID, profile.UserID)
	profile.UserName = firstNonEmptyString(result.Data.User.UserName, profile.UserName)
	return profile, nil
}

func tokenExpiresAt(expiresAt time.Time, expiresIn int64, now time.Time) time.Time {
	if !expiresAt.IsZero() {
		return expiresAt
	}
	if expiresIn > 0 {
		return now.Add(time.Duration(expiresIn) * time.Second)
	}
	return time.Time{}
}

func gatewayClient(rt Runtime, profile ProfileConfig) APIClient {
	client := APIClient{ServerURL: profile.ServerURL, Token: profile.AccessToken, RefreshToken: profile.RefreshToken, Client: rt.HTTPClient, Timeout: rt.HTTPTimeout}
	if !profile.refreshEnabled {
		return client
	}
	client.authState = &apiClientAuthState{AccessToken: profile.AccessToken, RefreshToken: profile.RefreshToken}
	client.refreshSession = func(ctx context.Context, sent string) (refreshResponse, error) {
		expected := profile
		expected.AccessToken = sent
		updated, err := refreshStoredProfile(ctx, rt, profile.runtimeName, expected, true)
		if err != nil {
			return refreshResponse{}, err
		}
		return authResultFromProfile(updated), nil
	}
	return client
}

func gatewayHeaders(profile ProfileConfig, aiClientID, aiClientName, skillID, source string) map[string]string {
	return map[string]string{
		"X-Soha-AI-Client-ID": firstNonEmptyString(aiClientID, profile.AIClientID),
		"X-Soha-AI-Client":    firstNonEmptyString(aiClientName, profile.AIClientName),
		"X-Soha-Skill-ID":     firstNonEmptyString(skillID, profile.SkillID),
		"X-Soha-Source":       firstNonEmptyString(source, profile.Source, "soha"),
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

func normalizeOutputFormat(format string, allowed ...string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(format))
	if normalized == "" && len(allowed) > 0 {
		normalized = allowed[0]
	}
	for _, item := range allowed {
		if normalized == item {
			return normalized, nil
		}
	}
	return "", fmt.Errorf("unsupported output format %q", format)
}

func normalizeCapabilityDomain(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "gateway", "ai-gateway", "aigateway":
		return "gateway", nil
	case "platform", "cluster", "clusters":
		return "platform", nil
	default:
		return "", fmt.Errorf("unsupported capability domain %q", value)
	}
}

func writeStructuredOutput(out io.Writer, format string, value any) error {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "json":
		return writePrettyJSON(out, value)
	case "yaml":
		return writeYAML(out, value)
	default:
		return fmt.Errorf("unsupported output format %q", format)
	}
}

func writeYAML(out io.Writer, value any) error {
	normalized, err := normalizeYAMLValue(value)
	if err != nil {
		return err
	}
	if err := writeYAMLValue(out, normalized, 0); err != nil {
		return err
	}
	_, err = fmt.Fprintln(out)
	return err
}

func normalizeYAMLValue(value any) (any, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var normalized any
	if err := json.Unmarshal(raw, &normalized); err != nil {
		return nil, err
	}
	return normalized, nil
}

func writeYAMLValue(out io.Writer, value any, indent int) error {
	prefix := strings.Repeat(" ", indent)
	switch typed := value.(type) {
	case map[string]any:
		if len(typed) == 0 {
			_, err := fmt.Fprintf(out, "%s{}\n", prefix)
			return err
		}
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			item := typed[key]
			if yamlInlineValue(item) {
				if _, err := fmt.Fprintf(out, "%s%s: %s\n", prefix, yamlKey(key), yamlScalar(item)); err != nil {
					return err
				}
				continue
			}
			if _, err := fmt.Fprintf(out, "%s%s:\n", prefix, yamlKey(key)); err != nil {
				return err
			}
			if err := writeYAMLValue(out, item, indent+2); err != nil {
				return err
			}
		}
		return nil
	case []any:
		if len(typed) == 0 {
			_, err := fmt.Fprintf(out, "%s[]\n", prefix)
			return err
		}
		for _, item := range typed {
			if yamlInlineValue(item) {
				if _, err := fmt.Fprintf(out, "%s- %s\n", prefix, yamlScalar(item)); err != nil {
					return err
				}
				continue
			}
			if _, err := fmt.Fprintf(out, "%s-\n", prefix); err != nil {
				return err
			}
			if err := writeYAMLValue(out, item, indent+2); err != nil {
				return err
			}
		}
		return nil
	default:
		_, err := fmt.Fprintf(out, "%s%s\n", prefix, yamlScalar(typed))
		return err
	}
}

func yamlInlineValue(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		return len(typed) == 0
	case []any:
		return len(typed) == 0
	default:
		return true
	}
}

func yamlKey(value string) string {
	if yamlBareString(value) {
		return value
	}
	return strconv.Quote(value)
}

func yamlScalar(value any) string {
	switch typed := value.(type) {
	case nil:
		return "null"
	case string:
		if yamlBareString(typed) {
			return typed
		}
		return strconv.Quote(typed)
	case bool:
		return strconv.FormatBool(typed)
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case json.Number:
		return typed.String()
	case map[string]any:
		if len(typed) == 0 {
			return "{}"
		}
	case []any:
		if len(typed) == 0 {
			return "[]"
		}
	}
	return strconv.Quote(fmt.Sprint(value))
}

func yamlBareString(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	switch strings.ToLower(value) {
	case "null", "true", "false", "yes", "no", "on", "off", "~":
		return false
	}
	if strings.HasPrefix(value, "-") {
		return false
	}
	for _, char := range value {
		switch char {
		case ' ', '\t', '\n', '\r', ':', '#', '{', '}', '[', ']', '&', '*', '?', '|', '<', '>', '=', '!', '%', '@', '`', '"', '\'':
			return false
		}
	}
	return true
}

func readToolInput(rt Runtime, path, inline string) (map[string]any, error) {
	return readJSONInput(rt, path, inline)
}

func readJSONInput(rt Runtime, path, inline string) (map[string]any, error) {
	const maximum = 10 << 20
	if strings.TrimSpace(inline) != "" {
		if len(inline) > maximum {
			return nil, fmt.Errorf("JSON input exceeds 10 MiB")
		}
		return parseJSONObject([]byte(inline))
	}
	if strings.TrimSpace(path) == "" {
		return map[string]any{}, nil
	}
	reader := rt.In
	if strings.TrimSpace(path) != "-" {
		// #nosec G304 -- the caller explicitly supplies the input file.
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer func() { _ = file.Close() }()
		reader = file
	}
	raw, err := io.ReadAll(io.LimitReader(reader, maximum+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maximum {
		return nil, fmt.Errorf("JSON input exceeds 10 MiB")
	}
	return parseJSONObject(raw)
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
		typed.Output = sanitizeCLIValue(typed.Output)
		typed.RelatedIDs = sanitizeCLIMap(typed.RelatedIDs)
		typed.Audit = sanitizeCLIMap(typed.Audit)
		typed.AdditionalProperties = sanitizeCLIMap(typed.AdditionalProperties)
		if typed.Task != nil {
			task := *typed.Task
			task.Status = redactSensitiveText(task.Status)
			task.StatusCall.Input = sanitizeCLIMap(task.StatusCall.Input)
			typed.Task = &task
		}
		return typed
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
			trace := sanitizeAs(*typed.ApprovalTrace)
			typed.ApprovalTrace = &trace
		}
		typed.Output = sanitizeCLIValue(typed.Output)
		typed.Summary = redactSensitiveText(typed.Summary)
		typed.DecisionComment = redactSensitiveText(typed.DecisionComment)
		return typed
	case ApprovalDecisionResult:
		typed.Request = sanitizeAs(typed.Request)
		if typed.Invocation != nil {
			invocation := sanitizeAs(*typed.Invocation)
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
		typed.Request = sanitizeAs(typed.Request)
		if typed.Trace != nil {
			trace := sanitizeAs(*typed.Trace)
			typed.Trace = &trace
		}
		for index := range typed.Events {
			typed.Events[index] = sanitizeAs(typed.Events[index])
		}
		return typed
	case GovernanceStatus:
		return sanitizeGovernanceStatus(typed)
	case PluginManifest:
		return sanitizeCLIJSONValue(typed)
	case MarketplacePlugin:
		return sanitizeCLIJSONValue(typed)
	case InstalledPlugin:
		return sanitizeCLIJSONValue(typed)
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
	case []MarketplacePlugin:
		return sanitizeCLIJSONValue(typed)
	case []InstalledPlugin:
		return sanitizeCLIJSONValue(typed)
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

func sanitizeAs[T any](value T) T {
	sanitized, ok := sanitizeCLIValue(value).(T)
	if !ok {
		return value
	}
	return sanitized
}

func sanitizeCLIJSONValue(value any) any {
	raw, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var normalized any
	if err := json.Unmarshal(raw, &normalized); err != nil {
		return value
	}
	return sanitizeCLIValue(normalized)
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
	value = sensitiveAuthorizationTextPattern.ReplaceAllString(value, `$1$2$3[REDACTED]`)
	value = sensitiveAssignmentTextPattern.ReplaceAllString(value, `$1$2$3[REDACTED]`)
	value = bearerCredentialTextPattern.ReplaceAllString(value, "Bearer [REDACTED]")
	return value
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
