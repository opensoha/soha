package manifestruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	domainapp "github.com/opensoha/soha/internal/domain/application"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
)

const (
	gitSyncTimeout = 90 * time.Second
	gitMaxFiles    = 100
	gitMaxBytes    = 2 << 20
)

type GitRepositoryReader interface {
	GetRepository(context.Context, string) (domainapp.SourceRepository, error)
}

type GitCredentialResolver interface {
	ResolveSourceCredentials(context.Context, string) (map[string]string, error)
}

type Git struct {
	repositories GitRepositoryReader
	credentials  GitCredentialResolver
}

func NewGit(repositories GitRepositoryReader, credentials GitCredentialResolver) *Git {
	return &Git{repositories: repositories, credentials: credentials}
}

func (g *Git) Execute(ctx context.Context, payload domainmanifest.TaskPayload) (domainmanifest.TaskResult, error) {
	if payload.Action != domainmanifest.TaskActionSync {
		return domainmanifest.TaskResult{}, fmt.Errorf("unsupported Git manifest action %q", payload.Action)
	}
	if strings.TrimSpace(payload.RepositoryURL) == "" {
		return domainmanifest.TaskResult{}, fmt.Errorf("manifest repository URL is unavailable")
	}
	if err := validateGitRepositoryURL(payload.RepositoryURL); err != nil {
		return domainmanifest.TaskResult{}, err
	}
	syncCtx, cancel := context.WithTimeout(ctx, gitSyncTimeout)
	defer cancel()
	workspace, err := os.MkdirTemp("", "soha-manifest-sync-")
	if err != nil {
		return domainmanifest.TaskResult{}, fmt.Errorf("create manifest sync workspace: %w", err)
	}
	defer func() { _ = os.RemoveAll(workspace) }()
	gitEnvironment, err := g.executionEnvironment(syncCtx, payload, workspace)
	if err != nil {
		return domainmanifest.TaskResult{}, err
	}

	checkout := filepath.Join(workspace, "repository")
	resolvedCommit, treeDigest, err := checkoutManifestRepository(syncCtx, payload, checkout, gitEnvironment)
	if err != nil {
		return domainmanifest.TaskResult{}, err
	}
	files, _, err := readManifestRepositoryFiles(checkout, payload.Path, payload.IncludePatterns, payload.ExcludePatterns)
	if err != nil {
		return domainmanifest.TaskResult{}, err
	}
	canonicalDigest, err := validateSyncedManifestFiles(files, payload.Renderer)
	if err != nil {
		return domainmanifest.TaskResult{}, err
	}
	return domainmanifest.TaskResult{
		Action: domainmanifest.TaskActionSync, Generation: payload.Generation, Stale: false,
		Diagnostics: []domainmanifest.Diagnostic{}, Inventory: []domainmanifest.ResourceInventory{},
		ResolvedCommit: resolvedCommit, TreeDigest: treeDigest, CanonicalDigest: canonicalDigest,
		SyncedFiles: files, EvidenceRefs: []string{"git:" + resolvedCommit},
	}, nil
}

func (g *Git) executionEnvironment(ctx context.Context, payload domainmanifest.TaskPayload, workspace string) ([]string, error) {
	if g.repositories == nil || strings.TrimSpace(payload.RepositoryID) == "" {
		return nil, nil
	}
	repository, err := g.repositories.GetRepository(ctx, strings.TrimSpace(payload.RepositoryID))
	if err != nil {
		return nil, fmt.Errorf("resolve manifest repository: %w", err)
	}
	if strings.TrimSpace(repository.URL) != strings.TrimSpace(payload.RepositoryURL) {
		return nil, fmt.Errorf("manifest repository configuration changed after the task was queued")
	}
	credentialRef := strings.TrimSpace(repository.CredentialRef)
	if credentialRef == "" {
		return nil, nil
	}
	if g.credentials == nil {
		return nil, fmt.Errorf("manifest repository credential resolver is unavailable")
	}
	credentials, err := g.credentials.ResolveSourceCredentials(ctx, credentialRef)
	if err != nil {
		return nil, fmt.Errorf("resolve manifest repository credentials: %w", err)
	}
	return prepareGitCredentialEnvironment(repository, credentials, workspace)
}

func prepareGitCredentialEnvironment(repository domainapp.SourceRepository, credentials map[string]string, workspace string) ([]string, error) {
	repositoryURL := strings.TrimSpace(repository.URL)
	lowerURL := strings.ToLower(repositoryURL)
	switch {
	case strings.HasPrefix(lowerURL, "https://"):
		username := firstGitCredential(credentials, "username", "user")
		if username == "" {
			switch strings.ToLower(strings.TrimSpace(repository.Provider)) {
			case "gitlab":
				username = "oauth2"
			case "github":
				username = "x-access-token"
			default:
				username = "git"
			}
		}
		password := firstGitCredential(credentials, "password", "token", "access_token")
		if password == "" {
			return nil, fmt.Errorf("manifest HTTPS repository credential has no password or token")
		}
		askpass := filepath.Join(workspace, "git-askpass.sh")
		const script = "#!/bin/sh\ncase \"$1\" in\n  *Username*) printf '%s\\n' \"$SOHA_GIT_USERNAME\" ;;\n  *) printf '%s\\n' \"$SOHA_GIT_PASSWORD\" ;;\nesac\n"
		if err := os.WriteFile(askpass, []byte(script), 0o700); err != nil {
			return nil, fmt.Errorf("create Git credential helper: %w", err)
		}
		return []string{"GIT_ASKPASS=" + askpass, "SOHA_GIT_USERNAME=" + username, "SOHA_GIT_PASSWORD=" + password}, nil
	case strings.HasPrefix(lowerURL, "http://"):
		return nil, fmt.Errorf("credentialed manifest repositories must use HTTPS or SSH")
	case strings.HasPrefix(lowerURL, "ssh://"), strings.HasPrefix(lowerURL, "git@"):
		privateKey := firstGitCredential(credentials, "private_key", "ssh_private_key")
		if privateKey == "" {
			return nil, fmt.Errorf("manifest SSH repository credential has no private key")
		}
		keyPath := filepath.Join(workspace, "git-identity")
		if err := os.WriteFile(keyPath, []byte(privateKey), 0o600); err != nil {
			return nil, fmt.Errorf("create Git SSH identity: %w", err)
		}
		sshCommand := "ssh -o BatchMode=yes -o IdentitiesOnly=yes -i " + strconv.Quote(keyPath)
		if knownHosts := firstGitCredential(credentials, "known_hosts"); knownHosts != "" {
			knownHostsPath := filepath.Join(workspace, "known_hosts")
			if err := os.WriteFile(knownHostsPath, []byte(knownHosts), 0o600); err != nil {
				return nil, fmt.Errorf("create Git SSH known_hosts: %w", err)
			}
			sshCommand += " -o StrictHostKeyChecking=yes -o UserKnownHostsFile=" + strconv.Quote(knownHostsPath)
		}
		return []string{"GIT_SSH_COMMAND=" + sshCommand, "GIT_SSH_VARIANT=ssh"}, nil
	default:
		return nil, fmt.Errorf("manifest repository URL must use HTTP(S) or SSH")
	}
}

func firstGitCredential(credentials map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(credentials[key]); value != "" {
			return value
		}
	}
	return ""
}

func validateSyncedManifestFiles(files []domainmanifest.File, renderer string) (string, error) {
	prepared, err := prepareFiles(files, nil)
	if err != nil {
		return "", err
	}
	inputs := prepared
	switch renderer {
	case domainmanifest.RendererRaw:
	case domainmanifest.RendererKustomize:
		inputs, err = renderKustomize(prepared)
	default:
		return "", fmt.Errorf("unsupported manifest renderer %q", renderer)
	}
	if err != nil {
		return "", err
	}
	documents, _, err := decodeDocuments(inputs, "default")
	if err != nil {
		return "", err
	}
	if len(documents) == 0 {
		return "", fmt.Errorf("manifest source did not render any Kubernetes resources")
	}
	return digestDocuments(documents), nil
}

func checkoutManifestRepository(ctx context.Context, payload domainmanifest.TaskPayload, directory string, environment []string) (string, string, error) {
	requested := strings.TrimSpace(payload.RequestedCommit)
	if requested == "" && payload.RefType == domainmanifest.SourceRefCommit {
		requested = strings.TrimSpace(payload.RefValue)
	}
	if requested != "" {
		if err := runGit(ctx, "", environment, "init", directory); err != nil {
			return "", "", err
		}
		if err := runGit(ctx, directory, environment, "remote", "add", "origin", payload.RepositoryURL); err != nil {
			return "", "", err
		}
		if err := runGit(ctx, directory, environment, "fetch", "--depth", "1", "origin", requested); err != nil {
			return "", "", err
		}
		if err := runGit(ctx, directory, environment, "checkout", "--detach", "FETCH_HEAD"); err != nil {
			return "", "", err
		}
	} else {
		ref := strings.TrimSpace(payload.RefValue)
		if ref == "" {
			return "", "", fmt.Errorf("manifest Git ref is required")
		}
		if err := runGit(ctx, "", environment, "clone", "--no-checkout", "--filter=blob:none", "--depth", "1", "--branch", ref, "--", payload.RepositoryURL, directory); err != nil {
			return "", "", err
		}
		if err := runGit(ctx, directory, environment, "checkout", "--detach"); err != nil {
			return "", "", err
		}
	}
	commit, err := gitOutput(ctx, directory, environment, "rev-parse", "HEAD")
	if err != nil {
		return "", "", err
	}
	tree, err := gitOutput(ctx, directory, environment, "rev-parse", "HEAD^{tree}")
	if err != nil {
		return "", "", err
	}
	return strings.TrimSpace(commit), strings.TrimSpace(tree), nil
}

func readManifestRepositoryFiles(checkout, sourcePath string, includes, excludes []string) ([]domainmanifest.File, string, error) {
	root := filepath.Join(checkout, filepath.FromSlash(strings.TrimSpace(sourcePath)))
	resolvedRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, "", err
	}
	resolvedCheckout, err := filepath.Abs(checkout)
	if err != nil {
		return nil, "", err
	}
	if resolvedRoot != resolvedCheckout && !strings.HasPrefix(resolvedRoot, resolvedCheckout+string(filepath.Separator)) {
		return nil, "", fmt.Errorf("manifest source path escapes repository root")
	}
	info, err := os.Stat(resolvedRoot)
	if err != nil || !info.IsDir() {
		return nil, "", fmt.Errorf("manifest source path is unavailable")
	}
	if len(includes) == 0 {
		includes = []string{"**/*.yaml", "**/*.yml", "*.yaml", "*.yml"}
	}
	reader := manifestRepositoryFileReader{root: resolvedRoot, includes: includes, excludes: excludes, files: make([]domainmanifest.File, 0)}
	err = filepath.WalkDir(resolvedRoot, reader.visit)
	if err != nil {
		return nil, "", err
	}
	if len(reader.files) == 0 {
		return nil, "", fmt.Errorf("manifest source did not select any YAML files")
	}
	sort.Slice(reader.files, func(i, j int) bool { return reader.files[i].Path < reader.files[j].Path })
	hash := sha256.New()
	for _, file := range reader.files {
		_, _ = hash.Write([]byte(file.Path))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(file.Content))
		_, _ = hash.Write([]byte{0})
	}
	return reader.files, "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

type manifestRepositoryFileReader struct {
	root       string
	includes   []string
	excludes   []string
	files      []domainmanifest.File
	totalBytes int
}

func (r *manifestRepositoryFileReader) visit(filePath string, entry fs.DirEntry, walkErr error) error {
	if walkErr != nil {
		return walkErr
	}
	if entry.Type()&os.ModeSymlink != 0 {
		return fmt.Errorf("manifest source contains a symbolic link")
	}
	if entry.IsDir() {
		return nil
	}
	relative, err := filepath.Rel(r.root, filePath)
	if err != nil {
		return err
	}
	relative = filepath.ToSlash(relative)
	if !manifestGlobSetMatches(relative, r.includes) || manifestGlobSetMatches(relative, r.excludes) {
		return nil
	}
	extension := strings.ToLower(filepath.Ext(relative))
	if extension != ".yaml" && extension != ".yml" {
		return fmt.Errorf("manifest include pattern selected non-YAML file %q", relative)
	}
	content, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}
	r.totalBytes += len(content)
	if len(r.files)+1 > gitMaxFiles || r.totalBytes > gitMaxBytes {
		return fmt.Errorf("manifest source exceeds the bounded file or byte limit")
	}
	r.files = append(r.files, domainmanifest.File{Path: relative, Content: string(content)})
	return nil
}

func manifestGlobSetMatches(path string, patterns []string) bool {
	for _, pattern := range patterns {
		matched, err := manifestGlobMatch(strings.TrimSpace(pattern), path)
		if err == nil && matched {
			return true
		}
	}
	return false
}

func manifestGlobMatch(pattern, value string) (bool, error) {
	if pattern == "" {
		return false, nil
	}
	var expression strings.Builder
	expression.WriteString("^")
	for index := 0; index < len(pattern); index++ {
		switch pattern[index] {
		case '*':
			if index+1 < len(pattern) && pattern[index+1] == '*' {
				expression.WriteString(".*")
				index++
			} else {
				expression.WriteString("[^/]*")
			}
		case '?':
			expression.WriteString("[^/]")
		default:
			expression.WriteString(regexp.QuoteMeta(string(pattern[index])))
		}
	}
	expression.WriteString("$")
	return regexp.MatchString(expression.String(), value)
}

func runGit(ctx context.Context, directory string, environment []string, args ...string) error {
	_, err := gitOutput(ctx, directory, environment, args...)
	return err
}

func gitOutput(ctx context.Context, directory string, environment []string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = directory
	command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0")
	command.Env = append(command.Env, environment...)
	output, err := command.CombinedOutput()
	if err != nil {
		message := sanitizeGitOutput(strings.TrimSpace(string(output)), environment)
		if len(message) > 500 {
			message = message[:500]
		}
		return "", fmt.Errorf("Git command failed: %s", message)
	}
	return string(output), nil
}

func sanitizeGitOutput(message string, environment []string) string {
	for _, entry := range environment {
		if strings.HasPrefix(entry, "SOHA_GIT_PASSWORD=") {
			secret := strings.TrimPrefix(entry, "SOHA_GIT_PASSWORD=")
			if secret != "" {
				message = strings.ReplaceAll(message, secret, "[REDACTED]")
			}
		}
	}
	return message
}

func validateGitRepositoryURL(value string) error {
	trimmed := strings.TrimSpace(value)
	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(lower, "git@") {
		return nil
	}
	if strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "ssh://") {
		parsed, err := url.Parse(trimmed)
		if err != nil || parsed.Host == "" {
			return fmt.Errorf("manifest repository URL is invalid")
		}
		if parsed.User != nil {
			return fmt.Errorf("manifest repository URL must not embed credentials")
		}
		return nil
	}
	return fmt.Errorf("manifest repository URL must use HTTP(S) or SSH")
}
