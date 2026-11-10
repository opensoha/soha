package config

import (
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

type DSHRuntimeConfig struct {
	Enabled           bool          `mapstructure:"enabled"`
	BaseURL           string        `mapstructure:"base_url"`
	ServiceTokenFile  string        `mapstructure:"service_token_file"`
	Timeout           time.Duration `mapstructure:"timeout"`
	AllowInsecureHTTP bool          `mapstructure:"allow_insecure_http"`
}
type HindsightConfig struct {
	Enabled           bool          `mapstructure:"enabled"`
	BaseURL           string        `mapstructure:"base_url"`
	TokenFile         string        `mapstructure:"token_file"`
	InstanceNamespace string        `mapstructure:"instance_namespace"`
	Timeout           time.Duration `mapstructure:"timeout"`
}

func validRuntimeValue(s string) bool {
	return s != "" && strings.TrimSpace(s) == s && !strings.ContainsRune(s, '\x00')
}
func validRuntimeURL(raw string, allowHTTP bool, originOnly bool) bool {
	if !validRuntimeValue(raw) {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(raw, "#") || (originOnly && u.Path != "" && u.Path != "/") {
		return false
	}
	loopback := strings.EqualFold(u.Hostname(), "localhost") || net.ParseIP(u.Hostname()) != nil && net.ParseIP(u.Hostname()).IsLoopback()
	return u.Scheme == "https" || u.Scheme == "http" && (allowHTTP || loopback)
}
func (c DSHRuntimeConfig) problems() []string {
	if !c.Enabled {
		return nil
	}
	var p []string
	if !validRuntimeURL(c.BaseURL, c.AllowInsecureHTTP, false) {
		p = append(p, "dsh_runtime.base_url must be a valid permitted HTTP(S) URL")
	}
	if !validRuntimeValue(c.ServiceTokenFile) {
		p = append(p, "dsh_runtime.service_token_file is required")
	}
	if c.Timeout <= 0 || c.Timeout > 5*time.Minute {
		p = append(p, "dsh_runtime.timeout must be within 5 minutes")
	}
	return p
}
func (c HindsightConfig) problems() []string {
	if !c.Enabled {
		return nil
	}
	var p []string
	if !validRuntimeURL(c.BaseURL, true, false) {
		p = append(p, "hindsight.base_url must be a valid HTTP(S) URL")
	}
	if !validRuntimeValue(c.TokenFile) {
		p = append(p, "hindsight.token_file is required")
	}
	if !validRuntimeValue(c.InstanceNamespace) {
		p = append(p, "hindsight.instance_namespace is required")
	}
	if c.Timeout <= 0 || c.Timeout > 30*time.Second {
		p = append(p, fmt.Sprintf("hindsight.timeout must be within %s", 30*time.Second))
	}
	return p
}
