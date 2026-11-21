package appgateway

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/opensoha/soha/internal/platform/s3endpoint"
)

const (
	EnvListenAddr               = "SOHA_APP_GATEWAY_LISTEN_ADDR"
	EnvHTTPRedirectAddr         = "SOHA_APP_GATEWAY_HTTP_REDIRECT_ADDR"
	EnvHealthAddr               = "SOHA_APP_GATEWAY_HEALTH_ADDR"
	EnvAppsDomain               = "SOHA_APP_GATEWAY_APPS_DOMAIN"
	EnvPublicScheme             = "SOHA_APP_GATEWAY_PUBLIC_SCHEME"
	EnvTLSCertFile              = "SOHA_APP_GATEWAY_TLS_CERT_FILE"
	EnvTLSKeyFile               = "SOHA_APP_GATEWAY_TLS_KEY_FILE"
	EnvTrustedProxies           = "SOHA_APP_GATEWAY_TRUSTED_PROXIES"
	EnvCoreURL                  = "SOHA_APP_GATEWAY_CORE_URL"
	EnvPublicCoreURL            = "SOHA_APP_GATEWAY_PUBLIC_CORE_URL"
	EnvTokenFile                = "SOHA_APP_GATEWAY_TOKEN_FILE"         //nolint:gosec // G101: environment variable name, not a credential
	EnvToken                    = "SOHA_APP_GATEWAY_TOKEN"              //nolint:gosec // G101: environment variable name, not a credential
	EnvCookieSecretFile         = "SOHA_APP_GATEWAY_COOKIE_SECRET_FILE" //nolint:gosec // G101: environment variable name, not a credential
	EnvCookieSecret             = "SOHA_APP_GATEWAY_COOKIE_SECRET"      //nolint:gosec // G101: environment variable name, not a credential
	EnvS3Endpoint               = "SOHA_APP_GATEWAY_S3_ENDPOINT"
	EnvS3Bucket                 = "SOHA_APP_GATEWAY_S3_BUCKET"
	EnvS3Region                 = "SOHA_APP_GATEWAY_S3_REGION"
	EnvS3Prefix                 = "SOHA_APP_GATEWAY_S3_PREFIX"
	EnvS3PathStyle              = "SOHA_APP_GATEWAY_S3_PATH_STYLE"
	EnvS3BucketEndpoint         = "SOHA_APP_GATEWAY_S3_BUCKET_ENDPOINT"
	EnvS3Insecure               = "SOHA_APP_GATEWAY_S3_INSECURE"
	EnvS3AccessKeyFile          = "SOHA_APP_GATEWAY_S3_ACCESS_KEY_FILE"
	EnvS3SecretKeyFile          = "SOHA_APP_GATEWAY_S3_SECRET_KEY_FILE" //nolint:gosec // G101: environment variable name, not a credential
	EnvS3AccessKeyID            = "SOHA_APP_GATEWAY_S3_ACCESS_KEY_ID"
	EnvS3SecretAccessKey        = "SOHA_APP_GATEWAY_S3_SECRET_ACCESS_KEY" //nolint:gosec // G101: environment variable name, not a credential
	EnvDecisionCacheTTL         = "SOHA_APP_GATEWAY_DECISION_CACHE_TTL"
	EnvRuntimeNamespace         = "SOHA_APP_GATEWAY_RUNTIME_NAMESPACE"
	EnvRuntimeClusterDomain     = "SOHA_APP_GATEWAY_RUNTIME_CLUSTER_DOMAIN"
	DefaultRuntimeClusterDomain = "cluster.local"
)

const (
	DefaultListenAddr       = ":8090"
	DefaultHealthAddr       = ":8091"
	DefaultPublicScheme     = "https"
	DefaultS3Region         = "us-east-1"
	DefaultDecisionCacheTTL = time.Duration(0)
	MinCookieSecretBytes    = 32

	SecureSessionCookieName    = "__Host-soha_app_session"
	SecureOAuthStateCookieName = "__Host-soha_app_oauth_state"
	SessionCookieName          = "_soha_app_session"
	OAuthStateCookieName       = "_soha_app_oauth_state"
)

func sessionCookieName(secure bool) string {
	if secure {
		return SecureSessionCookieName
	}
	return SessionCookieName
}

func oauthStateCookieName(secure bool) string {
	if secure {
		return SecureOAuthStateCookieName
	}
	return OAuthStateCookieName
}

// Config holds runtime configuration for the standalone Workspace App Hosting gateway.
type Config struct {
	ListenAddr       string
	HTTPRedirectAddr string
	HealthAddr       string
	AppsDomain       string
	PublicScheme     string
	TLSCertFile      string
	TLSKeyFile       string
	TrustedProxies   []string
	CoreURL          string
	PublicCoreURL    string

	TokenFile        string
	Token            string
	CookieSecretFile string
	CookieSecret     string

	S3Endpoint        string
	S3Bucket          string
	S3Region          string
	S3Prefix          string
	S3PathStyle       bool
	S3BucketEndpoint  bool
	S3Insecure        bool
	S3AccessKeyFile   string
	S3SecretKeyFile   string
	S3AccessKeyID     string
	S3SecretAccessKey string

	DecisionCacheTTL     time.Duration
	RuntimeNamespace     string
	RuntimeClusterDomain string
	trustedProxyPrefixes []netip.Prefix
}

// LoadConfig reads and validates gateway configuration from process environment variables.
func LoadConfig() (Config, error) {
	return LoadConfigFromLookup(os.LookupEnv)
}

// LoadConfigFromLookup reads and validates gateway configuration using the supplied lookup function.
func LoadConfigFromLookup(lookup func(string) (string, bool)) (Config, error) {
	if lookup == nil {
		return Config{}, errors.New("environment lookup function is required")
	}
	get := func(key string) string {
		val, _ := lookup(key)
		return strings.TrimSpace(val)
	}
	pathStyle, err := parseEnvBool(get(EnvS3PathStyle), true, EnvS3PathStyle)
	if err != nil {
		return Config{}, err
	}
	bucketEndpoint, err := parseEnvBool(get(EnvS3BucketEndpoint), false, EnvS3BucketEndpoint)
	if err != nil {
		return Config{}, err
	}
	insecure, err := parseEnvBool(get(EnvS3Insecure), false, EnvS3Insecure)
	if err != nil {
		return Config{}, err
	}
	ttl, err := parseCacheTTL(get(EnvDecisionCacheTTL))
	if err != nil {
		return Config{}, err
	}
	cfg := Config{
		ListenAddr:           defaultString(get(EnvListenAddr), DefaultListenAddr),
		HTTPRedirectAddr:     get(EnvHTTPRedirectAddr),
		HealthAddr:           defaultString(get(EnvHealthAddr), DefaultHealthAddr),
		AppsDomain:           get(EnvAppsDomain),
		PublicScheme:         defaultString(strings.ToLower(get(EnvPublicScheme)), DefaultPublicScheme),
		TLSCertFile:          get(EnvTLSCertFile),
		TLSKeyFile:           get(EnvTLSKeyFile),
		TrustedProxies:       splitCSV(get(EnvTrustedProxies)),
		CoreURL:              get(EnvCoreURL),
		PublicCoreURL:        get(EnvPublicCoreURL),
		TokenFile:            get(EnvTokenFile),
		Token:                get(EnvToken),
		CookieSecretFile:     get(EnvCookieSecretFile),
		CookieSecret:         get(EnvCookieSecret),
		S3Endpoint:           get(EnvS3Endpoint),
		S3Bucket:             get(EnvS3Bucket),
		S3Region:             defaultString(get(EnvS3Region), DefaultS3Region),
		S3Prefix:             get(EnvS3Prefix),
		S3PathStyle:          pathStyle,
		S3BucketEndpoint:     bucketEndpoint,
		S3Insecure:           insecure,
		S3AccessKeyFile:      get(EnvS3AccessKeyFile),
		S3SecretKeyFile:      get(EnvS3SecretKeyFile),
		S3AccessKeyID:        get(EnvS3AccessKeyID),
		S3SecretAccessKey:    get(EnvS3SecretAccessKey),
		DecisionCacheTTL:     ttl,
		RuntimeNamespace:     get(EnvRuntimeNamespace),
		RuntimeClusterDomain: get(EnvRuntimeClusterDomain),
	}
	if err := cfg.NormalizeAndValidate(true); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// NormalizeAndValidate normalizes defaults and checks required invariants.
func (c *Config) NormalizeAndValidate(requireS3 bool) error {
	if c == nil {
		return errors.New("gateway config is required")
	}
	c.ListenAddr = defaultString(strings.TrimSpace(c.ListenAddr), DefaultListenAddr)
	c.HTTPRedirectAddr = strings.TrimSpace(c.HTTPRedirectAddr)
	c.HealthAddr = strings.TrimSpace(c.HealthAddr)
	c.PublicScheme = defaultString(strings.ToLower(strings.TrimSpace(c.PublicScheme)), DefaultPublicScheme)
	if c.PublicScheme != "https" && c.PublicScheme != "http" {
		return fmt.Errorf("invalid public scheme %q: must be https or http", c.PublicScheme)
	}
	domain, err := NormalizeAppsDomain(c.AppsDomain)
	if err != nil {
		return err
	}
	c.AppsDomain = domain
	if err := c.validateURLsAndListeners(); err != nil {
		return err
	}
	prefixes, err := parseTrustedProxies(c.TrustedProxies)
	if err != nil {
		return err
	}
	c.trustedProxyPrefixes = prefixes
	if c.DecisionCacheTTL < 0 {
		return errors.New("decision cache TTL must not be negative")
	}
	runtimeNamespace, err := validateRuntimeNamespace(c.RuntimeNamespace)
	if err != nil {
		return err
	}
	c.RuntimeNamespace = runtimeNamespace
	runtimeDomain, err := validateRuntimeClusterDomain(c.RuntimeClusterDomain)
	if err != nil {
		return err
	}
	c.RuntimeClusterDomain = runtimeDomain
	if c.RuntimeNamespace != "" {
		if c.RuntimeClusterDomain == "" {
			c.RuntimeClusterDomain = DefaultRuntimeClusterDomain
		}
		if c.DecisionCacheTTL != 0 {
			return errors.New("dynamic app hosting requires a zero decision cache TTL")
		}
	}
	if err := c.validateS3Config(requireS3); err != nil {
		return err
	}
	secrets := newSecretProvider(*c)
	return secrets.ValidateAll(requireS3)
}

func (c *Config) validateURLsAndListeners() error {
	if (c.TLSCertFile == "") != (c.TLSKeyFile == "") {
		return errors.New("TLS cert file and TLS key file must be configured together")
	}
	if c.HTTPRedirectAddr != "" && (c.HTTPRedirectAddr == c.ListenAddr || c.HTTPRedirectAddr == c.HealthAddr) {
		return errors.New("HTTP redirect listener address must not collide with main or health listener")
	}
	if c.HealthAddr != "" && c.HealthAddr == c.ListenAddr {
		return errors.New("health listener address must not collide with main listener")
	}
	coreURL, err := normalizeHTTPBaseURL(c.CoreURL, "core URL")
	if err != nil {
		return err
	}
	c.CoreURL = coreURL
	publicCoreURL, err := normalizeHTTPBaseURL(c.PublicCoreURL, "public core URL")
	if err != nil {
		return err
	}
	c.PublicCoreURL = publicCoreURL
	return nil
}

func (c *Config) validateS3Config(requireS3 bool) error {
	c.S3Region = defaultString(strings.TrimSpace(c.S3Region), DefaultS3Region)
	cleanedPrefix, ok := cleanStoragePrefix(c.S3Prefix)
	if !ok {
		return errors.New("invalid S3 prefix")
	}
	c.S3Prefix = cleanedPrefix
	c.S3Bucket = strings.TrimSpace(c.S3Bucket)
	if c.S3BucketEndpoint {
		if err := s3endpoint.ValidateBucketEndpoint(c.S3Endpoint, c.S3PathStyle, c.S3Insecure); err != nil {
			return err
		}
	}
	if !requireS3 && strings.TrimSpace(c.S3Endpoint) == "" && c.S3Bucket == "" {
		return nil
	}
	endpoint, err := normalizeHTTPBaseURL(c.S3Endpoint, "S3 endpoint")
	if err != nil {
		return err
	}
	c.S3Endpoint = endpoint
	if c.S3Bucket == "" || strings.Contains(c.S3Bucket, "/") {
		return errors.New("valid S3 bucket is required")
	}
	return nil
}

// NormalizeAppsDomain normalizes and validates the wildcard base domain (e.g. "apps.example.com").
func NormalizeAppsDomain(raw string) (string, error) {
	trimmed := strings.ToLower(strings.TrimSpace(raw))
	trimmed = strings.TrimPrefix(trimmed, "*.")
	trimmed = strings.TrimPrefix(trimmed, ".")
	trimmed = strings.TrimSuffix(trimmed, ".")
	if trimmed == "" || len(trimmed) > 253 {
		return "", errors.New("apps domain is required")
	}
	if strings.ContainsAny(trimmed, ":/\\?#@ \t\r\n") || net.ParseIP(trimmed) != nil {
		return "", fmt.Errorf("invalid apps domain %q", raw)
	}
	labels := strings.Split(trimmed, ".")
	if len(labels) < 2 {
		return "", fmt.Errorf("apps domain %q must have at least two DNS labels", raw)
	}
	for _, label := range labels {
		if !isValidDNSLabel(label) {
			return "", fmt.Errorf("apps domain %q contains invalid DNS label %q", raw, label)
		}
	}
	return trimmed, nil
}

func normalizeHTTPBaseURL(raw, fieldName string) (string, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(raw), "/")
	if trimmed == "" {
		return "", fmt.Errorf("%s is required", fieldName)
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("invalid %s", fieldName)
	}
	return trimmed, nil
}

func parseTrustedProxies(entries []string) ([]netip.Prefix, error) {
	if len(entries) == 0 {
		return nil, nil
	}
	out := make([]netip.Prefix, 0, len(entries))
	for _, entry := range entries {
		for _, token := range splitCSV(entry) {
			prefix, err := parsePrefixOrAddr(token)
			if err != nil {
				return nil, fmt.Errorf("invalid trusted proxy %q: %w", token, err)
			}
			out = append(out, prefix)
		}
	}
	return out, nil
}

func parsePrefixOrAddr(raw string) (netip.Prefix, error) {
	if strings.Contains(raw, "/") {
		prefix, err := netip.ParsePrefix(raw)
		if err != nil {
			return netip.Prefix{}, err
		}
		return prefix.Masked(), nil
	}
	addr, err := netip.ParseAddr(raw)
	if err != nil {
		return netip.Prefix{}, err
	}
	addr = addr.Unmap()
	return netip.PrefixFrom(addr, addr.BitLen()), nil
}

func parseEnvBool(raw string, defaultValue bool, name string) (bool, error) {
	if raw == "" {
		return defaultValue, nil
	}
	parsed, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("invalid boolean for %s", name)
	}
	return parsed, nil
}

func parseCacheTTL(raw string) (time.Duration, error) {
	if raw == "" {
		return DefaultDecisionCacheTTL, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("invalid duration for %s", EnvDecisionCacheTTL)
	}
	return d, nil
}

func splitCSV(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func defaultString(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}
