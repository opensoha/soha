package sohacli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
)

func runPlugin(ctx context.Context, args []string, rt Runtime) error {
	if len(args) == 0 {
		return fmt.Errorf("plugin requires a subcommand: search, show, install, list, enable, disable, upgrade, config, remove")
	}
	switch args[0] {
	case "search":
		return runPluginSearchWithOutput(ctx, args[1:], rt)
	case "show":
		return runPluginShowWithOutput(ctx, args[1:], rt)
	case "install":
		return runPluginInstall(ctx, args[1:], rt)
	case "list":
		return runPluginListWithOutput(ctx, args[1:], rt)
	case "enable":
		return runPluginEnable(ctx, args[1:], rt)
	case "disable":
		return runPluginDisable(ctx, args[1:], rt)
	case "upgrade":
		return runPluginUpgrade(ctx, args[1:], rt)
	case "config":
		return runPluginConfig(ctx, args[1:], rt)
	case "remove", "rm":
		return runPluginRemove(ctx, args[1:], rt)
	default:
		return fmt.Errorf("unknown plugin command %q", args[0])
	}
}

func runPluginInstall(ctx context.Context, args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("plugin install", args, rt)
	profileFlag := fs.String("profile", "", "profile name")
	manifestPath := fs.String("manifest", "", "plugin manifest JSON file")
	source := fs.String("source", "", "plugin source URL, path, or marketplace id")
	marketplace := fs.String("marketplace", "", "marketplace catalog URL")
	sourceID := fs.String("source-id", "", "marketplace source id")
	version := fs.String("version", "", "plugin version")
	expectedChecksum := fs.String("expected-checksum", "", "expected sha256:<hex> manifest checksum")
	enable := fs.Bool("enable", false, "enable after install")
	jsonOutput := fs.Bool("json", false, "print JSON output")
	if err := fs.Parse(args); err != nil {
		return err
	}
	_, _, profile, err := loadRuntimeProfile(ctx, rt, *profileFlag)
	if err != nil {
		return err
	}
	req, err := pluginInstallRequest(pluginInstallOptions{
		PluginID:         firstNonEmptyString(fs.Arg(0), *source),
		Source:           *source,
		ManifestPath:     *manifestPath,
		ExpectedChecksum: *expectedChecksum,
		Enable:           *enable,
		MarketplaceURL:   firstNonEmptyString(*marketplace, profile.MarketplaceURL),
		SourceID:         firstNonEmptyString(*sourceID, profile.MarketplaceSourceID),
		Version:          *version,
	})
	if err != nil {
		return err
	}
	item, err := gatewayClient(rt, profile).InstallPlugin(ctx, req, gatewayHeaders(profile, "", "", "", "soha-cli"))
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writePrettyJSON(rt.Out, item)
	}
	_, err = fmt.Fprintf(rt.Out, "Installed plugin %s %s (%s)\n", item.ID, item.Version, item.Status)
	return err
}

func runPluginEnable(ctx context.Context, args []string, rt Runtime) error {
	return runPluginAction(ctx, args, rt, "enable")
}

func runPluginDisable(ctx context.Context, args []string, rt Runtime) error {
	return runPluginAction(ctx, args, rt, "disable")
}

func runPluginAction(ctx context.Context, args []string, rt Runtime, action string) error {
	fs := newRuntimeFlagSet("plugin "+action, args, rt)
	profileFlag := fs.String("profile", "", "profile name")
	jsonOutput := fs.Bool("json", false, "print JSON output")
	if err := fs.Parse(args); err != nil {
		return err
	}
	pluginID := strings.TrimSpace(fs.Arg(0))
	if pluginID == "" {
		return fmt.Errorf("plugin %s requires a plugin id", action)
	}
	_, _, profile, err := loadRuntimeProfile(ctx, rt, *profileFlag)
	if err != nil {
		return err
	}
	client := gatewayClient(rt, profile)
	headers := gatewayHeaders(profile, "", "", "", "soha-cli")
	var item InstalledPlugin
	if action == "enable" {
		item, err = client.EnablePlugin(ctx, pluginID, headers)
	} else {
		item, err = client.DisablePlugin(ctx, pluginID, headers)
	}
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writePrettyJSON(rt.Out, item)
	}
	_, err = fmt.Fprintf(rt.Out, "%sd plugin %s (%s)\n", strings.ToUpper(action[:1])+action[1:], item.ID, item.Status)
	return err
}

func runPluginUpgrade(ctx context.Context, args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("plugin upgrade", args, rt)
	profileFlag := fs.String("profile", "", "profile name")
	manifestPath := fs.String("manifest", "", "plugin manifest JSON file")
	source := fs.String("source", "", "plugin source URL, path, or marketplace id")
	marketplace := fs.String("marketplace", "", "marketplace catalog URL")
	sourceID := fs.String("source-id", "", "marketplace source id")
	version := fs.String("version", "", "plugin version")
	expectedChecksum := fs.String("expected-checksum", "", "expected sha256:<hex> manifest checksum")
	jsonOutput := fs.Bool("json", false, "print JSON output")
	if err := fs.Parse(args); err != nil {
		return err
	}
	pluginID := strings.TrimSpace(fs.Arg(0))
	if pluginID == "" {
		return fmt.Errorf("plugin upgrade requires a plugin id")
	}
	_, _, profile, err := loadRuntimeProfile(ctx, rt, *profileFlag)
	if err != nil {
		return err
	}
	req, err := pluginInstallRequest(pluginInstallOptions{
		PluginID:         pluginID,
		Source:           *source,
		ManifestPath:     *manifestPath,
		ExpectedChecksum: *expectedChecksum,
		MarketplaceURL:   firstNonEmptyString(*marketplace, profile.MarketplaceURL),
		SourceID:         firstNonEmptyString(*sourceID, profile.MarketplaceSourceID),
		Version:          *version,
	})
	if err != nil {
		return err
	}
	item, err := gatewayClient(rt, profile).UpgradePlugin(ctx, pluginID, req, gatewayHeaders(profile, "", "", "", "soha-cli"))
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writePrettyJSON(rt.Out, item)
	}
	_, err = fmt.Fprintf(rt.Out, "Upgraded plugin %s to %s\n", item.ID, item.Version)
	return err
}

func runPluginConfig(ctx context.Context, args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("plugin config", args, rt)
	profileFlag := fs.String("profile", "", "profile name")
	enabled := fs.Bool("enable", false, "enable plugin in the same request")
	metadataJSON := fs.String("metadata-json", "", "metadata JSON object")
	secretRefFlags := repeatableFlag{}
	fs.Var(&secretRefFlags, "secret-ref", "secret ref as name=secret://ref, repeatable")
	jsonOutput := fs.Bool("json", false, "print JSON output")
	if err := fs.Parse(args); err != nil {
		return err
	}
	pluginID := strings.TrimSpace(fs.Arg(0))
	if pluginID == "" {
		return fmt.Errorf("plugin config requires a plugin id")
	}
	var enabledValue *bool
	fs.Visit(func(flag *flag.Flag) {
		if flag.Name == "enable" {
			enabledValue = enabled
		}
	})
	metadata := map[string]any{}
	if strings.TrimSpace(*metadataJSON) != "" {
		parsed, err := parseJSONObject([]byte(*metadataJSON))
		if err != nil {
			return err
		}
		metadata = parsed
	}
	req := PluginConfigRequest{
		Enabled:    enabledValue,
		SecretRefs: parseKeyValueFlags(secretRefFlags),
		Metadata:   metadata,
	}
	_, _, profile, err := loadRuntimeProfile(ctx, rt, *profileFlag)
	if err != nil {
		return err
	}
	item, err := gatewayClient(rt, profile).ConfigurePlugin(ctx, pluginID, req, gatewayHeaders(profile, "", "", "", "soha-cli"))
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writePrettyJSON(rt.Out, item)
	}
	_, err = fmt.Fprintf(rt.Out, "Configured plugin %s\n", item.ID)
	return err
}

func runPluginRemove(ctx context.Context, args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("plugin remove", args, rt)
	profileFlag := fs.String("profile", "", "profile name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	pluginID := strings.TrimSpace(fs.Arg(0))
	if pluginID == "" {
		return fmt.Errorf("plugin remove requires a plugin id")
	}
	_, _, profile, err := loadRuntimeProfile(ctx, rt, *profileFlag)
	if err != nil {
		return err
	}
	if err := gatewayClient(rt, profile).RemovePlugin(ctx, pluginID, gatewayHeaders(profile, "", "", "", "soha-cli")); err != nil {
		return err
	}
	_, err = fmt.Fprintf(rt.Out, "Removed plugin %s\n", pluginID)
	return err
}

type pluginInstallOptions struct {
	PluginID         string
	Source           string
	ManifestPath     string
	ExpectedChecksum string
	Enable           bool
	MarketplaceURL   string
	SourceID         string
	Version          string
}

func pluginInstallRequest(options pluginInstallOptions) (PluginInstallRequest, error) {
	req := PluginInstallRequest{
		PluginID:         strings.TrimSpace(options.PluginID),
		Source:           strings.TrimSpace(options.Source),
		ExpectedChecksum: strings.TrimSpace(options.ExpectedChecksum),
		Enable:           options.Enable,
		MarketplaceURL:   normalizeServerURL(options.MarketplaceURL),
		SourceID:         strings.TrimSpace(options.SourceID),
		Version:          strings.TrimSpace(options.Version),
	}
	if strings.TrimSpace(options.ManifestPath) != "" {
		manifest, err := readPluginManifest(options.ManifestPath)
		if err != nil {
			return PluginInstallRequest{}, err
		}
		req.Manifest = &manifest
		if req.PluginID == "" {
			req.PluginID = manifest.ID
		}
	}
	if req.PluginID == "" && req.Manifest == nil {
		return PluginInstallRequest{}, fmt.Errorf("plugin id or --manifest is required")
	}
	return req, nil
}

func readPluginManifest(path string) (PluginManifest, error) {
	// #nosec G304 -- path is explicitly supplied through the --manifest CLI option.
	raw, err := os.ReadFile(path)
	if err != nil {
		return PluginManifest{}, err
	}
	var manifest PluginManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return PluginManifest{}, err
	}
	return manifest, nil
}

type repeatableFlag []string

func (r *repeatableFlag) String() string {
	return strings.Join(*r, ",")
}

func (r *repeatableFlag) Set(value string) error {
	*r = append(*r, value)
	return nil
}

func parseKeyValueFlags(values []string) map[string]string {
	out := map[string]string{}
	for _, value := range values {
		key, val, ok := strings.Cut(value, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		if key != "" && val != "" {
			out[key] = val
		}
	}
	return out
}
