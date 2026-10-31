package sohacli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultProfile   = "default"
	defaultServerURL = "https://mcp.opensoha.com"
)

type Config struct {
	SchemaVersion  int                      `json:"schemaVersion,omitempty"`
	CurrentProfile string                   `json:"currentProfile"`
	Profiles       map[string]ProfileConfig `json:"profiles"`
}

type ProfileConfig struct {
	CredentialRef       string              `json:"credentialRef,omitempty"`
	CredentialStore     string              `json:"credentialStore,omitempty"`
	CredentialKind      string              `json:"credentialKind,omitempty"`
	ProtocolVersion     int                 `json:"protocolVersion,omitempty"`
	InsecureHTTP        bool                `json:"insecureHttp,omitempty"`
	credential          *credentialEnvelope `json:"-"`
	ServerURL           string              `json:"serverUrl"`
	AccessToken         string              `json:"accessToken,omitempty"`
	RefreshToken        string              `json:"refreshToken,omitempty"`
	ExpiresAt           time.Time           `json:"expiresAt,omitempty"`
	UserID              string              `json:"userId,omitempty"`
	UserName            string              `json:"userName,omitempty"`
	AIClientID          string              `json:"aiClientId,omitempty"`
	AIClientName        string              `json:"aiClientName,omitempty"`
	SkillID             string              `json:"skillId,omitempty"`
	Source              string              `json:"source,omitempty"`
	MarketplaceURL      string              `json:"marketplaceUrl,omitempty"`
	MarketplaceSourceID string              `json:"marketplaceSourceId,omitempty"`

	runtimeName    string `json:"-"`
	refreshEnabled bool   `json:"-"`
	refreshPersist bool   `json:"-"`
}

func defaultConfigPath() string {
	if value := strings.TrimSpace(os.Getenv("SOHA_CONFIG")); value != "" {
		return value
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ".soha/config.json"
	}
	return filepath.Join(home, ".soha", "config.json")
}

func loadConfig(path string) (Config, error) {
	cfg := Config{CurrentProfile: defaultProfile, Profiles: map[string]ProfileConfig{}}
	if err := rejectConfigSymlink(path); err != nil {
		return Config{}, err
	}
	// #nosec G304 -- path is the configured Soha profile file, including the documented SOHA_CONFIG override.
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return Config{}, err
	}
	if len(raw) == 0 {
		return cfg, nil
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Config{}, err
	}
	if cfg.SchemaVersion > 2 {
		return Config{}, fmt.Errorf("unsupported_config_version: this CLI cannot safely edit the newer configuration format")
	}
	if cfg.CurrentProfile == "" {
		cfg.CurrentProfile = defaultProfile
	}
	if cfg.Profiles == nil {
		cfg.Profiles = map[string]ProfileConfig{}
	}
	return cfg, nil
}

func saveConfig(path string, cfg Config) error {
	if cfg.CurrentProfile == "" {
		cfg.CurrentProfile = defaultProfile
	}
	if cfg.Profiles == nil {
		cfg.Profiles = map[string]ProfileConfig{}
	}
	// Copy before stripping secrets; runtime profiles may contain loaded envelopes.
	sanitized := make(map[string]ProfileConfig, len(cfg.Profiles))
	for name, p := range cfg.Profiles {
		if p.CredentialRef != "" {
			p.AccessToken = ""
			p.RefreshToken = ""
		}
		sanitized[name] = p
	}
	cfg.Profiles = sanitized
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	_, dirErr := os.Lstat(filepath.Dir(path))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := rejectConfigSymlink(path); err != nil {
		return err
	}
	if err := requirePrivateConfigDirectory(filepath.Dir(path), os.IsNotExist(dirErr)); err != nil {
		return err
	}
	return writeAtomicFile(path, append(raw, '\n'), 0o600)
}

func rejectConfigSymlink(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("profile config must be a regular file")
	}
	return nil
}

func sameServerURL(a, b string) bool {
	x, err := canonicalCredentialURL(a)
	if err != nil {
		return false
	}
	y, err := canonicalCredentialURL(b)
	return err == nil && x.String() == y.String()
}

func profileName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return defaultProfile
	}
	return value
}

func defaultServerFromEnv() string {
	return normalizeServerURL(firstNonEmptyString(env("SOHA_SERVER"), defaultServerURL))
}

func normalizeServerURL(value string) string {
	value = strings.TrimRight(strings.TrimSpace(value), "/")
	if value == "" {
		return ""
	}
	lower := strings.ToLower(value)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		value = "https://" + value
	}
	return value
}

func resolveProfile(cfg Config, name string) (string, ProfileConfig, error) {
	name = profileName(firstNonEmptyString(name, cfg.CurrentProfile))
	profile, ok := cfg.Profiles[name]
	if !ok {
		return name, ProfileConfig{}, fmt.Errorf("profile %q is not configured; run soha login first", name)
	}
	if strings.TrimSpace(profile.ServerURL) == "" {
		return name, ProfileConfig{}, fmt.Errorf("profile %q has no server URL", name)
	}
	if strings.TrimSpace(profile.AccessToken) == "" && profile.CredentialRef == "" {
		return name, ProfileConfig{}, fmt.Errorf("profile %q has no access token; run soha login again", name)
	}
	return name, profile, nil
}

func redactToken(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 12 {
		if value == "" {
			return ""
		}
		return "***"
	}
	return value[:8] + "..." + value[len(value)-4:]
}
