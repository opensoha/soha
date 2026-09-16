package manifestruntime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

var gitCommitPattern = regexp.MustCompile(`^(?:[a-fA-F0-9]{40}|[a-fA-F0-9]{64})$`)

// ResolveRepositoryCommit uses the same repository identity and temporary
// credential environment as Git synchronization, without checking out code.
// The caller authorizes the selected repository before resolving its ref.
func (g *Git) ResolveRepositoryCommit(ctx context.Context, repositoryID, refType, refName string) (string, error) {
	if g.repositories == nil {
		return "", fmt.Errorf("%w: repository reader is unavailable", apperrors.ErrInvalidArgument)
	}
	repository, err := g.repositories.GetRepository(ctx, repositoryID)
	if err != nil {
		return "", err
	}
	if repository.SourceConnectionID != "" {
		access, ok := g.credentials.(DeliveryGitAccessResolver)
		if !ok {
			return "", fmt.Errorf("%w: registered source access is unavailable", apperrors.ErrAccessDenied)
		}
		return NewDeliveryGit(access).ResolveDeliveryCommit(ctx, repository, refType, refName)
	}
	if err := validateGitRepositoryURL(repository.URL); err != nil {
		return "", err
	}
	ref, err := buildGitRef(refType, strings.TrimSpace(refName))
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, gitSyncTimeout)
	defer cancel()
	workspace, err := os.MkdirTemp("", "soha-git-ref-")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(workspace) }()
	environment, err := g.executionEnvironment(ctx, domainmanifest.TaskPayload{RepositoryID: repositoryID, RepositoryURL: repository.URL}, workspace)
	if err != nil {
		return "", err
	}
	if refType == "commit" {
		return verifyGitCommit(ctx, filepath.Join(workspace, "repository"), environment, repository.URL, ref)
	}
	if err := runGit(ctx, workspace, environment, "check-ref-format", ref); err != nil {
		return "", fmt.Errorf("%w: invalid Git ref", apperrors.ErrInvalidArgument)
	}
	output, err := gitOutput(ctx, workspace, environment, "ls-remote", "--exit-code", "--", repository.URL, ref, ref+"^{}")
	if err != nil {
		return "", err
	}
	commit := ""
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || !gitCommitPattern.MatchString(fields[0]) {
			continue
		}
		if fields[1] == ref+"^{}" {
			commit = strings.ToLower(fields[0])
			break
		}
		if fields[1] == ref {
			commit = strings.ToLower(fields[0])
		}
	}
	if commit == "" {
		return "", fmt.Errorf("%w: Git ref did not resolve to a commit", apperrors.ErrNotFound)
	}
	if refType == "tag" {
		// Git tags may point at blobs or trees; only a peeled commit can build.
		return verifyGitCommit(ctx, filepath.Join(workspace, "repository"), environment, repository.URL, commit)
	}
	return commit, nil
}

func buildGitRef(refType, refName string) (string, error) {
	if refName == "" {
		return "", fmt.Errorf("%w: Git ref is required", apperrors.ErrInvalidArgument)
	}
	switch refType {
	case "branch":
		return "refs/heads/" + refName, nil
	case "tag":
		return "refs/tags/" + refName, nil
	case "commit":
		if gitCommitPattern.MatchString(refName) {
			return strings.ToLower(refName), nil
		}
	}
	return "", fmt.Errorf("%w: select a branch, tag, or full Git commit", apperrors.ErrInvalidArgument)
}

func verifyGitCommit(ctx context.Context, directory string, environment []string, repositoryURL, commit string) (string, error) {
	if err := runGit(ctx, "", environment, "init", "--bare", directory); err != nil {
		return "", err
	}
	if err := runGit(ctx, directory, environment, "fetch", "--depth=1", "--", repositoryURL, commit); err != nil {
		return "", err
	}
	actual, err := gitOutput(ctx, directory, environment, "rev-parse", "FETCH_HEAD^{commit}")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(actual) != commit {
		return "", fmt.Errorf("%w: selected commit differs from repository result", apperrors.ErrConflict)
	}
	return commit, nil
}
