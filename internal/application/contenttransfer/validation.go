package contenttransfer

import (
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/opensoha/soha/internal/application/contentdlp"
	d "github.com/opensoha/soha/internal/domain/contenttransfer"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

var secretReference = regexp.MustCompile(`^soha://secrets/[A-Za-z0-9._:-]+(?:/versions/[1-9][0-9]*)?$`)
var integrationID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

func validID(id string) bool {
	v, err := uuid.Parse(id)
	return err == nil && v != uuid.Nil && v.String() == id
}
func validName(name string) bool {
	if name == "" || len(name) > 200 || !utf8.ValidString(name) || strings.ContainsAny(name, "/\\") || name == "." || name == ".." {
		return false
	}
	for _, r := range name {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}
func ValidateConfig(c d.Config) error {
	if !validName(c.Name) || len(c.Name) > 120 || len(c.PrivateCIDRs) > 8 {
		return apperrors.ErrInvalidArgument
	}
	if err := contentdlp.ValidatePolicy(c.Policy.Scanner()); err != nil {
		return err
	}
	if c.Kind == "s3" {
		if !integrationID.MatchString(c.StorageIntegrationID) || c.BaseURL != "" || c.SecretRef != "" || len(c.PrivateCIDRs) > 0 {
			return apperrors.ErrInvalidArgument
		}
		return nil
	}
	return validateWebDAVConfig(c)
}
func validateWebDAVConfig(c d.Config) error {
	if c.Kind != "webdav" || c.StorageIntegrationID != "" || len(c.BaseURL) > 2048 || len(c.SecretRef) > 256 || c.SecretRef != "" && !secretReference.MatchString(c.SecretRef) {
		return apperrors.ErrInvalidArgument
	}
	if err := validateWebDAVURL(c.BaseURL); err != nil {
		return err
	}
	for _, cidr := range c.PrivateCIDRs {
		prefix, err := netip.ParsePrefix(cidr)
		if err != nil || prefix != prefix.Masked() || prefix.Bits() == 0 || !prefix.Addr().IsPrivate() {
			return apperrors.ErrInvalidArgument
		}
	}
	return nil
}
func validateWebDAVURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" || !strings.HasSuffix(u.Path, "/") || strings.Contains(u.Path, "\\") {
		return apperrors.ErrInvalidArgument
	}
	for _, part := range strings.Split(u.Path, "/") {
		if part == "." || part == ".." {
			return apperrors.ErrInvalidArgument
		}
	}
	return nil
}
