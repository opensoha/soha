package sohacli

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
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
	fmt.Fprintln(out, "  profile        List, show, or switch profiles")
	fmt.Fprintln(out, "  context        Show or update AI client context headers")
	fmt.Fprintln(out, "  mcp start      Run the soha MCP stdio server")
	fmt.Fprintln(out, "  mcp install    Print MCP client configuration")
	fmt.Fprintln(out, "  skill list     List local soha AI Gateway skill files")
	fmt.Fprintln(out, "  skill install  Install local soha AI Gateway skill files")
	fmt.Fprintln(out, "  diagnose       Check profile and Gateway connectivity")
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
	format := fs.String("output", "json", "output format: json or names")
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
	default:
		return fmt.Errorf("unsupported output format %q", *format)
	}
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
	if err := fs.Parse(args); err != nil {
		return err
	}
	_, name, profile, err := loadRuntimeProfile(rt, *profileFlag)
	if err != nil {
		return err
	}
	manifest, err := gatewayClient(rt, profile).Capabilities(ctx, gatewayHeaders(profile, "", "", "", ""))
	if err != nil {
		return err
	}
	fmt.Fprintf(rt.Out, "profile: %s\nserver: %s\nuser: %s\n", name, profile.ServerURL, profile.UserName)
	fmt.Fprintf(rt.Out, "tools: %d\nresources: %d\nprompts: %d\nskills: %d\n", len(manifest.Tools), len(manifest.Resources), len(manifest.Prompts), len(manifest.Skills))
	return nil
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
