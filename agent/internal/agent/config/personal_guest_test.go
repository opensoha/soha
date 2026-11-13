package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestPersonalGuestConfigurationBoundary(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil { //nolint:gosec // Private directory needs owner traversal; files remain 0600.
		t.Fatal(err)
	}
	path := filepath.Join(dir, "guest.json")
	base := PersonalGuestConfig{Endpoint: "https://core.example:8444", BindingID: uuid.NewString(), EnrollmentID: uuid.NewString(), Challenge: strings.Repeat("A", 43),
		CertificateFile: filepath.Join(dir, "guest.pem"), KeyFile: filepath.Join(dir, "guest.key"), CAFile: filepath.Join(dir, "ca.pem"), StateFile: filepath.Join(dir, "state.json")}
	for _, test := range []struct {
		name   string
		change func(*PersonalGuestConfig)
		mode   os.FileMode
		valid  bool
	}{
		{"valid_private_https", func(*PersonalGuestConfig) {}, 0600, true},
		{"plain_http", func(c *PersonalGuestConfig) { c.Endpoint = "http://core.example" }, 0600, false},
		{"url_credentials", func(c *PersonalGuestConfig) { c.Endpoint = "https://user:secret@core.example" }, 0600, false},
		{"redirect_path", func(c *PersonalGuestConfig) { c.Endpoint += "/path" }, 0600, false},
		{"foreign_file_directory", func(c *PersonalGuestConfig) { c.KeyFile = "/different/key.pem" }, 0600, false},
		{"file_collision", func(c *PersonalGuestConfig) { c.StateFile = c.KeyFile }, 0600, false},
		{"invalid_challenge", func(c *PersonalGuestConfig) { c.Challenge = "invalid" }, 0600, false},
		{"invalid_identity", func(c *PersonalGuestConfig) { c.BindingID = "guest" }, 0600, false},
		{"world_readable_bootstrap", func(*PersonalGuestConfig) {}, 0644, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := base
			test.change(&cfg)
			raw, err := json.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, test.mode); err != nil {
				t.Fatal(err)
			}
			_, err = LoadPersonalGuest(path)
			if (err == nil) != test.valid {
				t.Fatalf("valid=%v: %v", test.valid, err)
			}
		})
	}
}
