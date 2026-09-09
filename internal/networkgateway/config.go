package networkgateway

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/platform/securefile"
)

type Config struct {
	RuntimeID               string
	DeviceID                string
	ControlURL              string
	ControlCAFile           string
	ControlCertFile         string
	ControlKeyFile          string
	ControlServerName       string
	IngestURL               string
	IngestCAFile            string
	IngestCertFile          string
	IngestKeyFile           string
	IngestServerName        string
	WireGuardPrivateKeyFile string
	IPPath                  string
	NFTPath                 string
	EgressInterface         string
	HealthAddress           string
	PollInterval            time.Duration
	HeartbeatInterval       time.Duration
	RequestTimeout          time.Duration
	MaxClockSkew            time.Duration
	EnrollmentID            string
	EnrollmentChallengeID   string
	EnrollmentTokenFile     string
}

func LoadConfig() (Config, error) {
	config := Config{
		RuntimeID: os.Getenv("SOHA_NETWORK_GATEWAY_RUNTIME_ID"), DeviceID: os.Getenv("SOHA_NETWORK_GATEWAY_DEVICE_ID"),
		ControlURL: os.Getenv("SOHA_NETWORK_GATEWAY_CONTROL_URL"), ControlCAFile: os.Getenv("SOHA_NETWORK_GATEWAY_CONTROL_CA_FILE"),
		ControlCertFile: os.Getenv("SOHA_NETWORK_GATEWAY_CONTROL_CERT_FILE"), ControlKeyFile: os.Getenv("SOHA_NETWORK_GATEWAY_CONTROL_KEY_FILE"), ControlServerName: os.Getenv("SOHA_NETWORK_GATEWAY_CONTROL_SERVER_NAME"),
		IngestURL: os.Getenv("SOHA_NETWORK_GATEWAY_INGEST_URL"), IngestCAFile: os.Getenv("SOHA_NETWORK_GATEWAY_INGEST_CA_FILE"), IngestCertFile: os.Getenv("SOHA_NETWORK_GATEWAY_INGEST_CERT_FILE"), IngestKeyFile: os.Getenv("SOHA_NETWORK_GATEWAY_INGEST_KEY_FILE"), IngestServerName: os.Getenv("SOHA_NETWORK_GATEWAY_INGEST_SERVER_NAME"),
		WireGuardPrivateKeyFile: valueOrDefault(os.Getenv("SOHA_NETWORK_GATEWAY_PRIVATE_KEY_FILE"), "/var/lib/soha-network-gateway/wireguard.key"),
		IPPath:                  valueOrDefault(os.Getenv("SOHA_NETWORK_GATEWAY_IP_PATH"), "/sbin/ip"), NFTPath: valueOrDefault(os.Getenv("SOHA_NETWORK_GATEWAY_NFT_PATH"), "/usr/sbin/nft"), EgressInterface: os.Getenv("SOHA_NETWORK_GATEWAY_EGRESS_INTERFACE"),
		HealthAddress: valueOrDefault(os.Getenv("SOHA_NETWORK_GATEWAY_HEALTH_ADDR"), ":8084"), PollInterval: time.Minute, HeartbeatInterval: time.Minute, RequestTimeout: 5 * time.Second, MaxClockSkew: 5 * time.Minute,
		EnrollmentID: os.Getenv("SOHA_NETWORK_GATEWAY_ENROLLMENT_ID"), EnrollmentChallengeID: os.Getenv("SOHA_NETWORK_GATEWAY_ENROLLMENT_CHALLENGE_ID"), EnrollmentTokenFile: os.Getenv("SOHA_NETWORK_GATEWAY_ENROLLMENT_TOKEN_FILE"),
	}
	for name, target := range map[string]*time.Duration{
		"SOHA_NETWORK_GATEWAY_POLL_INTERVAL": &config.PollInterval, "SOHA_NETWORK_GATEWAY_HEARTBEAT_INTERVAL": &config.HeartbeatInterval,
		"SOHA_NETWORK_GATEWAY_REQUEST_TIMEOUT": &config.RequestTimeout, "SOHA_NETWORK_GATEWAY_MAX_CLOCK_SKEW": &config.MaxClockSkew,
	} {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			parsed, err := time.ParseDuration(value)
			if err != nil {
				return Config{}, fmt.Errorf("%s must be a duration: %w", name, err)
			}
			*target = parsed
		}
	}
	if err := config.Validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func (c Config) Validate() error {
	if !identifierPattern.MatchString(strings.TrimSpace(c.RuntimeID)) || !identifierPattern.MatchString(strings.TrimSpace(c.DeviceID)) {
		return fmt.Errorf("network gateway runtime and device IDs are invalid")
	}
	if _, err := runtimeOrigin(c.ControlURL); err != nil {
		return err
	}
	if err := validateGatewayFiles(map[string]string{"control CA": c.ControlCAFile, "control certificate": c.ControlCertFile, "control key": c.ControlKeyFile}); err != nil {
		return err
	}
	if err := c.validateSystemConfig(); err != nil {
		return err
	}
	return c.validateIngestConfig()
}

func (c Config) validateSystemConfig() error {
	if !filepath.IsAbs(c.WireGuardPrivateKeyFile) || !filepath.IsAbs(c.IPPath) || !filepath.IsAbs(c.NFTPath) || (c.EgressInterface != "" && !linuxInterfacePattern.MatchString(c.EgressInterface)) {
		return fmt.Errorf("network gateway system paths or egress interface are invalid")
	}
	_, port, err := net.SplitHostPort(c.HealthAddress)
	if err != nil {
		return fmt.Errorf("network gateway health address must be host:port")
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return fmt.Errorf("network gateway health port is invalid")
	}
	if c.PollInterval <= 0 || c.PollInterval > 5*time.Minute || c.HeartbeatInterval <= 0 || c.HeartbeatInterval > 5*time.Minute || c.RequestTimeout <= 0 || c.RequestTimeout > time.Minute || c.MaxClockSkew <= 0 || c.MaxClockSkew > 5*time.Minute {
		return fmt.Errorf("network gateway intervals are outside safe bounds")
	}
	return nil
}

func (c Config) validateIngestConfig() error {
	ingestValues := []string{c.IngestURL, c.IngestCAFile, c.IngestCertFile, c.IngestKeyFile}
	configured := 0
	for _, value := range ingestValues {
		if strings.TrimSpace(value) != "" {
			configured++
		}
	}
	if configured != 0 && configured != len(ingestValues) {
		return fmt.Errorf("network ingest URL and separately scoped TLS files must be configured together")
	}
	if configured == len(ingestValues) {
		if _, err := runtimeOrigin(c.IngestURL); err != nil {
			return err
		}
		return validateGatewayFiles(map[string]string{"ingest CA": c.IngestCAFile, "ingest certificate": c.IngestCertFile, "ingest key": c.IngestKeyFile})
	}
	return nil
}

func validateGatewayFiles(files map[string]string) error {
	for name, path := range files {
		if err := regularFile(path); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}

func (c Config) ControlHTTPClient() (*http.Client, string, error) {
	tlsConfig, err := networkidentity.LoadClientTLS(c.ControlCertFile, c.ControlKeyFile, c.ControlCAFile, c.ControlServerName)
	if err != nil {
		return nil, "", err
	}
	if len(tlsConfig.Certificates) != 1 || len(tlsConfig.Certificates[0].Certificate) == 0 {
		return nil, "", fmt.Errorf("control client certificate is empty")
	}
	certificate, err := x509.ParseCertificate(tlsConfig.Certificates[0].Certificate[0])
	if err != nil {
		return nil, "", fmt.Errorf("parse control client certificate: %w", err)
	}
	publicKey, err := x509.MarshalPKIXPublicKey(certificate.PublicKey)
	if err != nil {
		return nil, "", fmt.Errorf("marshal control client public key: %w", err)
	}
	baseTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, "", fmt.Errorf("default HTTP transport is not configurable")
	}
	transport := baseTransport.Clone()
	transport.TLSClientConfig = tlsConfig
	return &http.Client{Transport: transport, Timeout: c.RequestTimeout, CheckRedirect: rejectRedirect}, string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicKey})), nil
}

func (c Config) IngestHTTPClient() (*http.Client, error) {
	if c.IngestURL == "" {
		return nil, nil
	}
	tlsConfig, err := networkidentity.LoadClientTLS(c.IngestCertFile, c.IngestKeyFile, c.IngestCAFile, c.IngestServerName)
	if err != nil {
		return nil, err
	}
	baseTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("default HTTP transport is not configurable")
	}
	transport := baseTransport.Clone()
	transport.TLSClientConfig = tlsConfig
	return &http.Client{Transport: transport, Timeout: c.RequestTimeout, CheckRedirect: rejectRedirect}, nil
}

func (c Config) Enrollment(privateKeyPublic, devicePublicKey, clientVersion string) (Enrollment, error) {
	if !identifierPattern.MatchString(c.EnrollmentID) || !identifierPattern.MatchString(c.EnrollmentChallengeID) {
		return Enrollment{}, fmt.Errorf("gateway enrollment ID and challenge ID are required")
	}
	raw, err := securefile.Read(c.EnrollmentTokenFile, 4096, true)
	if err != nil {
		return Enrollment{}, fmt.Errorf("read bounded gateway enrollment token: %w", err)
	}
	token := strings.TrimSpace(string(raw))
	if len(token) < 32 || strings.ContainsAny(token, "\r\n\t ") {
		return Enrollment{}, fmt.Errorf("gateway enrollment token is invalid")
	}
	return Enrollment{EnrollmentID: c.EnrollmentID, ChallengeID: c.EnrollmentChallengeID, DeviceID: c.DeviceID, DevicePublicKey: devicePublicKey, WireGuardPublicKey: privateKeyPublic, ClientVersion: clientVersion, Token: token}, nil
}

func rejectRedirect(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

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
