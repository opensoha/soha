package sohacli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type addTargetSpec struct {
	Name           string
	DisplayName    string
	ConfigKind     string
	DefaultConfig  func() string
	DefaultSkill   func() string
	JSONC          bool
	JSONDisabled   bool
	RestartMessage string
}

type setupMode string

const (
	setupModeMCP   setupMode = "mcp"
	setupModeSkill setupMode = "skill"
	setupModeBoth  setupMode = "both"
)

func runSetup(ctx context.Context, args []string, rt Runtime) error {
	client, remaining, err := extractSetupClient(args)
	if err != nil {
		return err
	}
	if client == "" {
		return runAdd(ctx, remaining, rt)
	}
	if len(remaining) > 0 && !strings.HasPrefix(strings.TrimSpace(remaining[0]), "-") {
		positional := strings.ToLower(strings.TrimSpace(remaining[0]))
		selected := strings.ToLower(strings.TrimSpace(client))
		if positional != selected {
			return fmt.Errorf("setup target %q conflicts with --client %q", positional, selected)
		}
		remaining = remaining[1:]
	}
	return runAdd(ctx, append([]string{client}, remaining...), rt)
}

func extractSetupClient(args []string) (string, []string, error) {
	remaining := make([]string, 0, len(args))
	client := ""
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--client":
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				return "", nil, fmt.Errorf("--client requires a target")
			}
			i++
			client = strings.TrimSpace(args[i])
		case strings.HasPrefix(arg, "--client="):
			client = strings.TrimSpace(strings.TrimPrefix(arg, "--client="))
		default:
			remaining = append(remaining, arg)
		}
	}
	if strings.TrimSpace(client) == "" && len(args) != len(remaining) {
		return "", nil, fmt.Errorf("--client requires a target")
	}
	return client, remaining, nil
}

func parseSetupMode(value string) (setupMode, error) {
	mode := setupMode(strings.ToLower(strings.TrimSpace(value)))
	switch mode {
	case setupModeMCP, setupModeSkill, setupModeBoth:
		return mode, nil
	default:
		return "", fmt.Errorf("invalid --mode %q; use mcp, skill, or both", value)
	}
}

func (mode setupMode) includesMCP() bool {
	return mode == setupModeMCP || mode == setupModeBoth
}

func (mode setupMode) includesSkills() bool {
	return mode == setupModeSkill || mode == setupModeBoth
}

func runAdd(ctx context.Context, args []string, rt Runtime) error {
	resolvedSources := map[string]string{}
	if len(args) == 0 || strings.HasPrefix(strings.TrimSpace(args[0]), "-") {
		targets, err := promptAddTargets(rt)
		if err != nil {
			return err
		}
		for _, spec := range targets {
			if err := runAddTarget(ctx, args, rt, spec, resolvedSources); err != nil {
				return err
			}
		}
		return nil
	}
	targetName := strings.ToLower(strings.TrimSpace(args[0]))
	if targetName == "all" {
		for _, spec := range addAllTargets() {
			if err := runAddTarget(ctx, args[1:], rt, spec, resolvedSources); err != nil {
				return err
			}
		}
		return nil
	}
	spec, ok := addTarget(targetName)
	if !ok {
		return fmt.Errorf("unknown add target %q", args[0])
	}
	return runAddTarget(ctx, args[1:], rt, spec, resolvedSources)
}

func promptAddTargets(rt Runtime) ([]addTargetSpec, error) {
	targets := addSelectableTargets()
	out := newCheckedWriter(rt.Out)
	out.Println("Select AI agents or IDEs to add Soha MCP and skills:")
	for i, spec := range targets {
		out.Printf("  %d) %s (%s)\n", i+1, spec.DisplayName, spec.Name)
	}
	out.Print("Enter numbers or names separated by commas, or all: ")
	if err := out.Err(); err != nil {
		return nil, err
	}
	line, err := bufio.NewReader(rt.In).ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		return nil, err
	}
	return parseAddTargetSelection(line, targets)
}

func parseAddTargetSelection(input string, targets []addTargetSpec) ([]addTargetSpec, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, fmt.Errorf("no add targets selected")
	}
	if strings.EqualFold(input, "all") {
		return targets, nil
	}
	byName := map[string]addTargetSpec{}
	for _, spec := range targets {
		byName[spec.Name] = spec
		byName[strings.ToLower(spec.DisplayName)] = spec
	}
	var selected []addTargetSpec
	seen := map[string]bool{}
	for _, token := range strings.FieldsFunc(input, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	}) {
		token = strings.ToLower(strings.TrimSpace(token))
		if token == "" {
			continue
		}
		if token == "all" {
			return targets, nil
		}
		var spec addTargetSpec
		if index, ok := parsePositiveIndex(token); ok {
			if index < 1 || index > len(targets) {
				return nil, fmt.Errorf("target number %d is out of range", index)
			}
			spec = targets[index-1]
		} else {
			var ok bool
			spec, ok = addTarget(token)
			if !ok {
				spec, ok = byName[token]
			}
			if !ok {
				return nil, fmt.Errorf("unknown add target %q", token)
			}
		}
		if !seen[spec.Name] {
			selected = append(selected, spec)
			seen[spec.Name] = true
		}
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("no add targets selected")
	}
	return selected, nil
}

func parsePositiveIndex(value string) (int, bool) {
	n := 0
	for _, r := range value {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = n*10 + int(r-'0')
	}
	return n, value != ""
}

func runAddTarget(ctx context.Context, args []string, rt Runtime, target addTargetSpec, resolvedSources map[string]string) error {
	out := newCheckedWriter(rt.Out)
	fs := newRuntimeFlagSet("add "+target.Name, args, rt)
	modeFlag := fs.String("mode", string(setupModeBoth), "components to configure: mcp, skill, or both")
	scopeFlag := fs.String("scope", "user", "configuration scope: user or project")
	checkFlag := fs.Bool("check", false, "verify configuration without writing files")
	profileFlag := fs.String("profile", "", "profile name for the generated MCP server")
	serverFlag := fs.String("server", "", "Soha API base URL for the profile")
	baseURLFlag := fs.String("base-url", "", "alias for --server")
	commandFlag := fs.String("command", "", "soha executable path for the generated MCP server")
	aiClientIDFlag := fs.String("ai-client-id", "", "AI client id for Gateway audit context")
	aiClientNameFlag := fs.String("ai-client", "", "AI client display name for Gateway audit context")
	skillIDFlag := fs.String("skill-id", "", "skill id for the generated MCP server")
	sourceFlag := fs.String("skills-source", defaultSkillSourcePath(), "Soha skill directory, release archive, URL, or github:owner/repo[@latest|@version]")
	sourceAliasFlag := fs.String("source", "", "alias for --skills-source")
	skillsFlag := fs.String("skills", "all", "comma-separated skill ids to install, or all")
	codexHomeFlag := fs.String("codex-home", defaultCodexHome(), "Codex home containing config.toml")
	codexConfigFlag := fs.String("codex-config", "", "Codex config.toml path")
	configFlag := fs.String("config", "", "target MCP config path")
	agentsHomeFlag := fs.String("agents-home", defaultAgentsHome(), "Codex agents home containing skills")
	skillDestFlag := fs.String("skill-dest", "", "Codex skill destination directory")
	destAliasFlag := fs.String("dest", "", "alias for --skill-dest")
	runtimeSkillDestFlag := fs.String("runtime-skill-dest", "", "Soha runtime skill destination directory")
	mcpNameFlag := fs.String("mcp-name", "soha", "Codex MCP server name")
	noRuntimeSkillsFlag := fs.Bool("no-runtime-skills", false, "skip installing raw Soha runtime skills")
	overwriteFlag := fs.Bool("overwrite", true, "overwrite generated skill files")
	dryRunFlag := fs.Bool("dry-run", false, "print planned changes without writing files")
	if err := fs.Parse(args); err != nil {
		return err
	}
	mode, err := parseSetupMode(*modeFlag)
	if err != nil {
		return err
	}
	scope := strings.ToLower(strings.TrimSpace(*scopeFlag))
	if scope != "user" && scope != "project" {
		return fmt.Errorf("invalid --scope %q; use user or project", *scopeFlag)
	}
	if *checkFlag && *dryRunFlag {
		return fmt.Errorf("--check and --dry-run cannot be used together")
	}
	installMCP := mode.includesMCP()
	installSkills := mode.includesSkills()

	cfg, err := loadConfig(rt.ConfigPath)
	if err != nil {
		return err
	}
	profileNameValue := profileName(firstNonEmptyString(*profileFlag, cfg.CurrentProfile))
	profileConfig, profileExists := cfg.Profiles[profileNameValue]
	command := strings.TrimSpace(*commandFlag)
	if command == "" {
		command = defaultSohaCommand()
	}
	aiClientID := firstNonEmptyString(*aiClientIDFlag, profileConfig.AIClientID, "codex-local")
	aiClientName := firstNonEmptyString(*aiClientNameFlag, profileConfig.AIClientName, "Codex")
	skillID := firstNonEmptyString(*skillIDFlag, profileConfig.SkillID)
	serverURL := normalizeServerURL(firstNonEmptyString(*serverFlag, *baseURLFlag, profileConfig.ServerURL, defaultServerURL))
	if serverURL == "" {
		return fmt.Errorf("soha API base URL is required")
	}
	if profileHasCredentials(profileConfig) && !sameServerURL(serverURL, profileConfig.ServerURL) {
		return fmt.Errorf("server_mismatch: this profile already contains credentials for another server; log in to a new profile before configuring MCP")
	}
	updatedProfile := profileConfig
	updatedProfile.ServerURL = serverURL
	if strings.TrimSpace(updatedProfile.AIClientID) == "" || strings.TrimSpace(*aiClientIDFlag) != "" {
		updatedProfile.AIClientID = aiClientID
	}
	if strings.TrimSpace(updatedProfile.AIClientName) == "" || strings.TrimSpace(*aiClientNameFlag) != "" {
		updatedProfile.AIClientName = aiClientName
	}
	if strings.TrimSpace(updatedProfile.SkillID) == "" || strings.TrimSpace(*skillIDFlag) != "" {
		updatedProfile.SkillID = skillID
	}
	if strings.TrimSpace(updatedProfile.Source) == "" {
		updatedProfile.Source = "codex"
	}
	var sourceRef, source string
	var skillNames []string
	if installSkills {
		sourceRef = firstNonEmptyString(*sourceAliasFlag, *sourceFlag)
		if sourceRef == "" {
			return fmt.Errorf("skill source directory is required")
		}
		source = resolvedSources[sourceRef]
		if source == "" {
			source, err = resolveSkillSource(ctx, sourceRef, rt)
			if err != nil {
				return err
			}
			resolvedSources[sourceRef] = source
		}
		skillNames, err = addSkillNames(source, *skillsFlag)
		if err != nil {
			return err
		}
	}
	mcpName := strings.TrimSpace(*mcpNameFlag)
	if installMCP && !isSafeSkillName(mcpName) {
		return fmt.Errorf("invalid MCP server name %q", mcpName)
	}
	codexHome := expandHome(*codexHomeFlag)
	agentsHome := expandHome(*agentsHomeFlag)
	defaultConfigPath, defaultSkillDest, err := addTargetDefaultsForScope(target, scope)
	if err != nil {
		return err
	}
	if scope == "user" && target.Name == "codex" {
		defaultSkillDest = filepath.Join(agentsHome, "skills")
		defaultConfigPath = filepath.Join(codexHome, "config.toml")
	}
	skillDest := firstNonEmptyString(*destAliasFlag, *skillDestFlag, defaultSkillDest)
	skillDest = expandHome(skillDest)
	runtimeSkillDest, err := resolveSkillInstallDestination(scope, *runtimeSkillDestFlag)
	if err != nil {
		return err
	}
	targetConfigPath := expandHome(firstNonEmptyString(*configFlag, *codexConfigFlag, defaultConfigPath))
	var mcpArgs []string
	var mcpBlock string
	if installMCP {
		mcpBaseURL := firstNonEmptyString(*baseURLFlag, *serverFlag)
		if strings.TrimSpace(mcpBaseURL) == "" && serverURL != defaultServerURL {
			mcpBaseURL = serverURL
		}
		mcpArgs = mcpInstallArgs(profileNameValue, mcpBaseURL, aiClientID, aiClientName, skillID)
		mcpBlock = codexMCPBlock(mcpName, command, mcpArgs)
	}
	if installSkills {
		if _, err := sohaSkillPackageFiles(source, skillDest, skillNames); err != nil {
			return err
		}
	}

	if *checkFlag {
		if !profileExists {
			return fmt.Errorf("soha profile %q is not configured", profileNameValue)
		}
		if updatedProfile != profileConfig {
			return fmt.Errorf("soha profile %q does not match the requested setup", profileNameValue)
		}
		out.Printf("Checked Soha profile %s: %s\n", profileNameValue, serverURL)
		if installMCP {
			if err := verifyMCPConfig(target, targetConfigPath, mcpName, command, mcpArgs, mcpBlock); err != nil {
				return err
			}
			out.Printf("Checked %s MCP server %s in %s\n", target.DisplayName, mcpName, targetConfigPath)
		}
		if installSkills {
			if err := verifySohaSkillPackage(source, skillDest, skillNames); err != nil {
				return err
			}
			out.Printf("Checked %s Soha skill package in %s\n", target.DisplayName, filepath.Join(skillDest, "soha"))
			if !*noRuntimeSkillsFlag {
				if err := verifyRuntimeSkills(source, runtimeSkillDest, skillNames); err != nil {
					return err
				}
				out.Printf("Checked Soha runtime skills in %s\n", runtimeSkillDest)
			}
		}
		return out.Err()
	}

	if *dryRunFlag {
		if !profileExists || updatedProfile != profileConfig {
			out.Printf("Would configure Soha profile %s with API base URL: %s\n", profileNameValue, serverURL)
		} else {
			out.Printf("Soha profile %s already uses API base URL: %s\n", profileNameValue, serverURL)
		}
		if installMCP {
			if target.ConfigKind == "toml" {
				out.Printf("Would update %s MCP config: %s\n\n%s\n", target.DisplayName, targetConfigPath, mcpBlock)
			} else {
				out.Printf("Would update %s MCP config: %s\n", target.DisplayName, targetConfigPath)
				out.Printf("Would set mcpServers.%s command=%s args=%s\n", mcpName, command, strings.Join(mcpArgs, " "))
			}
		}
		if installSkills {
			out.Printf("Would install %s Soha skill package: %s\n", target.DisplayName, filepath.Join(skillDest, "soha", "SKILL.md"))
			out.Printf("Would copy skill references from %s: %s\n", sourceRef, strings.Join(skillNames, ", "))
			if !*noRuntimeSkillsFlag {
				out.Printf("Would install Soha runtime skills to %s: %s\n", runtimeSkillDest, strings.Join(skillNames, ", "))
			}
		}
		return out.Err()
	}

	if installMCP {
		var backupPath string
		var changed bool
		if target.ConfigKind == "toml" {
			backupPath, changed, err = upsertCodexMCPConfig(targetConfigPath, "mcp_servers."+mcpName, mcpBlock)
			if err != nil {
				return err
			}
		} else {
			backupPath, changed, err = upsertJSONMCPConfig(targetConfigPath, mcpName, command, mcpArgs, target.JSONC, target.JSONDisabled)
			if err != nil {
				return err
			}
		}
		if changed {
			out.Printf("Configured %s MCP server %s in %s\n", target.DisplayName, mcpName, targetConfigPath)
			if backupPath != "" {
				out.Printf("Backed up previous %s config to %s\n", target.DisplayName, backupPath)
			}
		} else {
			out.Printf("%s MCP server %s already up to date in %s\n", target.DisplayName, mcpName, targetConfigPath)
		}
	}

	if installSkills {
		installed, err := installSohaSkillPackage(source, skillDest, skillNames, *overwriteFlag)
		if err != nil {
			return err
		}
		for _, path := range installed {
			out.Printf("Installed %s Soha skill file %s\n", target.DisplayName, path)
		}

		if !*noRuntimeSkillsFlag {
			generation, changed, err := installSkillGeneration(source, sourceRef, runtimeSkillDest, skillNames, *overwriteFlag, "install")
			if err != nil {
				return err
			}
			if !changed {
				out.Printf("Soha runtime skills already up to date in %s\n", runtimeSkillDest)
			} else {
				for _, name := range generation.Skills {
					out.Printf("Installed Soha runtime skill %s to %s\n", name, filepath.Join(runtimeSkillDest, name, "SKILL.md"))
				}
			}
		}
	}

	if !profileExists || updatedProfile != profileConfig {
		if err := persistSetupProfile(ctx, rt, profileNameValue, updatedProfile, *aiClientIDFlag != "", *aiClientNameFlag != "", *skillIDFlag != ""); err != nil {
			return err
		}
		out.Printf("Configured Soha profile %s with API base URL %s\n", profileNameValue, serverURL)
	} else {
		out.Printf("Soha profile %s already uses API base URL %s\n", profileNameValue, serverURL)
	}

	if strings.TrimSpace(target.RestartMessage) != "" {
		out.Println(target.RestartMessage)
	} else {
		out.Printf("Restart %s or open a new session so MCP servers and skills are reloaded.\n", target.DisplayName)
	}
	return out.Err()
}

func addTargetDefaultsForScope(target addTargetSpec, scope string) (string, string, error) {
	if scope == "user" {
		return target.DefaultConfig(), target.DefaultSkill(), nil
	}
	root, err := os.Getwd()
	if err != nil {
		return "", "", fmt.Errorf("resolve project directory: %w", err)
	}
	switch target.Name {
	case "codex":
		return filepath.Join(root, ".codex", "config.toml"), filepath.Join(root, ".agents", "skills"), nil
	case "claude":
		return filepath.Join(root, ".mcp.json"), filepath.Join(root, ".claude", "skills"), nil
	case "cursor":
		return filepath.Join(root, ".cursor", "mcp.json"), filepath.Join(root, ".cursor", "skills"), nil
	case "kiro":
		return filepath.Join(root, ".kiro", "settings", "mcp.json"), filepath.Join(root, ".kiro", "steering"), nil
	case "gemini":
		return filepath.Join(root, ".gemini", "settings.json"), filepath.Join(root, ".gemini", "skills"), nil
	case "antigravity":
		return filepath.Join(root, ".gemini", "antigravity", "mcp_config.json"), filepath.Join(root, ".gemini", "antigravity", "skills"), nil
	case "antigravity-ide":
		return filepath.Join(root, ".gemini", "antigravity-ide", "mcp_config.json"), filepath.Join(root, ".gemini", "antigravity-ide", "skills"), nil
	case "trae":
		return filepath.Join(root, ".trae", "mcp.json"), filepath.Join(root, ".trae", "skills"), nil
	default:
		return "", "", fmt.Errorf("project scope is not supported for %s", target.DisplayName)
	}
}

func addTarget(name string) (addTargetSpec, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "codex", "openai":
		return addTargetSpec{
			Name:           "codex",
			DisplayName:    "Codex",
			ConfigKind:     "toml",
			DefaultConfig:  func() string { return filepath.Join(defaultCodexHome(), "config.toml") },
			DefaultSkill:   func() string { return filepath.Join(defaultAgentsHome(), "skills") },
			RestartMessage: "Restart Codex or open a new Codex session so MCP servers and skills are reloaded.",
		}, true
	case "claude", "claude-desktop":
		return addTargetSpec{
			Name:        "claude",
			DisplayName: "Claude",
			ConfigKind:  "json",
			DefaultConfig: func() string {
				return filepath.Join(userHomeDirFallback(), "Library", "Application Support", "Claude", "claude_desktop_config.json")
			},
			DefaultSkill: func() string { return filepath.Join(userHomeDirFallback(), ".claude", "skills") },
		}, true
	case "cursor":
		return addTargetSpec{
			Name:          "cursor",
			DisplayName:   "Cursor",
			ConfigKind:    "json",
			DefaultConfig: func() string { return filepath.Join(userHomeDirFallback(), ".cursor", "mcp.json") },
			DefaultSkill:  func() string { return filepath.Join(userHomeDirFallback(), ".cursor", "skills-cursor") },
		}, true
	case "kiro":
		return addTargetSpec{
			Name:          "kiro",
			DisplayName:   "Kiro",
			ConfigKind:    "json",
			DefaultConfig: func() string { return filepath.Join(userHomeDirFallback(), ".kiro", "settings", "mcp.json") },
			DefaultSkill:  func() string { return filepath.Join(userHomeDirFallback(), ".kiro", "steering") },
			JSONC:         true,
			JSONDisabled:  true,
		}, true
	case "gemini", "gemini-cli":
		return addTargetSpec{
			Name:          "gemini",
			DisplayName:   "Gemini",
			ConfigKind:    "json",
			DefaultConfig: func() string { return filepath.Join(userHomeDirFallback(), ".gemini", "settings.json") },
			DefaultSkill:  func() string { return filepath.Join(userHomeDirFallback(), ".gemini", "skills") },
		}, true
	case "antigravity", "anti-gravity", "反重力":
		return addTargetSpec{
			Name:        "antigravity",
			DisplayName: "Antigravity",
			ConfigKind:  "json",
			DefaultConfig: func() string {
				return filepath.Join(userHomeDirFallback(), ".gemini", "antigravity", "mcp_config.json")
			},
			DefaultSkill: func() string { return filepath.Join(userHomeDirFallback(), ".gemini", "antigravity", "skills") },
		}, true
	case "antigravity-ide":
		return addTargetSpec{
			Name:        "antigravity-ide",
			DisplayName: "Antigravity IDE",
			ConfigKind:  "json",
			DefaultConfig: func() string {
				return filepath.Join(userHomeDirFallback(), ".gemini", "antigravity-ide", "mcp_config.json")
			},
			DefaultSkill: func() string { return filepath.Join(userHomeDirFallback(), ".gemini", "antigravity-ide", "skills") },
		}, true
	case "trae":
		return addTargetSpec{
			Name:        "trae",
			DisplayName: "Trae",
			ConfigKind:  "json",
			DefaultConfig: func() string {
				return filepath.Join(userHomeDirFallback(), "Library", "Application Support", "Trae", "User", "mcp.json")
			},
			DefaultSkill: func() string { return filepath.Join(userHomeDirFallback(), ".trae", "skills") },
			JSONDisabled: true,
		}, true
	default:
		return addTargetSpec{}, false
	}
}

func mustAddTarget(name string) addTargetSpec {
	spec, ok := addTarget(name)
	if !ok {
		panic("unknown built-in add target " + name)
	}
	return spec
}

func addAllTargets() []addTargetSpec {
	return addSelectableTargets()
}

func addSelectableTargets() []addTargetSpec {
	return []addTargetSpec{
		mustAddTarget("codex"),
		mustAddTarget("claude"),
		mustAddTarget("cursor"),
		mustAddTarget("kiro"),
		mustAddTarget("gemini"),
		mustAddTarget("antigravity"),
		mustAddTarget("antigravity-ide"),
		mustAddTarget("trae"),
	}
}

func addSkillNames(source, value string) ([]string, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.EqualFold(value, "all") {
		return listLocalSkills(source)
	}
	names := splitCSV(value)
	if len(names) == 0 {
		return nil, fmt.Errorf("--skills requires at least one skill id or all")
	}
	for _, name := range names {
		if !isSafeSkillName(name) {
			return nil, fmt.Errorf("invalid skill id %q", name)
		}
		if _, err := os.Stat(filepath.Join(source, name, "SKILL.md")); err != nil {
			return nil, err
		}
	}
	sort.Strings(names)
	return names, nil
}

func defaultSohaCommand() string {
	if path, err := os.Executable(); err == nil {
		path = strings.TrimSpace(path)
		if path != "" && strings.HasPrefix(filepath.Base(path), "soha") {
			return path
		}
	}
	if path, err := exec.LookPath("soha"); err == nil {
		return path
	}
	return "soha"
}

func defaultCodexHome() string {
	if value := env("CODEX_HOME"); value != "" {
		return value
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return filepath.Join(".codex")
	}
	return filepath.Join(home, ".codex")
}

func defaultAgentsHome() string {
	if value := env("AGENTS_HOME"); value != "" {
		return value
	}
	home := userHomeDirFallback()
	if strings.TrimSpace(home) == "" {
		return filepath.Join(".agents")
	}
	return filepath.Join(home, ".agents")
}

func userHomeDirFallback() string {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ""
	}
	return home
}

func expandHome(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || path == "~" {
		home, err := os.UserHomeDir()
		if err != nil || strings.TrimSpace(home) == "" {
			return path
		}
		if path == "~" {
			return home
		}
		return path
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil || strings.TrimSpace(home) == "" {
			return path
		}
		return filepath.Join(home, strings.TrimPrefix(path, "~/"))
	}
	return path
}

func codexMCPBlock(name, command string, args []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[mcp_servers.%s]\n", name)
	fmt.Fprintln(&b, `type = "stdio"`)
	fmt.Fprintf(&b, "command = %s\n", tomlString(command))
	fmt.Fprintf(&b, "args = %s\n", tomlStringArray(args))
	return b.String()
}

func tomlStringArray(values []string) string {
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, tomlString(value))
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

func tomlString(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	value = strings.ReplaceAll(value, "\n", `\n`)
	value = strings.ReplaceAll(value, "\r", `\r`)
	value = strings.ReplaceAll(value, "\t", `\t`)
	return `"` + value + `"`
}

func upsertCodexMCPConfig(path, section, block string) (string, bool, error) {
	var raw []byte
	var mode fs.FileMode = 0o600
	stat, statErr := os.Stat(path)
	if statErr == nil {
		mode = stat.Mode().Perm()
		var err error
		// #nosec G304 -- path is the explicit target client configuration selected by the user.
		raw, err = os.ReadFile(path)
		if err != nil {
			return "", false, err
		}
	} else if !os.IsNotExist(statErr) {
		return "", false, statErr
	}

	next := upsertTOMLSection(string(raw), section, strings.TrimRight(block, "\n"))
	if next == string(raw) {
		return "", false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", false, err
	}
	backupPath := ""
	if statErr == nil {
		backupPath = fmt.Sprintf("%s.bak.%s", path, time.Now().Format("20060102150405"))
		// #nosec G703 -- backup is beside the explicit local client configuration selected by the user, not a remotely supplied path.
		if err := os.WriteFile(backupPath, raw, mode); err != nil {
			return "", false, err
		}
	}
	// #nosec G703 -- path is the explicit local client configuration selected by the user, matching the authorized read above.
	if err := os.WriteFile(path, []byte(next), mode); err != nil {
		return "", false, err
	}
	return backupPath, true, nil
}

func upsertJSONMCPConfig(path, serverName, command string, args []string, jsonc bool, includeDisabled bool) (string, bool, error) {
	raw, mode, statErr, err := readOptionalFile(path)
	if err != nil {
		return "", false, err
	}
	root := map[string]any{}
	if len(bytes.TrimSpace(raw)) > 0 {
		decodeRaw := raw
		if jsonc {
			decodeRaw = stripJSONComments(raw)
		}
		if err := json.Unmarshal(decodeRaw, &root); err != nil {
			return "", false, fmt.Errorf("decode %s: %w", path, err)
		}
	}
	servers, ok := root["mcpServers"].(map[string]any)
	if !ok {
		servers = map[string]any{}
		root["mcpServers"] = servers
	}
	entry := map[string]any{
		"command": command,
		"args":    args,
	}
	if includeDisabled {
		entry["disabled"] = false
	}
	servers[serverName] = entry
	next, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return "", false, err
	}
	next = append(next, '\n')
	if bytes.Equal(bytes.TrimSpace(raw), bytes.TrimSpace(next)) {
		return "", false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", false, err
	}
	backupPath := ""
	if statErr == nil {
		backupPath = fmt.Sprintf("%s.bak.%s", path, time.Now().Format("20060102150405"))
		if err := os.WriteFile(backupPath, raw, mode); err != nil {
			return "", false, err
		}
	}
	if err := os.WriteFile(path, next, mode); err != nil {
		return "", false, err
	}
	return backupPath, true, nil
}

func verifyMCPConfig(target addTargetSpec, path, serverName, command string, args []string, tomlBlock string) error {
	raw, _, statErr, err := readOptionalFile(path)
	if err != nil {
		return err
	}
	if statErr != nil {
		return fmt.Errorf("%s MCP config does not exist: %s", target.DisplayName, path)
	}
	if target.ConfigKind == "toml" {
		next := upsertTOMLSection(string(raw), "mcp_servers."+serverName, strings.TrimRight(tomlBlock, "\n"))
		if next != string(raw) {
			return fmt.Errorf("%s MCP server %q is missing or stale in %s", target.DisplayName, serverName, path)
		}
		return nil
	}
	decodeRaw := raw
	if target.JSONC {
		decodeRaw = stripJSONComments(raw)
	}
	root := map[string]any{}
	if err := json.Unmarshal(decodeRaw, &root); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	servers, _ := root["mcpServers"].(map[string]any)
	entry, _ := servers[serverName].(map[string]any)
	if entry == nil || entry["command"] != command || !jsonStringSliceEqual(entry["args"], args) {
		return fmt.Errorf("%s MCP server %q is missing or stale in %s", target.DisplayName, serverName, path)
	}
	if target.JSONDisabled && entry["disabled"] != false {
		return fmt.Errorf("%s MCP server %q is disabled in %s", target.DisplayName, serverName, path)
	}
	return nil
}

func jsonStringSliceEqual(value any, expected []string) bool {
	items, ok := value.([]any)
	if !ok || len(items) != len(expected) {
		return false
	}
	for index, item := range items {
		if item != expected[index] {
			return false
		}
	}
	return true
}

func readOptionalFile(path string) ([]byte, fs.FileMode, error, error) {
	mode := fs.FileMode(0o600)
	stat, statErr := os.Stat(path)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			return nil, mode, statErr, nil
		}
		return nil, mode, statErr, statErr
	}
	mode = stat.Mode().Perm()
	// #nosec G304 -- path is the explicit target client configuration selected by the user.
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, mode, statErr, err
	}
	return raw, mode, statErr, nil
}

func stripJSONComments(raw []byte) []byte {
	out := make([]byte, 0, len(raw))
	inString := false
	escaped := false
	for i := 0; i < len(raw); i++ {
		ch := raw[i]
		if inString {
			out = append(out, ch)
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		if ch == '"' {
			inString = true
			out = append(out, ch)
			continue
		}
		if ch == '/' && i+1 < len(raw) && raw[i+1] == '/' {
			for i < len(raw) && raw[i] != '\n' {
				i++
			}
			if i < len(raw) {
				out = append(out, raw[i])
			}
			continue
		}
		if ch == '/' && i+1 < len(raw) && raw[i+1] == '*' {
			i += 2
			for i+1 < len(raw) && (raw[i] != '*' || raw[i+1] != '/') {
				if raw[i] == '\n' {
					out = append(out, '\n')
				}
				i++
			}
			i++
			continue
		}
		out = append(out, ch)
	}
	return out
}

func upsertTOMLSection(raw, section, block string) string {
	header := "[" + section + "]"
	lines := strings.Split(raw, "\n")
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == header {
			start = i
			break
		}
	}
	blockLines := strings.Split(strings.TrimRight(block, "\n"), "\n")
	if start >= 0 {
		end := len(lines)
		for i := start + 1; i < len(lines); i++ {
			trimmed := strings.TrimSpace(lines[i])
			if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
				end = i
				break
			}
		}
		next := append([]string{}, lines[:start]...)
		next = append(next, blockLines...)
		next = append(next, lines[end:]...)
		out := strings.Join(next, "\n")
		if !strings.HasSuffix(out, "\n") {
			out += "\n"
		}
		return out
	}
	if strings.TrimSpace(raw) == "" {
		return strings.Join(blockLines, "\n") + "\n"
	}
	return strings.TrimRight(raw, "\n") + "\n\n" + strings.Join(blockLines, "\n") + "\n"
}

func sohaSkillPackageFiles(source, dest string, skillNames []string) (map[string][]byte, error) {
	root := filepath.Join(dest, "soha")
	agentSkillSource, err := findSohaAgentSkillSource(source)
	if err != nil {
		return nil, err
	}
	// #nosec G304 -- agentSkillSource is discovered inside the verified skills package.
	skillMarkdown, err := os.ReadFile(filepath.Join(agentSkillSource, "SKILL.md"))
	if err != nil {
		return nil, err
	}
	// #nosec G304 -- agentSkillSource is discovered inside the verified skills package.
	openAIYAML, err := os.ReadFile(filepath.Join(agentSkillSource, "agents", "openai.yaml"))
	if err != nil {
		return nil, err
	}
	writes := map[string][]byte{
		filepath.Join(root, "SKILL.md"):              skillMarkdown,
		filepath.Join(root, "agents", "openai.yaml"): openAIYAML,
	}
	indexPath := filepath.Join(filepath.Dir(source), "index.json")
	// #nosec G304 -- indexPath is adjacent to the verified skills source.
	if raw, err := os.ReadFile(indexPath); err == nil {
		writes[filepath.Join(root, "references", "skills", "index.json")] = raw
	}
	for _, name := range skillNames {
		// #nosec G304 -- name is validated and source is the verified skills directory.
		raw, err := os.ReadFile(filepath.Join(source, name, "SKILL.md"))
		if err != nil {
			return nil, err
		}
		writes[filepath.Join(root, "references", "skills", name+".md")] = raw
	}
	return writes, nil
}

func findSohaAgentSkillSource(source string) (string, error) {
	candidates := []string{
		filepath.Join(filepath.Dir(filepath.Dir(source)), "agent-skills", "soha"),
		filepath.Join(source, "agent-skills", "soha"),
		filepath.Join(filepath.Dir(source), "agent-skills", "soha"),
	}
	seen := map[string]bool{}
	for _, candidate := range candidates {
		candidate = filepath.Clean(candidate)
		if seen[candidate] {
			continue
		}
		seen[candidate] = true
		if _, err := os.Stat(filepath.Join(candidate, "SKILL.md")); err != nil {
			continue
		}
		if _, err := os.Stat(filepath.Join(candidate, "agents", "openai.yaml")); err != nil {
			continue
		}
		return candidate, nil
	}
	return "", fmt.Errorf("skills source %s does not contain canonical agent-skills/soha", source)
}

func installSohaSkillPackage(source, dest string, skillNames []string, overwrite bool) ([]string, error) {
	writes, err := sohaSkillPackageFiles(source, dest, skillNames)
	if err != nil {
		return nil, err
	}

	paths := make([]string, 0, len(writes))
	for path := range writes {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		if err := writeGeneratedFile(path, writes[path], overwrite); err != nil {
			return nil, err
		}
	}
	return paths, nil
}

func verifySohaSkillPackage(source, dest string, skillNames []string) error {
	writes, err := sohaSkillPackageFiles(source, dest, skillNames)
	if err != nil {
		return err
	}
	for path, expected := range writes {
		// #nosec G304 -- path is generated from the selected destination and verified package manifest.
		actual, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("soha skill file is missing: %s", path)
		}
		if !bytes.Equal(actual, expected) {
			return fmt.Errorf("soha skill file is stale: %s", path)
		}
	}
	return nil
}

func verifyRuntimeSkills(source, dest string, skillNames []string) error {
	for _, name := range skillNames {
		// #nosec G304 -- name is validated and source is the verified skills directory.
		expected, err := os.ReadFile(filepath.Join(source, name, "SKILL.md"))
		if err != nil {
			return err
		}
		path := filepath.Join(dest, name, "SKILL.md")
		// #nosec G304 -- path is generated from the selected destination and validated skill name.
		actual, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("soha runtime skill is missing: %s", path)
		}
		if !bytes.Equal(actual, expected) {
			return fmt.Errorf("soha runtime skill is stale: %s", path)
		}
	}
	return nil
}

func writeGeneratedFile(path string, raw []byte, overwrite bool) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	flag := os.O_WRONLY | os.O_CREATE
	if overwrite {
		flag |= os.O_TRUNC
	} else {
		flag |= os.O_EXCL
	}
	// #nosec G304 -- path is a generated file destination selected by the user.
	file, err := os.OpenFile(path, flag, fs.FileMode(0o600))
	if err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("%s already exists; pass --overwrite=true to replace it", path)
		}
		return err
	}
	if _, err := file.Write(raw); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}
