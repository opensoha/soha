package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	guest "github.com/opensoha/soha-contracts/agent/guest"
)

// PersonalGuestConfig belongs to the isolated guest client, never the management Agent.
type PersonalGuestConfig struct {
	Endpoint        string `json:"endpoint"`
	BindingID       string `json:"bindingId"`
	EnrollmentID    string `json:"enrollmentId"`
	Challenge       string `json:"challenge"`
	CertificateFile string `json:"certificateFile"`
	KeyFile         string `json:"keyFile"`
	CAFile          string `json:"caFile"`
	StateFile       string `json:"stateFile"`
}

func LoadPersonalGuest(path string) (PersonalGuestConfig, error) {
	var cfg PersonalGuestConfig
	raw, err := ReadPrivateFile(path, 16384)
	if err != nil {
		return cfg, errors.New("personal guest configuration must be a bounded private file")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&cfg) != nil || decoder.Decode(new(any)) != io.EOF {
		return cfg, errors.New("invalid personal guest configuration")
	}
	if !validPersonalGuestEndpoint(cfg.Endpoint) {
		return cfg, errors.New("personal guest requires an explicit HTTPS origin")
	}
	if id, err := uuid.Parse(cfg.BindingID); err != nil || id.String() != cfg.BindingID {
		return cfg, errors.New("invalid personal guest identity")
	}
	if err := (guest.Enrollment{SchemaVersion: guest.EnrollmentVersion, EnrollmentID: cfg.EnrollmentID}).Validate(); err != nil {
		return cfg, err
	}
	if _, err := guest.EnrollmentTokenHash("Bearer " + cfg.Challenge); err != nil {
		return cfg, err
	}
	seen := map[string]bool{path: true}
	for _, name := range []string{cfg.CertificateFile, cfg.KeyFile, cfg.CAFile, cfg.StateFile} {
		if !filepath.IsAbs(name) || filepath.Clean(name) != name || filepath.Dir(name) != filepath.Dir(path) || seen[name] {
			return cfg, errors.New("personal guest files must stay in the same private directory")
		}
		seen[name] = true
	}
	return cfg, nil
}

func validPersonalGuestEndpoint(endpoint string) bool {
	u, err := url.Parse(endpoint)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && u.Path == "" && len(endpoint) <= 512 && strings.TrimSpace(endpoint) == endpoint
}
