package systemintegration

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"
	"unicode"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"github.com/opensoha/soha/internal/application/sourceanalysis"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

// AnalyzeSourceRepository is called only after the application service has
// authorized the stored repository and its explicit application association.
func (s *Service) AnalyzeSourceRepository(ctx context.Context, repository domainapp.SourceRepository, input sohaapi.RepositoryAnalysisInput) (sohaapi.RepositoryAnalysis, error) {
	if err := validateAnalysisInput(&input); err != nil {
		return sohaapi.RepositoryAnalysis{}, err
	}
	result := sourceanalysis.Identify(input.ProjectPath, nil)
	if repository.SourceConnectionID == "" || repository.ProviderRepositoryID == "" {
		return analysisFailure(result, sohaapi.AnalysisUnsupported, "connection_required", "Associate this repository with an authorized source connection first.", ""), nil
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	reader, err := s.sourceMetadataReader(ctx, repository.SourceConnectionID, repository.Provider)
	if err != nil {
		return analysisReadFailure(result, err, ""), nil
	}
	if reader == nil {
		return analysisFailure(result, sohaapi.AnalysisUnsupported, "provider_unsupported", "This source provider does not support bounded metadata analysis.", ""), nil
	}
	// Check identity again: changing a provider-side path must not redirect a
	// previously granted connection to a different repository.
	if err := validateSourceIdentity(ctx, reader, repository.ProviderRepositoryID, repository.URL); err != nil {
		return analysisReadFailure(result, err, ""), nil
	}
	return analyzeRepositoryMetadata(ctx, reader, repository.ProviderRepositoryID, input), nil
}

func (s *Service) ValidateSourceRepositoryBinding(ctx context.Context, input domainapp.SourceRepositoryInput) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	reader, err := s.sourceMetadataReader(ctx, input.SourceConnectionID, input.Provider)
	if err != nil {
		return err
	}
	if reader == nil {
		return fmt.Errorf("%w: provider does not support repository identity verification", apperrors.ErrInvalidArgument)
	}
	return validateSourceIdentity(ctx, reader, input.ProviderRepositoryID, input.URL)
}

func (s *Service) sourceMetadataReader(ctx context.Context, connectionID, provider string) (domainapp.SourceMetadataReader, error) {
	integration, adapter, err := s.sourceAdapter(ctx, connectionID, true)
	if err != nil {
		return nil, err
	}
	if integration.ProviderType != provider {
		return nil, fmt.Errorf("%w: repository and source provider do not match", apperrors.ErrAccessDenied)
	}
	reader, _ := adapter.(domainapp.SourceMetadataReader)
	return reader, nil
}

func validateSourceIdentity(ctx context.Context, reader domainapp.SourceMetadataReader, repositoryID, repositoryURL string) error {
	urls, err := reader.RepositoryCloneURLs(ctx, repositoryID)
	if err != nil {
		return err
	}
	for _, candidate := range urls {
		if candidate != "" && strings.TrimSuffix(strings.TrimRight(candidate, "/"), ".git") == strings.TrimSuffix(strings.TrimRight(repositoryURL, "/"), ".git") {
			return nil
		}
	}
	return fmt.Errorf("%w: repository URL does not match the selected provider repository", apperrors.ErrAccessDenied)
}

func validateAnalysisInput(input *sohaapi.RepositoryAnalysisInput) error {
	if input.ProjectPath == "" {
		input.ProjectPath = "."
	}
	if input.RepositoryID == "" || len(input.RepositoryID) > 200 || !input.RefType.Valid() || input.RefName == "" || len(input.RefName) > 512 || strings.ContainsFunc(input.RefName, unicode.IsControl) {
		return fmt.Errorf("%w: repository and a valid reference are required", apperrors.ErrInvalidArgument)
	}
	if len(input.ProjectPath) > 512 || path.IsAbs(input.ProjectPath) || strings.ContainsAny(input.ProjectPath, "\\:") || strings.ContainsFunc(input.ProjectPath, unicode.IsControl) {
		return fmt.Errorf("%w: project directory must be repository-relative", apperrors.ErrInvalidArgument)
	}
	for _, segment := range strings.Split(input.ProjectPath, "/") {
		if segment == ".." {
			return fmt.Errorf("%w: project directory cannot escape the repository", apperrors.ErrInvalidArgument)
		}
	}
	input.ProjectPath = path.Clean(input.ProjectPath)
	if input.RefType == sohaapi.AnalysisRefCommit && !validAnalysisCommit(input.RefName) {
		return fmt.Errorf("%w: commit must be a full hexadecimal object ID", apperrors.ErrInvalidArgument)
	}
	return nil
}

func validAnalysisCommit(commit string) bool {
	if len(commit) != 40 && len(commit) != 64 {
		return false
	}
	_, err := hex.DecodeString(commit)
	return err == nil
}

func analyzeRepositoryMetadata(ctx context.Context, reader domainapp.SourceMetadataReader, repositoryID string, input sohaapi.RepositoryAnalysisInput) sohaapi.RepositoryAnalysis {
	result := sourceanalysis.Identify(input.ProjectPath, nil)
	commit, err := reader.ResolveRepositoryRef(ctx, repositoryID, string(input.RefType), input.RefName)
	if err != nil {
		return analysisReadFailure(result, err, "")
	}
	if !validAnalysisCommit(commit) {
		return analysisFailure(result, sohaapi.AnalysisReadFailed, "invalid_commit", "The source provider did not resolve a full commit ID.", "")
	}
	if input.RefType == sohaapi.AnalysisRefCommit && !strings.EqualFold(commit, input.RefName) {
		return analysisFailure(result, sohaapi.AnalysisReadFailed, "commit_mismatch", "The source provider returned a different commit than requested.", "")
	}
	result.ResolvedCommit = strings.ToLower(commit)
	files := make(map[string]string)
	remaining := int64(1 << 20)
	// ponytail: sequential reads keep the total byte budget exact; parallelize
	// with a shared byte budget if measured provider latency needs it.
	for _, name := range sourceanalysis.CandidateFiles {
		filePath := path.Join(input.ProjectPath, name)
		if remaining <= 0 {
			return analysisFailure(result, sohaapi.AnalysisReadFailed, "total_size_limit", "Repository metadata exceeds the total analysis byte limit.", filePath)
		}
		data, err := reader.ReadRepositoryFile(ctx, repositoryID, result.ResolvedCommit, filePath, min(remaining, 256<<10))
		if errors.Is(err, apperrors.ErrNotFound) {
			continue
		}
		if err != nil {
			return analysisReadFailure(result, err, filePath)
		}
		if int64(len(data)) > min(remaining, 256<<10) {
			return analysisReadFailure(result, domainapp.ErrSourceFileTooLarge, filePath)
		}
		remaining -= int64(len(data))
		files[name] = string(data)
	}
	identified := sourceanalysis.Identify(input.ProjectPath, files)
	identified.ResolvedCommit = result.ResolvedCommit
	if len(files) == 0 {
		identified.Warnings = append(identified.Warnings, sohaapi.RepositoryAnalysisWarning{Code: "no_candidate_files", Message: "The selected commit and directory contain none of the supported metadata files.", Path: input.ProjectPath})
	}
	return identified
}

func analysisReadFailure(result sohaapi.RepositoryAnalysis, err error, filePath string) sohaapi.RepositoryAnalysis {
	status, code, message := sohaapi.AnalysisReadFailed, "provider_read_failed", "The source provider could not read repository metadata."
	switch {
	case errors.Is(err, apperrors.ErrAccessDenied):
		status, code, message = sohaapi.AnalysisForbidden, "source_access_denied", "Access to the configured source repository was denied."
	case errors.Is(err, apperrors.ErrNotFound):
		code, message = "source_not_found", "The configured repository or requested reference does not exist."
	case errors.Is(err, context.DeadlineExceeded):
		code, message = "analysis_timeout", "Repository analysis exceeded its time limit."
	case errors.Is(err, context.Canceled):
		code, message = "analysis_canceled", "Repository analysis was canceled."
	case errors.Is(err, domainapp.ErrSourceFileTooLarge):
		code, message = "file_size_limit", "Repository metadata exceeds the analysis file size limit."
	}
	return analysisFailure(result, status, code, message, filePath)
}

func analysisFailure(result sohaapi.RepositoryAnalysis, status sohaapi.RepositoryAnalysisStatus, code, message, filePath string) sohaapi.RepositoryAnalysis {
	result.Status = status
	result.Warnings = append(result.Warnings, sohaapi.RepositoryAnalysisWarning{Code: code, Message: message, Path: filePath})
	return result
}
