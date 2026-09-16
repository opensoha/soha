package catalog

import (
	"fmt"
	"net/url"
	"path"
	"strings"

	contractsapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaindocument "github.com/opensoha/soha/internal/domain/deliverydocument"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func validateTemplateSourceInput(id string, input domaindocument.SourceInput) (domaindocument.Source, error) {
	item := domaindocument.Source{ID: id, Name: strings.TrimSpace(input.Name), RepositoryID: strings.TrimSpace(input.RepositoryID), RefType: contractsapi.DeliveryTemplateSourceRefType(input.RefType), RefValue: strings.TrimSpace(input.RefValue), Path: strings.TrimSpace(input.Path), Kinds: input.Kinds, IncludePatterns: input.IncludePatterns, ExcludePatterns: input.ExcludePatterns, Enabled: input.Enabled}
	if item.Name == "" || len(item.Name) > 200 || item.RepositoryID == "" || len(item.RepositoryID) > 128 || id == "" && input.ExpectedGeneration != 0 || id != "" && input.ExpectedGeneration < 1 {
		return item, apperrors.ErrInvalidArgument
	}
	if item.RefType != "branch" && item.RefType != "tag" && item.RefType != "commit" || item.RefValue == "" || len(item.RefValue) > 256 || strings.HasPrefix(item.RefValue, "-") || strings.ContainsAny(item.RefValue, "\x00\r\n") {
		return item, apperrors.ErrInvalidArgument
	}
	if !validTemplateSourcePath(item.Path) || !validTemplateSourceKinds(item.Kinds) || !validTemplateSourcePatterns(item.IncludePatterns) || !validTemplateSourcePatterns(item.ExcludePatterns) {
		return item, apperrors.ErrInvalidArgument
	}
	return item, nil
}

func validTemplateSourceKinds(kinds []contractsapi.DeliveryDocumentKind) bool {
	if len(kinds) == 0 || len(kinds) > 4 {
		return false
	}
	seen := map[contractsapi.DeliveryDocumentKind]bool{}
	for _, kind := range kinds {
		if kind != "BuildTemplate" && kind != "DeploymentTemplate" && kind != "WorkflowTemplate" && kind != "Workflow" || seen[kind] {
			return false
		}
		seen[kind] = true
	}
	return true
}

func validTemplateSourcePatterns(patterns []string) bool {
	if len(patterns) > 32 {
		return false
	}
	for _, pattern := range patterns {
		if !validTemplateSourcePath(pattern) {
			return false
		}
	}
	return true
}

func validTemplateSourcePath(value string) bool {
	return value != "" && len(value) <= 512 && path.Clean(value) == value && !strings.HasPrefix(value, "/") && value != ".." && !strings.HasPrefix(value, "../") && !strings.ContainsAny(value, "\\\x00\r\n")
}

func validateTemplateRepository(repository domainapp.SourceRepository) error {
	if repository.SourceConnectionID == "" || repository.ProviderRepositoryID == "" || repository.CredentialRef != "" && repository.CredentialRef != repository.SourceConnectionID {
		return fmt.Errorf("%w: register the repository through a source connection before using it as a template source", apperrors.ErrInvalidArgument)
	}
	value := repository.URL
	if strings.HasPrefix(value, "git@") {
		parts := strings.SplitN(strings.TrimPrefix(value, "git@"), ":", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" || strings.ContainsAny(value, "\x00\r\n?#\\") {
			return apperrors.ErrInvalidArgument
		}
		return nil
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Hostname() == "" || parsed.Path == "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Scheme != "https" && parsed.Scheme != "ssh" {
		return fmt.Errorf("%w: template repositories require HTTPS or SSH without query or fragment", apperrors.ErrInvalidArgument)
	}
	if parsed.User != nil {
		_, password := parsed.User.Password()
		if parsed.Scheme != "ssh" || password {
			return fmt.Errorf("%w: repository URLs must not embed credentials", apperrors.ErrInvalidArgument)
		}
	}
	return nil
}
