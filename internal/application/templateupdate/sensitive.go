package templateupdate

import (
	"context"
	"errors"
	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	domainupdate "github.com/opensoha/soha/internal/domain/templateupdate"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

// Classify both published source and actual renderer output. Frozen comparisons
// retain this private flag so a later permission revocation also fences reads.
func (s *Service) authorizePreviewHistory(ctx context.Context, p domainidentity.Principal, b domainupdate.Binding, selection sohaapi.TemplateUpdateSelection, versions []version, to int64) (bool, error) {
	sensitive := false
	low, high := min(b.Target.FromVersion, to), max(b.Target.FromVersion, to)
	parameters := selectedParameters(b, selection)
	for _, v := range versions {
		if v.Number < low || v.Number > high || v.Deployment == nil {
			continue
		}
		if domainmanifest.SensitiveFiles(v.Deployment.Source.Files) {
			sensitive = true
		}
		if b.ManifestBinding.ID != "" {
			result, err := s.deps.Manifests.PreviewTemplateAdoption(ctx, p, b.ManifestBinding.ID, templateSelection(b, v, parameters))
			if errors.Is(err, apperrors.ErrAccessDenied) || errors.Is(err, apperrors.ErrUnauthorized) {
				return true, err
			}
			if err == nil && domainmanifest.SensitiveDocuments(result.Documents) {
				sensitive = true
			}
		}
	}
	if sensitive {
		return true, s.deps.Manifests.AuthorizeSensitiveTemplatePreview(ctx, p)
	}
	return false, nil
}
