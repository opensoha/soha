package appconfig

import (
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

// CLIAuth applies only to newly issued v2 CLI sessions. Existing Web/App/v1
// session policies remain governed by Auth.JWT.
type CLIAuth struct {
	Enabled            bool          `mapstructure:"enabled"`
	PublicURL          string        `mapstructure:"public_url"`
	AllowPassword      bool          `mapstructure:"allow_password"`
	DeviceTTL          time.Duration `mapstructure:"device_ttl"`
	PollInterval       time.Duration `mapstructure:"poll_interval"`
	AccessTTL          time.Duration `mapstructure:"access_ttl"`
	IdleTTL            time.Duration `mapstructure:"idle_ttl"`
	AbsoluteTTL        time.Duration `mapstructure:"absolute_ttl"`
	CreateLimit        int           `mapstructure:"create_limit"`
	SourcePendingLimit int           `mapstructure:"source_pending_limit"`
	ActiveGrantLimit   int           `mapstructure:"active_grant_limit"`
	SourceAttemptLimit int           `mapstructure:"source_attempt_limit"`
	UserAttemptLimit   int           `mapstructure:"user_attempt_limit"`
}

func (c CLIAuth) WithDefaults() CLIAuth {
	if c.DeviceTTL == 0 {
		c.DeviceTTL = 5 * time.Minute
	}
	if c.PollInterval == 0 {
		c.PollInterval = 5 * time.Second
	}
	if c.AccessTTL == 0 {
		c.AccessTTL = 15 * time.Minute
	}
	if c.IdleTTL == 0 {
		c.IdleTTL = 7 * 24 * time.Hour
	}
	if c.AbsoluteTTL == 0 {
		c.AbsoluteTTL = 30 * 24 * time.Hour
	}
	if c.CreateLimit == 0 {
		c.CreateLimit = 10
	}
	if c.SourcePendingLimit == 0 {
		c.SourcePendingLimit = 5
	}
	if c.ActiveGrantLimit == 0 {
		c.ActiveGrantLimit = 1000
	}
	if c.SourceAttemptLimit == 0 {
		c.SourceAttemptLimit = 20
	}
	if c.UserAttemptLimit == 0 {
		c.UserAttemptLimit = 5
	}
	return c
}

func (c CLIAuth) Validate() error {
	c = c.WithDefaults()
	if c.DeviceTTL < 30*time.Second || c.DeviceTTL > 10*time.Minute || c.PollInterval < time.Second || c.PollInterval > 30*time.Second {
		return fmt.Errorf("CLI device TTL must be 30s..10m and polling interval 1s..30s")
	}
	if c.AccessTTL < time.Second || c.IdleTTL < c.AccessTTL || c.AbsoluteTTL < c.IdleTTL || c.AbsoluteTTL > 365*24*time.Hour {
		return fmt.Errorf("CLI session TTLs must be finite with access <= idle <= absolute <= 365d")
	}
	for _, n := range []int{c.CreateLimit, c.SourcePendingLimit, c.ActiveGrantLimit, c.SourceAttemptLimit, c.UserAttemptLimit} {
		if n < 1 || n > 10000 {
			return fmt.Errorf("CLI authentication budgets must be 1..10000")
		}
	}
	return validateCLIPublicURL(c.Enabled, c.PublicURL)
}
func validateCLIPublicURL(enabled bool, publicURL string) error {
	if !enabled && publicURL == "" {
		return nil
	}
	u, err := url.Parse(publicURL)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || strings.IndexFunc(u.Path, func(r rune) bool { return r == 92 || r < 32 }) >= 0 {
		return fmt.Errorf("CLI public URL must be an absolute HTTP(S) URL without credentials, query or fragment")
	}
	if u.Scheme == "https" {
		return nil
	}
	host := u.Hostname()
	ip := net.ParseIP(host)
	if u.Scheme == "http" && (host == "localhost" || ip != nil && ip.IsLoopback()) {
		return nil
	}
	return fmt.Errorf("CLI public URL requires HTTPS except on loopback")
}
