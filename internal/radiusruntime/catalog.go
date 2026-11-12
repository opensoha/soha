package radiusruntime

import (
	"net/url"
	"strings"

	api "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

// ConfigurationSnapshot is local installer approval, never downloaded control
// data. Material equality includes every generated contract field.
type ConfigurationSnapshot struct {
	Material     api.NetworkRADIUSConfiguration `json:"material"`
	Directory    string                         `json:"directory"`
	TreeSHA256   string                         `json:"treeSHA256"`
	Hooks        HookAttestation                `json:"hooks"`
	Environment  map[string]string              `json:"environment"`
	Materials    map[string]string              `json:"materials"`
	Certificates []string                       `json:"certificates"`
}

func snapshotInstallation(base Installation, entry ConfigurationSnapshot) (Installation, error) {
	c := base
	c.ConfigurationDir, c.ConfigurationSHA256 = entry.Directory, entry.TreeSHA256
	c.Hooks, c.Environment, c.Certificates = entry.Hooks, entry.Environment, entry.Certificates
	c.Materials = make(map[string]string, len(base.Materials)+len(entry.Materials))
	for path, hash := range base.Materials {
		c.Materials[path] = hash
	}
	for path, hash := range entry.Materials {
		if common, ok := c.Materials[path]; ok && common != hash {
			return Installation{}, ErrDenied
		}
		c.Materials[path] = hash
	}
	return c, nil
}

func hasLegacySnapshot(c Installation) bool {
	h := c.Hooks
	return c.ConfigurationDir != "" || c.ConfigurationSHA256 != "" || len(c.Environment) != 0 || len(c.Certificates) != 0 || h.ConfigurationSHA256 != "" || h.Socket != "" || len(h.Routes) != 0 || h.NonceAttribute != "" || h.ControlOnly || h.EpochEnvironment != "" || h.SocketEnvironment != ""
}

func validateCatalogLayout(c Installation) error {
	if hasLegacySnapshot(c) || len(c.ConfigurationCatalog) > 64 {
		return ErrInvalid
	}
	seen := make(map[string]bool)
	for _, entry := range c.ConfigurationCatalog {
		m := entry.Material
		if m.Reference == "" || seen[m.Reference] || !digestPattern.MatchString(entry.TreeSHA256) || m.TreeHash != "sha256:"+entry.TreeSHA256 || !m.HooksAttested {
			return ErrDenied
		}
		for _, ref := range []string{m.EapCARef, m.EapServerCertificateRef, m.EapServerKeyRef, m.NasSharedSecretRef} {
			if ref == "" || strings.ContainsRune(ref, 0) {
				return ErrInvalid
			}
		}
		seen[m.Reference] = true
		selected, err := snapshotInstallation(c, entry)
		if err != nil {
			return err
		}
		if err = validateHooks(selected); err != nil {
			return err
		}
	}
	return nil
}

func validateSnapshot(c Installation, paths pathPolicy) error {
	if !digestPattern.MatchString(c.ConfigurationSHA256) {
		return ErrInvalid
	}
	if err := paths.directory(c.ConfigurationDir); err != nil {
		return err
	}
	if c.StateDir == c.ConfigurationDir {
		return ErrDenied
	}
	if err := validateReferences(c, paths); err != nil {
		return err
	}
	if err := validateHooks(c); err != nil {
		return err
	}
	return validateEnvironment(c.Environment)
}

// ApproveControl derives private client allowlists solely from the root-owned
// manifest. Common TLS hashes are checked before NewControlClient reads them.
func (c Installation) ApproveControl(control ClientConfiguration) (ClientConfiguration, error) {
	for _, path := range []string{control.CertificateFile, control.KeyFile, control.CAFile} {
		if _, ok := c.Materials[path]; !ok {
			return ClientConfiguration{}, ErrDenied
		}
	}
	control.ApprovedMaterials = c.Materials
	if len(c.ConfigurationCatalog) == 0 {
		if control.Configuration.TreeHash != "sha256:"+c.ConfigurationSHA256 {
			return ClientConfiguration{}, ErrDenied
		}
		control.AllowedConfigurations = []api.NetworkRADIUSConfiguration{control.Configuration}
		return control, nil
	}
	if control.Configuration != (api.NetworkRADIUSConfiguration{}) {
		return ClientConfiguration{}, ErrInvalid
	}
	if err := validateCatalogLayout(c); err != nil {
		return ClientConfiguration{}, err
	}
	control.AllowedConfigurations = nil
	for _, entry := range c.ConfigurationCatalog {
		if err := validateCatalogControlEnvironment(entry.Environment, control); err != nil {
			return ClientConfiguration{}, err
		}
		control.AllowedConfigurations = append(control.AllowedConfigurations, entry.Material)
	}
	return control, nil
}

func validateCatalogControlEnvironment(env map[string]string, control ClientConfiguration) error {
	for key, path := range map[string]string{"SOHA_RADIUS_CONTROL_CERT_FILE": control.CertificateFile, "SOHA_RADIUS_CONTROL_KEY_FILE": control.KeyFile, "SOHA_RADIUS_CONTROL_CA_FILE": control.CAFile, "SOHA_RADIUS_RUNTIME_ID": control.RuntimeID} {
		if env[key] != path {
			return ErrDenied
		}
	}
	u, err := url.Parse(env["SOHA_RADIUS_CONTROL_URL"])
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" && u.Path != "/" {
		return ErrInvalid
	}
	return nil
}

func (c *ControlClient) approvedConfiguration(material api.NetworkRADIUSConfiguration) bool {
	if len(c.config.AllowedConfigurations) == 0 {
		return material == c.config.Configuration
	}
	for _, allowed := range c.config.AllowedConfigurations {
		if allowed == material {
			return true
		}
	}
	return false
}

func (s *Supervisor) selectConfiguration(lease Lease) error {
	if len(s.config.ConfigurationCatalog) == 0 {
		return s.matchConfiguration(lease)
	}
	for _, entry := range s.config.ConfigurationCatalog {
		if entry.Material != lease.Configuration {
			continue
		}
		selected, err := snapshotInstallation(s.config, entry)
		if err != nil {
			return err
		}
		if selected.Environment["SOHA_RADIUS_NAS_ID"] != lease.NASID {
			return ErrDenied
		}
		s.config, s.selectedMaterial = selected, &entry
		return s.matchConfiguration(lease)
	}
	return ErrDenied
}

func (s *Supervisor) matchConfiguration(lease Lease) error {
	if lease.ConfigurationSHA256 != s.config.ConfigurationSHA256 {
		return ErrDenied
	}
	if s.selectedMaterial != nil && lease.Configuration != s.selectedMaterial.Material {
		return ErrDenied
	}
	return nil
}
