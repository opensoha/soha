package appconfig

import (
	"testing"
	"time"
)

func TestCLIAuthPolicyDefaultsAndValidation(t *testing.T) {
	c := (CLIAuth{}).WithDefaults()
	if c.Enabled || c.AccessTTL != 15*time.Minute || c.IdleTTL != 7*24*time.Hour || c.AbsoluteTTL != 30*24*time.Hour || c.DeviceTTL != 5*time.Minute {
		t.Fatal("CLI defaults changed")
	}
	for _, tc := range []struct {
		name, url string
		valid     bool
	}{
		{"disabled", "", true}, {"https base path", "https://example.test/soha", true}, {"loopback", "http://127.0.0.1:5197", true}, {"ipv6", "http://[::1]:5197", true}, {"plain remote", "http://example.test", false}, {"userinfo", "https://user:secret@example.test", false}, {"query", "https://example.test/?x=y", false}, {"fragment", "https://example.test/#x", false}, {"relative", "/soha", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := CLIAuth{Enabled: tc.url != "", PublicURL: tc.url}
			if (c.Validate() == nil) != tc.valid {
				t.Fatal("unexpected CLI URL validation result")
			}
		})
	}
	for _, mutate := range []func(*CLIAuth){
		func(c *CLIAuth) { c.AccessTTL = -time.Second }, func(c *CLIAuth) { c.IdleTTL = time.Second }, func(c *CLIAuth) { c.AbsoluteTTL = time.Hour }, func(c *CLIAuth) { c.AbsoluteTTL = 366 * 24 * time.Hour }, func(c *CLIAuth) { c.CreateLimit = -1 }, func(c *CLIAuth) { c.DeviceTTL = 24 * time.Hour },
	} {
		c := CLIAuth{}
		mutate(&c)
		if c.Validate() == nil {
			t.Fatal("unsafe CLI authentication policy accepted")
		}
	}
	if (CLIAuth{Enabled: true}).Validate() == nil {
		t.Fatal("enabled device login requires a configured public URL")
	}
}
