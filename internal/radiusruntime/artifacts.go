package radiusruntime

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"strings"
	"time"
)

// ApprovedRootFile fences the installer-owned command configuration before any
// TLS credentials or runtime paths are consumed. Production hosting is Linux-only.
func ApprovedRootFile(path string) error {
	if !supportedHost() {
		return ErrDenied
	}
	return approvedRootFile(path, nativePaths())
}
func approvedRootFile(path string, paths pathPolicy) error {
	info, err := paths.regular(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > 32768 {
		return ErrDenied
	}
	uid, _, ok := paths.owner(info)
	if !ok || uid != 0 {
		return ErrDenied
	}
	return nil
}

func (s *Supervisor) certificatesUntil() (time.Time, error) {
	var until time.Time
	for _, path := range s.config.Certificates {
		info, err := s.host.paths().regular(path)
		if err != nil || info.Size() > 1<<20 {
			return time.Time{}, ErrDenied
		}
		data, err := os.ReadFile(path) // #nosec G304 -- regular above rejects symlinks and non-root-owned or writable ancestors before this read.
		if err != nil || len(data) > 1<<20 {
			return time.Time{}, ErrDenied
		}
		found := false
		for len(data) > 0 {
			block, remaining := pem.Decode(data)
			if block == nil {
				return time.Time{}, ErrDenied
			}
			data = remaining
			if block.Type != "CERTIFICATE" {
				return time.Time{}, ErrDenied
			}
			certificate, err := x509.ParseCertificate(block.Bytes)
			if err != nil || !certificate.NotAfter.After(time.Now()) || certificate.NotBefore.After(time.Now()) {
				return time.Time{}, ErrDenied
			}
			found = true
			if until.IsZero() || certificate.NotAfter.Before(until) {
				until = certificate.NotAfter
			}
		}
		if !found {
			return time.Time{}, ErrDenied
		}
	}
	if until.IsZero() {
		return time.Time{}, ErrDenied
	}
	return until, nil
}
func (s *Supervisor) verifyMaterialFiles(materials map[string]string) error {
	for path, expected := range materials {
		info, err := s.host.paths().regular(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 {
			return ErrDenied
		}
		uid, _, ok := s.host.ownership(info)
		if !ok || uid != 0 {
			return ErrDenied
		}
		actual, err := hashFile(path)
		if err != nil || actual != expected {
			return ErrDenied
		}
	}
	return nil
}
func (s *Supervisor) verifyMaterials() error {
	if err := s.verifyMaterialFiles(s.config.Materials); err != nil {
		return err
	}
	for key, path := range s.config.Environment {
		if strings.HasSuffix(key, "_FILE") {
			if _, err := s.host.paths().regular(path); err != nil {
				return err
			}
		}
	}
	_, err := s.certificatesUntil()
	return err
}
