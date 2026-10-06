package networkproxyruntime

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/platform/securefile"
)

type Config struct {
	RuntimeID            string
	Engine               string
	ControlURL           string
	ControlCertFile      string
	ControlKeyFile       string
	ControlCAFile        string
	ControlServerName    string
	IngestURL            string
	IngestCertFile       string
	IngestKeyFile        string
	IngestCAFile         string
	IngestServerName     string
	ControllerURL        string
	ControllerSecretFile string
	EngineBinary         string
	ConfigFile           string
	StateDir             string
	EnrollmentID         string
	ChallengeID          string
	EnrollmentTokenFile  string
	PollInterval         time.Duration
}

func LoadConfig() (Config, error) {
	c := Config{
		RuntimeID:            strings.TrimSpace(os.Getenv("SOHA_PROXY_RUNTIME_ID")),
		Engine:               strings.TrimSpace(os.Getenv("SOHA_PROXY_ENGINE")),
		ControlURL:           strings.TrimSpace(os.Getenv("SOHA_PROXY_CONTROL_URL")),
		ControlCertFile:      os.Getenv("SOHA_PROXY_CONTROL_CERT_FILE"),
		ControlKeyFile:       os.Getenv("SOHA_PROXY_CONTROL_KEY_FILE"),
		ControlCAFile:        os.Getenv("SOHA_PROXY_CONTROL_CA_FILE"),
		ControlServerName:    os.Getenv("SOHA_PROXY_CONTROL_SERVER_NAME"),
		IngestURL:            strings.TrimSpace(os.Getenv("SOHA_PROXY_INGEST_URL")),
		IngestCertFile:       os.Getenv("SOHA_PROXY_INGEST_CERT_FILE"),
		IngestKeyFile:        os.Getenv("SOHA_PROXY_INGEST_KEY_FILE"),
		IngestCAFile:         os.Getenv("SOHA_PROXY_INGEST_CA_FILE"),
		IngestServerName:     os.Getenv("SOHA_PROXY_INGEST_SERVER_NAME"),
		ControllerURL:        strings.TrimSpace(os.Getenv("SOHA_PROXY_CONTROLLER_URL")),
		ControllerSecretFile: os.Getenv("SOHA_PROXY_CONTROLLER_SECRET_FILE"),
		EngineBinary:         os.Getenv("SOHA_PROXY_ENGINE_BINARY"),
		ConfigFile:           os.Getenv("SOHA_PROXY_CONFIG_FILE"),
		StateDir:             os.Getenv("SOHA_PROXY_STATE_DIR"),
		EnrollmentID:         os.Getenv("SOHA_PROXY_ENROLLMENT_ID"),
		ChallengeID:          os.Getenv("SOHA_PROXY_ENROLLMENT_CHALLENGE_ID"),
		EnrollmentTokenFile:  os.Getenv("SOHA_PROXY_ENROLLMENT_TOKEN_FILE"),
		PollInterval:         10 * time.Second,
	}
	if c.StateDir == "" {
		c.StateDir = "/var/lib/soha-proxy-runtime"
	}
	if c.ConfigFile == "" {
		c.ConfigFile = filepath.Join(c.StateDir, "engine.conf")
	}
	if err := c.validate(); err != nil {
		return Config{}, err
	}
	return c, nil
}

func (c Config) validate() error {
	if err := c.validateRequired(); err != nil {
		return err
	}
	if c.Engine != "mihomo" && c.Engine != "sing-box" && c.Engine != "v2ray" {
		return fmt.Errorf("proxy engine is invalid")
	}
	if err := loopbackHTTP(c.ControllerURL); err != nil {
		return err
	}
	if err := validateControlURL(c.ControlURL); err != nil {
		return err
	}
	if c.IngestURL == "" || c.IngestCertFile == "" || c.IngestKeyFile == "" || c.IngestCAFile == "" {
		return fmt.Errorf("proxy ingest URL and TLS identity are required")
	}
	if err := validateHTTPSOrigin(c.IngestURL, "proxy ingest"); err != nil {
		return err
	}
	if c.Engine != "v2ray" && c.ControllerSecretFile == "" {
		return fmt.Errorf("controller secret file is required")
	}
	if err := c.validateEnrollment(); err != nil {
		return err
	}
	if !filepath.IsAbs(c.ConfigFile) || !filepath.IsAbs(c.StateDir) {
		return fmt.Errorf("proxy state and config paths must be absolute")
	}
	return nil
}

func (c Config) validateRequired() error {
	if c.RuntimeID == "" || c.Engine == "" || c.ControlURL == "" || c.ControlCertFile == "" || c.ControlKeyFile == "" || c.ControlCAFile == "" || c.ControllerURL == "" || c.EngineBinary == "" {
		return fmt.Errorf("proxy runtime identity, control TLS and local controller settings are required")
	}
	return nil
}

func validateControlURL(raw string) error {
	return validateHTTPSOrigin(raw, "proxy control")
}

func validateHTTPSOrigin(raw, name string) error {
	control, err := url.Parse(raw)
	if err != nil || control.Scheme != "https" || control.Host == "" || control.User != nil || control.Path != "" || control.RawQuery != "" || control.Fragment != "" {
		return fmt.Errorf("%s URL must be an HTTPS origin", name)
	}
	return nil
}

func (c Config) validateEnrollment() error {
	if (c.EnrollmentID != "" || c.ChallengeID != "" || c.EnrollmentTokenFile != "") &&
		(c.EnrollmentID == "" || c.ChallengeID == "" || c.EnrollmentTokenFile == "") {
		return fmt.Errorf("proxy enrollment ID, challenge and token file must be set together")
	}
	return nil
}

func loopbackHTTP(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
		return fmt.Errorf("controller must be a loopback HTTP origin")
	}
	host, port, err := net.SplitHostPort(u.Host)
	portNumber, portErr := strconv.Atoi(port)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() || portErr != nil || portNumber < 1 || portNumber > 65535 {
		return fmt.Errorf("controller must use a loopback IP and port")
	}
	return nil
}

func (c Config) ControlClient() (*Client, error) {
	tlsConfig, err := networkidentity.LoadClientTLS(c.ControlCertFile, c.ControlKeyFile, c.ControlCAFile, c.ControlServerName)
	if err != nil {
		return nil, err
	}
	return NewClient(c.ControlURL, c.RuntimeID, tlsConfig), nil
}

func (c Config) IngestClient() (*IngestClient, error) {
	tlsConfig, err := networkidentity.LoadClientTLS(c.IngestCertFile, c.IngestKeyFile, c.IngestCAFile, c.IngestServerName)
	if err != nil {
		return nil, err
	}
	return NewIngestClient(c.IngestURL, c.RuntimeID, tlsConfig), nil
}

func (c Config) ControllerSecret() (string, error) {
	raw, err := securefile.Read(c.ControllerSecretFile, 4096, true)
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(raw))
	if len(value) < 16 || len(value) > 1024 {
		return "", fmt.Errorf("controller secret is invalid")
	}
	return value, nil
}

func (c Config) EnrollmentToken() (string, error) {
	raw, err := securefile.Read(c.EnrollmentTokenFile, 4096, true)
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(raw))
	if len(value) < 32 {
		return "", fmt.Errorf("enrollment token is invalid")
	}
	return value, nil
}
