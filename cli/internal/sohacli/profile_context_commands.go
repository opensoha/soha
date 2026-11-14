package sohacli

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

func runProfile(ctx context.Context, args []string, rt Runtime) error {
	out := newCheckedWriter(rt.Out)
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
			out.Printf("%s %s\t%s\n", marker, name, cfg.Profiles[name].ServerURL)
		}
	case "use":
		unlock, e := lockConfig(ctx, rt)
		if e != nil {
			return e
		}
		defer unlock()
		cfg, err = loadConfig(rt.ConfigPath)
		if err != nil {
			return err
		}
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
		out.Printf("Current profile: %s\n", name)
	case "remove":
		if len(args) != 2 {
			return usageError{message: "profile remove requires one profile name"}
		}
		return removeLocalProfile(ctx, rt, profileName(args[1]))
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
	return out.Err()
}

func runContext(ctx context.Context, args []string, rt Runtime) error {
	if len(args) == 0 {
		args = []string{"show"}
	}
	switch args[0] {
	case "show":
		fs := newRuntimeFlagSet("context show", args[1:], rt)
		profileFlag := fs.String("profile", "", "profile name")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		_, name, profile, err := loadLocalRuntimeProfile(rt, *profileFlag)
		if err != nil {
			return err
		}
		return writePrettyJSON(rt.Out, map[string]any{
			"profile":             name,
			"serverUrl":           profile.ServerURL,
			"aiClientId":          profile.AIClientID,
			"aiClientName":        profile.AIClientName,
			"skillId":             profile.SkillID,
			"source":              profile.Source,
			"marketplaceUrl":      profile.MarketplaceURL,
			"marketplaceSourceId": profile.MarketplaceSourceID,
		})
	case "set":
		fs := newRuntimeFlagSet("context set", args[1:], rt)
		profileFlag := fs.String("profile", "", "profile name")
		aiClientID := fs.String("ai-client-id", "", "AI client id")
		aiClientName := fs.String("ai-client", "", "AI client display name")
		skillID := fs.String("skill-id", "", "skill id")
		source := fs.String("source", "", "request source label")
		marketplace := fs.String("marketplace", "", "default marketplace catalog URL")
		marketplaceSourceID := fs.String("marketplace-source-id", "", "default marketplace source id")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		unlock, e := lockConfig(ctx, rt)
		if e != nil {
			return e
		}
		defer unlock()
		cfg, name, profile, err := loadLocalRuntimeProfile(rt, *profileFlag)
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
		if *marketplace != "" {
			profile.MarketplaceURL = normalizeServerURL(*marketplace)
		}
		if *marketplaceSourceID != "" {
			profile.MarketplaceSourceID = strings.TrimSpace(*marketplaceSourceID)
		}
		cfg.Profiles[name] = profile
		if err := saveConfig(rt.ConfigPath, cfg); err != nil {
			return err
		}
		_, err = fmt.Fprintf(rt.Out, "Updated context for profile %s\n", name)
		return err
	default:
		return fmt.Errorf("unknown context command %q", args[0])
	}
}

func loadLocalRuntimeProfile(rt Runtime, requested string) (Config, string, ProfileConfig, error) {
	cfg, err := loadConfig(rt.ConfigPath)
	if err != nil {
		return Config{}, "", ProfileConfig{}, err
	}
	name, profile, err := resolveProfile(cfg, requested)
	if err != nil {
		return Config{}, "", ProfileConfig{}, err
	}
	if strings.TrimSpace(profile.Source) == "" {
		profile.Source = "soha"
	}
	return cfg, name, profile, nil
}

func removeLocalProfile(ctx context.Context, rt Runtime, name string) error {
	cfg, err := loadConfig(rt.ConfigPath)
	if err != nil {
		return err
	}
	expected, ok := cfg.Profiles[name]
	if !ok {
		return fmt.Errorf("profile is not configured")
	}
	unlock, err := lockProfile(ctx, rt, expected.ServerURL, name)
	if err != nil {
		return err
	}
	defer unlock()
	unlockConfig, err := lockConfig(ctx, rt)
	if err != nil {
		return err
	}
	defer unlockConfig()
	cfg, err = loadConfig(rt.ConfigPath)
	if err != nil {
		return err
	}
	p, ok := cfg.Profiles[name]
	if !ok {
		return nil
	}
	if !sameServerURL(p.ServerURL, expected.ServerURL) {
		return fmt.Errorf("profile_changed: retry profile removal")
	}
	delete(cfg.Profiles, name)
	if cfg.CurrentProfile == name {
		cfg.CurrentProfile = defaultProfile
		names := make([]string, 0, len(cfg.Profiles))
		for n := range cfg.Profiles {
			names = append(names, n)
		}
		sort.Strings(names)
		if len(names) > 0 {
			cfg.CurrentProfile = names[0]
		}
	}
	if err = saveConfig(rt.ConfigPath, cfg); err != nil {
		return err
	}
	if p.CredentialRef != "" {
		store, e := openCredentialStorage(rt, p.CredentialStore)
		if e != nil {
			return e
		}
		if e = store.Delete(p.CredentialRef); e != nil {
			return fmt.Errorf("profile removed; credential cleanup failed: %w", e)
		}
	}
	if env("SOHA_TOKEN") != "" {
		_, _ = fmt.Fprintln(rt.Err, "Profile removed; environment authentication remains active until SOHA_TOKEN is unset.")
	}
	_, err = fmt.Fprintf(rt.Out, "Removed local profile %s. Server credentials remain active; use logout or Web session revocation to revoke them.\n", name)
	return err
}

func profileHasCredentials(p ProfileConfig) bool {
	return p.AccessToken != "" || p.RefreshToken != "" || p.CredentialRef != ""
}
func persistSetupProfile(ctx context.Context, rt Runtime, name string, desired ProfileConfig, overwriteID, overwriteName, overwriteSkill bool) error {
	unlock, err := lockProfile(ctx, rt, desired.ServerURL, name)
	if err != nil {
		return err
	}
	defer unlock()
	unlockConfig, err := lockConfig(ctx, rt)
	if err != nil {
		return err
	}
	defer unlockConfig()
	cfg, err := loadConfig(rt.ConfigPath)
	if err != nil {
		return err
	}
	current := cfg.Profiles[name]
	if profileHasCredentials(current) && !sameServerURL(current.ServerURL, desired.ServerURL) {
		return fmt.Errorf("server_mismatch: the profile changed while setup was running; its credentials were retained")
	}
	current.ServerURL = desired.ServerURL
	if overwriteID || current.AIClientID == "" {
		current.AIClientID = desired.AIClientID
	}
	if overwriteName || current.AIClientName == "" {
		current.AIClientName = desired.AIClientName
	}
	if overwriteSkill || current.SkillID == "" {
		current.SkillID = desired.SkillID
	}
	if current.Source == "" {
		current.Source = desired.Source
	}
	cfg.Profiles[name] = current
	return saveConfig(rt.ConfigPath, cfg)
}
