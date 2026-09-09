package radiusadapter

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/opensoha/soha/internal/networkidentity"
)

var identifierPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

type Config struct {
	RuntimeID      string
	NASID          string
	ControlURL     string
	CACertFile     string
	ClientCertFile string
	ClientKeyFile  string
	TLSServerName  string
	NASTarget      string
	SecretFile     string
	JournalFile    string
	RadclientPath  string
	PollInterval   time.Duration
	RequestTimeout time.Duration
	CommandTimeout time.Duration
	MaxClockSkew   time.Duration
}

func LoadConfig() (Config, error) {
	cfg := Config{
		RuntimeID: os.Getenv("SOHA_RADIUS_RUNTIME_ID"), NASID: os.Getenv("SOHA_RADIUS_NAS_ID"),
		ControlURL: os.Getenv("SOHA_RADIUS_CONTROL_URL"), CACertFile: os.Getenv("SOHA_RADIUS_CONTROL_CA_FILE"),
		ClientCertFile: os.Getenv("SOHA_RADIUS_CONTROL_CERT_FILE"), ClientKeyFile: os.Getenv("SOHA_RADIUS_CONTROL_KEY_FILE"),
		TLSServerName: os.Getenv("SOHA_RADIUS_CONTROL_SERVER_NAME"), NASTarget: os.Getenv("SOHA_RADIUS_NAS_TARGET"),
		SecretFile: os.Getenv("SOHA_RADIUS_SHARED_SECRET_FILE"), JournalFile: os.Getenv("SOHA_RADIUS_JOURNAL_FILE"),
		RadclientPath: valueOrDefault(os.Getenv("SOHA_RADIUS_RADCLIENT_PATH"), "/usr/bin/radclient"),
		PollInterval:  2 * time.Second, RequestTimeout: 5 * time.Second, CommandTimeout: 5 * time.Second, MaxClockSkew: 2 * time.Minute,
	}
	var err error
	for name, target := range map[string]*time.Duration{
		"SOHA_RADIUS_POLL_INTERVAL": &cfg.PollInterval, "SOHA_RADIUS_REQUEST_TIMEOUT": &cfg.RequestTimeout,
		"SOHA_RADIUS_COMMAND_TIMEOUT": &cfg.CommandTimeout, "SOHA_RADIUS_MAX_CLOCK_SKEW": &cfg.MaxClockSkew,
	} {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			*target, err = time.ParseDuration(value)
			if err != nil {
				return Config{}, fmt.Errorf("%s must be a duration: %w", name, err)
			}
		}
	}
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) validateIdentity() error {
	if !identifierPattern.MatchString(strings.TrimSpace(c.RuntimeID)) || !identifierPattern.MatchString(strings.TrimSpace(c.NASID)) {
		return fmt.Errorf("RADIUS runtime and NAS IDs must be valid network identifiers")
	}
	parsed, err := url.Parse(c.ControlURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return fmt.Errorf("SOHA_RADIUS_CONTROL_URL must be an HTTPS origin without credentials, query, or path")
	}
	if c.MaxClockSkew <= 0 || c.MaxClockSkew > 5*time.Minute {
		return fmt.Errorf("SOHA_RADIUS_MAX_CLOCK_SKEW must be between 1ns and 5m")
	}
	return nil
}

func (c Config) validate() error {
	if err := c.validateIdentity(); err != nil {
		return err
	}
	if c.PollInterval <= 0 || c.PollInterval > time.Minute || c.RequestTimeout <= 0 || c.RequestTimeout > time.Minute || c.CommandTimeout <= 0 || c.CommandTimeout > time.Minute {
		return fmt.Errorf("RADIUS adapter poll and request timeouts must be positive and at most 1m")
	}
	if _, _, err := net.SplitHostPort(c.NASTarget); err != nil {
		return fmt.Errorf("SOHA_RADIUS_NAS_TARGET must be host:port: %w", err)
	}
	for name, path := range map[string]string{
		"SOHA_RADIUS_CONTROL_CA_FILE": c.CACertFile, "SOHA_RADIUS_CONTROL_CERT_FILE": c.ClientCertFile,
		"SOHA_RADIUS_CONTROL_KEY_FILE": c.ClientKeyFile, "SOHA_RADIUS_SHARED_SECRET_FILE": c.SecretFile,
		"SOHA_RADIUS_RADCLIENT_PATH": c.RadclientPath,
	} {
		if err := regularFile(path); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	if !filepath.IsAbs(c.JournalFile) {
		return fmt.Errorf("SOHA_RADIUS_JOURNAL_FILE must be an absolute path")
	}
	secret, err := os.ReadFile(c.SecretFile)
	if err != nil {
		return fmt.Errorf("read RADIUS shared secret: %w", err)
	}
	if len(strings.TrimSpace(string(secret))) < 16 || len(secret) > 4096 {
		return fmt.Errorf("RADIUS shared secret file must contain between 16 and 4096 bytes")
	}
	return nil
}

func NewHTTPClient(cfg Config) (*http.Client, error) {
	tlsConfig, err := networkidentity.LoadClientTLS(cfg.ClientCertFile, cfg.ClientKeyFile, cfg.CACertFile, cfg.TLSServerName)
	if err != nil {
		return nil, fmt.Errorf("load RADIUS adapter TLS: %w", err)
	}
	baseTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("default HTTP transport is not configurable")
	}
	transport := baseTransport.Clone()
	transport.TLSClientConfig = tlsConfig
	return &http.Client{Transport: transport, Timeout: cfg.RequestTimeout}, nil
}

func regularFile(path string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("file path is required")
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%q is not a regular file", path)
	}
	return nil
}

func valueOrDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
