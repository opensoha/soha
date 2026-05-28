package sohacli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const defaultProfile = "default"

type Config struct {
	CurrentProfile string                   `json:"currentProfile"`
	Profiles       map[string]ProfileConfig `json:"profiles"`
}

type ProfileConfig struct {
	ServerURL    string    `json:"serverUrl"`
	AccessToken  string    `json:"accessToken,omitempty"`
	RefreshToken string    `json:"refreshToken,omitempty"`
	ExpiresAt    time.Time `json:"expiresAt,omitempty"`
	UserID       string    `json:"userId,omitempty"`
	UserName     string    `json:"userName,omitempty"`
	AIClientID   string    `json:"aiClientId,omitempty"`
	AIClientName string    `json:"aiClientName,omitempty"`
	SkillID      string    `json:"skillId,omitempty"`
	Source       string    `json:"source,omitempty"`
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
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o600)
}

func profileName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return defaultProfile
	}
	return value
}

func resolveProfile(cfg Config, name string) (string, ProfileConfig, error) {
	name = profileName(firstNonEmptyString(name, cfg.CurrentProfile))
	profile, ok := cfg.Profiles[name]
	if !ok {
		return name, ProfileConfig{}, fmt.Errorf("profile %q is not configured; run soha-cli login first", name)
	}
	if strings.TrimSpace(profile.ServerURL) == "" {
		return name, ProfileConfig{}, fmt.Errorf("profile %q has no server URL", name)
	}
	if strings.TrimSpace(profile.AccessToken) == "" {
		return name, ProfileConfig{}, fmt.Errorf("profile %q has no access token; run soha-cli login again", name)
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
