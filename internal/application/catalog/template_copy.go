package catalog

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaindocument "github.com/opensoha/soha/internal/domain/deliverydocument"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

// Capture derivation separately from immutable publication provenance. The new
// draft may be edited before creation and never inherits source management.
func (s *Service) templateCopyAudit(ctx context.Context, principal domainidentity.Principal, kind, targetID string, origin *domaincatalog.TemplateCopyOrigin) (map[string]any, error) {
	if origin == nil {
		return nil, nil
	}
	if targetID != "" || origin.ID == "" || len(origin.ID) > 200 || strings.ContainsFunc(origin.ID, unicode.IsControl) || origin.Revision < 1 || origin.Version < 0 {
		return nil, fmt.Errorf("%w: copiedFrom requires a template ID and positive revision on creation", apperrors.ErrInvalidArgument)
	}
	documents := DocumentService{catalog: s}
	_, revision, _, err := documents.readDocument(ctx, principal, kind, origin.ID, origin.Version)
	if err != nil {
		return nil, err
	}
	if revision != origin.Revision {
		return nil, fmt.Errorf("%w: copy source changed; reopen the source template", apperrors.ErrConflict)
	}
	var source domaindocument.SourceInfo
	if repo, ok := s.repo.(TemplateSourceRepository); ok {
		source, err = repo.GetDocumentSource(ctx, kind, origin.ID, origin.Version)
		if err != nil {
			return nil, err
		}
	}
	// Recheck after reading the source association so a concurrent Git import
	// cannot pair an old revision with a newer commit in the derivation audit.
	document, revision, _, err := documents.readDocument(ctx, principal, kind, origin.ID, origin.Version)
	if err != nil {
		return nil, err
	}
	if revision != origin.Revision {
		return nil, fmt.Errorf("%w: copy source changed; reopen the source template", apperrors.ErrConflict)
	}
	digest, err := document.NormalizedSpecDigest()
	if err != nil {
		return nil, err
	}
	parent := map[string]any{"kind": kind, "id": origin.ID, "revision": revision, "version": origin.Version, "name": document.Metadata.DisplayName, "normalizedSpecDigest": digest}
	if origin.Version > 0 {
		parent["provenance"] = source.Provenance
	} else {
		parent["association"] = source.Association
	}
	return map[string]any{"copiedFrom": parent}, nil
}
