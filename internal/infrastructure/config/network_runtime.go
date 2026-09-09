package config

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-viper/mapstructure/v2"
	"github.com/opensoha/soha/internal/platform/keyring"
	"github.com/spf13/viper"
	"go.uber.org/zap/zapcore"
)

type MutualTLSConfig struct {
	CertFile     string `mapstructure:"cert_file"`
	KeyFile      string `mapstructure:"key_file"`
	ClientCAFile string `mapstructure:"client_ca_file"`
}

type NetworkControlConfig struct {
	HTTP                     HTTPConfig      `mapstructure:"http"`
	Logger                   LoggerConfig    `mapstructure:"logger"`
	Database                 DatabaseConfig  `mapstructure:"database"`
	TLS                      MutualTLSConfig `mapstructure:"tls"`
	SnapshotRefreshInterval  time.Duration   `mapstructure:"snapshot_refresh_interval"`
	MaxBodyBytes             int64           `mapstructure:"max_body_bytes"`
	RequestsPerMinute        int             `mapstructure:"requests_per_minute"`
	MaxClockSkew             time.Duration   `mapstructure:"max_clock_skew"`
	ConfigurationTTL         time.Duration   `mapstructure:"configuration_ttl"`
	LeaseTTL                 time.Duration   `mapstructure:"lease_ttl"`
	CredentialEncryptionKeys keyring.Ring    `mapstructure:"-"`
}

type IngestConfig struct {
	HTTP              HTTPConfig      `mapstructure:"http"`
	Logger            LoggerConfig    `mapstructure:"logger"`
	Database          DatabaseConfig  `mapstructure:"database"`
	TLS               MutualTLSConfig `mapstructure:"tls"`
	MaxBodyBytes      int64           `mapstructure:"max_body_bytes"`
	MaxEventsPerBatch int             `mapstructure:"max_events_per_batch"`
	RequestsPerMinute int             `mapstructure:"requests_per_minute"`
	MaxClockSkew      time.Duration   `mapstructure:"max_clock_skew"`
	Retention         time.Duration   `mapstructure:"retention"`
}

type networkRuntimeConfig struct {
	NetworkControl NetworkControlConfig `mapstructure:"network_control"`
	Ingest         IngestConfig         `mapstructure:"ingest"`
	Security       struct {
		CredentialEncryptionKey string `mapstructure:"credential_encryption_key"`
	} `mapstructure:"security"`
}

func LoadNetworkControlConfig() (NetworkControlConfig, error) {
	cfg, err := loadNetworkRuntimeConfig()
	if err != nil {
		return NetworkControlConfig{}, err
	}
	if err := cfg.NetworkControl.Validate(); err != nil {
		return NetworkControlConfig{}, err
	}
	return cfg.NetworkControl, nil
}

func LoadIngestConfig() (IngestConfig, error) {
	cfg, err := loadNetworkRuntimeConfig()
	if err != nil {
		return IngestConfig{}, err
	}
	if err := cfg.Ingest.Validate(); err != nil {
		return IngestConfig{}, err
	}
	return cfg.Ingest, nil
}

func loadNetworkRuntimeConfig() (networkRuntimeConfig, error) {
	v := newConfigViper("SOHA", "SOHA_CONFIG_FILE", "config", "configs", ".")
	setNetworkRuntimeDefaults(v)
	if err := readConfig(v); err != nil {
		return networkRuntimeConfig{}, err
	}

	var cfg networkRuntimeConfig
	if err := v.Unmarshal(&cfg, viper.DecodeHook(mapstructure.ComposeDecodeHookFunc(
		mapstructure.StringToTimeDurationHookFunc(),
	))); err != nil {
		return networkRuntimeConfig{}, fmt.Errorf("unmarshal network runtime config: %w", err)
	}
	cfg.expandEnv()
	key, err := keyring.NewKey(encryptionKeyID, cfg.Security.CredentialEncryptionKey, time.Unix(0, 0).UTC(), nil)
	if err != nil {
		return networkRuntimeConfig{}, fmt.Errorf("build network control credential key: %w", err)
	}
	cfg.NetworkControl.CredentialEncryptionKeys, err = keyring.New(key, nil)
	if err != nil {
		return networkRuntimeConfig{}, fmt.Errorf("build network control credential keyring: %w", err)
	}
	return cfg, nil
}

func (c NetworkControlConfig) Validate() error {
	problems := validateRuntimeConfig("network_control", c.HTTP, c.Logger, c.Database, c.TLS)
	if c.SnapshotRefreshInterval <= 0 {
		problems = append(problems, "network_control.snapshot_refresh_interval must be positive")
	}
	if c.MaxBodyBytes <= 0 {
		problems = append(problems, "network_control.max_body_bytes must be positive")
	}
	if c.RequestsPerMinute <= 0 {
		problems = append(problems, "network_control.requests_per_minute must be positive")
	}
	if c.MaxClockSkew <= 0 {
		problems = append(problems, "network_control.max_clock_skew must be positive")
	}
	if c.ConfigurationTTL <= 0 || c.ConfigurationTTL > 5*time.Minute {
		problems = append(problems, "network_control.configuration_ttl must be between 1ns and 5m")
	}
	if c.LeaseTTL <= 0 || c.LeaseTTL > 5*time.Minute {
		problems = append(problems, "network_control.lease_ttl must be between 1ns and 5m")
	}
	return runtimeConfigError(problems)
}

func (c IngestConfig) Validate() error {
	problems := validateRuntimeConfig("ingest", c.HTTP, c.Logger, c.Database, c.TLS)
	if c.MaxBodyBytes <= 0 {
		problems = append(problems, "ingest.max_body_bytes must be positive")
	}
	if c.MaxEventsPerBatch < 1 || c.MaxEventsPerBatch > 1000 {
		problems = append(problems, "ingest.max_events_per_batch must be between 1 and 1000")
	}
	if c.RequestsPerMinute <= 0 {
		problems = append(problems, "ingest.requests_per_minute must be positive")
	}
	if c.MaxClockSkew <= 0 {
		problems = append(problems, "ingest.max_clock_skew must be positive")
	}
	if c.Retention <= 0 {
		problems = append(problems, "ingest.retention must be positive")
	}
	return runtimeConfigError(problems)
}

func (c *networkRuntimeConfig) expandEnv() {
	c.Security.CredentialEncryptionKey = os.ExpandEnv(c.Security.CredentialEncryptionKey)
	for _, item := range []struct {
		database *DatabaseConfig
		tls      *MutualTLSConfig
	}{
		{database: &c.NetworkControl.Database, tls: &c.NetworkControl.TLS},
		{database: &c.Ingest.Database, tls: &c.Ingest.TLS},
	} {
		item.database.Password = os.ExpandEnv(item.database.Password)
		item.database.MigrationPath = os.ExpandEnv(item.database.MigrationPath)
		item.database.MigrationFile = os.ExpandEnv(item.database.MigrationFile)
		item.tls.CertFile = os.ExpandEnv(item.tls.CertFile)
		item.tls.KeyFile = os.ExpandEnv(item.tls.KeyFile)
		item.tls.ClientCAFile = os.ExpandEnv(item.tls.ClientCAFile)
	}
}

func validateRuntimeConfig(prefix string, httpConfig HTTPConfig, loggerConfig LoggerConfig, database DatabaseConfig, tls MutualTLSConfig) []string {
	problems := make([]string, 0)
	if err := validateListenAddress(httpConfig.Addr); err != nil {
		problems = append(problems, fmt.Sprintf("%s.http.addr %s", prefix, err))
	}
	if httpConfig.ReadTimeout <= 0 || httpConfig.WriteTimeout <= 0 || httpConfig.IdleTimeout <= 0 {
		problems = append(problems, prefix+".http timeouts must be positive")
	}
	if httpConfig.MaxHeaderBytes <= 0 {
		problems = append(problems, prefix+".http.max_header_bytes must be positive")
	}
	var level zapcore.Level
	if err := level.UnmarshalText([]byte(strings.ToLower(strings.TrimSpace(loggerConfig.Level)))); err != nil {
		problems = append(problems, fmt.Sprintf("%s.logger.level %q is invalid", prefix, loggerConfig.Level))
	}
	switch strings.ToLower(strings.TrimSpace(loggerConfig.Format)) {
	case "console", "json":
	default:
		problems = append(problems, fmt.Sprintf("%s.logger.format %q must be console or json", prefix, loggerConfig.Format))
	}
	if normalizedDatabaseDriver(database.Driver) != "postgres" {
		problems = append(problems, prefix+".database.driver must be postgres")
	}
	for name, value := range map[string]string{
		"host": database.Host,
		"name": database.Name,
		"user": database.User,
	} {
		if strings.TrimSpace(value) == "" {
			problems = append(problems, fmt.Sprintf("%s.database.%s is required", prefix, name))
		}
	}
	if database.Port < 1 || database.Port > 65535 {
		problems = append(problems, prefix+".database.port must be between 1 and 65535")
	}
	if database.MaxOpenConns <= 0 || database.MaxIdleConns < 0 || database.MaxIdleConns > database.MaxOpenConns {
		problems = append(problems, prefix+".database connection limits are invalid")
	}
	if database.ConnMaxLifetime <= 0 {
		problems = append(problems, prefix+".database.conn_max_lifetime must be positive")
	}
	for name, value := range map[string]string{
		"cert_file":      tls.CertFile,
		"key_file":       tls.KeyFile,
		"client_ca_file": tls.ClientCAFile,
	} {
		if strings.TrimSpace(value) == "" {
			problems = append(problems, fmt.Sprintf("%s.tls.%s is required", prefix, name))
		}
	}
	return problems
}

func validateListenAddress(address string) error {
	_, rawPort, err := net.SplitHostPort(strings.TrimSpace(address))
	if err != nil {
		return fmt.Errorf("must be a host:port address")
	}
	port, err := strconv.Atoi(rawPort)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("must use a port between 1 and 65535")
	}
	return nil
}

func runtimeConfigError(problems []string) error {
	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("network runtime config validation failed: %s", strings.Join(problems, "; "))
}

func setNetworkRuntimeDefaults(v *viper.Viper) {
	setRuntimeDefaults(v, "network_control", ":8082", "soha", "migrations", false, 8, 4)
	setRuntimeDefaults(v, "ingest", ":8083", "soha_ingest", "migrations/network-ingest/postgres", true, 12, 6)
	v.SetDefault("network_control.snapshot_refresh_interval", "1s")
	v.SetDefault("network_control.max_body_bytes", 1<<20)
	v.SetDefault("network_control.requests_per_minute", 600)
	v.SetDefault("network_control.max_clock_skew", "5m")
	v.SetDefault("network_control.configuration_ttl", "5m")
	v.SetDefault("network_control.lease_ttl", "5m")
	v.SetDefault("security.credential_encryption_key", defaultSystemSecret)
	v.SetDefault("ingest.max_body_bytes", 8<<20)
	v.SetDefault("ingest.max_events_per_batch", 1000)
	v.SetDefault("ingest.requests_per_minute", 1200)
	v.SetDefault("ingest.max_clock_skew", "5m")
	v.SetDefault("ingest.retention", "168h")
}

func setRuntimeDefaults(v *viper.Viper, prefix, address, databaseName, migrationPath string, autoMigrate bool, maxOpen, maxIdle int) {
	for key, value := range map[string]any{
		"http.addr":                  address,
		"http.read_timeout":          "10s",
		"http.write_timeout":         "10s",
		"http.idle_timeout":          "60s",
		"http.max_header_bytes":      64 << 10,
		"logger.level":               "info",
		"logger.format":              "json",
		"database.driver":            "postgres",
		"database.host":              "localhost",
		"database.port":              5432,
		"database.name":              databaseName,
		"database.user":              "pgsql",
		"database.password":          "pgsql",
		"database.sslmode":           "disable",
		"database.max_open_conns":    maxOpen,
		"database.max_idle_conns":    maxIdle,
		"database.conn_max_lifetime": "1h",
		"database.auto_migrate":      autoMigrate,
		"database.migration_path":    migrationPath,
	} {
		v.SetDefault(prefix+"."+key, value)
	}
}
