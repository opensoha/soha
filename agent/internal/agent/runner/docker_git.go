package runner

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type gitDockerfileBuildSpec struct {
	RepositoryURL  string
	Ref            string
	DockerfilePath string
	ContextDir     string
	Image          string
	Platform       string
	Pull           bool
	NoCache        bool
}

var gitCommitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
var dockerBuildOperationIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

func (r *Runner) executeGitDockerfileBuild(ctx context.Context, composeDir string, operation DockerOperation) ([]string, string, error) {
	spec, err := gitDockerfileBuildSpecFromOperation(operation)
	if err != nil {
		return nil, "", err
	}
	if !dockerBuildOperationIDPattern.MatchString(strings.TrimSpace(operation.ID)) {
		return nil, "", fmt.Errorf("docker operation id is invalid for git build workspace")
	}
	buildRoot, err := resolveWorkspacePath(filepath.Join(composeDir, ".soha-build"), operation.ID)
	if err != nil {
		return nil, "", fmt.Errorf("resolve git build workspace: %w", err)
	}
	repositoryDir := filepath.Join(buildRoot, "repository")
	if err := os.MkdirAll(repositoryDir, 0o755); err != nil {
		return nil, buildRoot, fmt.Errorf("create git build workspace: %w", err)
	}
	logs := []string{"git build workspace prepared"}
	if _, err := os.Stat(filepath.Join(repositoryDir, ".git")); err != nil {
		if !os.IsNotExist(err) {
			return logs, buildRoot, fmt.Errorf("inspect git build workspace: %w", err)
		}
		if err := os.RemoveAll(repositoryDir); err != nil {
			return logs, buildRoot, fmt.Errorf("reset git build workspace: %w", err)
		}
		if err := os.MkdirAll(repositoryDir, 0o755); err != nil {
			return logs, buildRoot, fmt.Errorf("recreate git build workspace: %w", err)
		}
		commandLogs, commandErr := runCommand(ctx, repositoryDir, "git", "init", "--quiet")
		logs = append(logs, commandLogs...)
		if commandErr != nil {
			return logs, buildRoot, fmt.Errorf("initialize git repository: %w", commandErr)
		}
	}
	if _, err := runCommand(ctx, repositoryDir, "git", "remote", "get-url", "origin"); err == nil {
		commandLogs, commandErr := runCommand(ctx, repositoryDir, "git", "remote", "set-url", "origin", spec.RepositoryURL)
		logs = append(logs, maskRepositoryURLLog(commandLogs)...)
		if commandErr != nil {
			return logs, buildRoot, fmt.Errorf("update git origin: %w", commandErr)
		}
	} else {
		commandLogs, commandErr := runCommand(ctx, repositoryDir, "git", "remote", "add", "origin", spec.RepositoryURL)
		logs = append(logs, maskRepositoryURLLog(commandLogs)...)
		if commandErr != nil {
			return logs, buildRoot, fmt.Errorf("configure git origin: %w", commandErr)
		}
	}
	resolvedCommitPath := filepath.Join(buildRoot, "resolved-commit")
	fetchRef := spec.Ref
	if raw, readErr := os.ReadFile(resolvedCommitPath); readErr == nil {
		if commit := strings.TrimSpace(string(raw)); gitCommitPattern.MatchString(commit) {
			fetchRef = commit
		}
	}
	commandLogs, err := runCommand(ctx, repositoryDir, "git", "fetch", "--quiet", "--depth=1", "origin", fetchRef)
	logs = append(logs, commandLogs...)
	if err != nil {
		return logs, buildRoot, fmt.Errorf("fetch git ref %q: %w", fetchRef, err)
	}
	commandLogs, err = runCommand(ctx, repositoryDir, "git", "checkout", "--quiet", "--detach", "FETCH_HEAD")
	logs = append(logs, commandLogs...)
	if err != nil {
		return logs, buildRoot, fmt.Errorf("checkout git ref %q: %w", fetchRef, err)
	}
	commandLogs, err = runCommand(ctx, repositoryDir, "git", "clean", "-fdx")
	logs = append(logs, commandLogs...)
	if err != nil {
		return logs, buildRoot, fmt.Errorf("clean git checkout: %w", err)
	}
	commitLogs, err := runCommand(ctx, repositoryDir, "git", "rev-parse", "HEAD")
	if err != nil || len(commitLogs) < 2 || !gitCommitPattern.MatchString(strings.TrimSpace(commitLogs[len(commitLogs)-1])) {
		return logs, buildRoot, fmt.Errorf("resolve checked out git commit")
	}
	commit := strings.TrimSpace(commitLogs[len(commitLogs)-1])
	if err := os.WriteFile(resolvedCommitPath, []byte(commit+"\n"), 0o600); err != nil {
		return logs, buildRoot, fmt.Errorf("persist resolved git commit: %w", err)
	}
	logs = append(logs, "git source resolved at commit "+commit)

	dockerfilePath, err := resolveRepositoryBuildPath(repositoryDir, spec.DockerfilePath, false)
	if err != nil {
		return logs, buildRoot, fmt.Errorf("resolve Dockerfile: %w", err)
	}
	contextDir, err := resolveRepositoryBuildPath(repositoryDir, spec.ContextDir, true)
	if err != nil {
		return logs, buildRoot, fmt.Errorf("resolve build context: %w", err)
	}
	args := []string{"build", "--file", dockerfilePath, "--tag", spec.Image}
	if spec.Platform != "" {
		args = append(args, "--platform", spec.Platform)
	}
	if spec.Pull {
		args = append(args, "--pull")
	}
	if spec.NoCache {
		args = append(args, "--no-cache")
	}
	args = append(args, contextDir)
	commandLogs, err = runCommand(ctx, repositoryDir, "docker", args...)
	logs = append(logs, commandLogs...)
	if err != nil {
		return logs, buildRoot, fmt.Errorf("build Docker image %q: %w", spec.Image, err)
	}
	logs = append(logs, "Docker image built from Git source: "+spec.Image)
	return logs, buildRoot, nil
}

func cleanupGitBuildWorkspace(buildRoot string, preserveResolvedCommit bool) error {
	if strings.TrimSpace(buildRoot) == "" {
		return nil
	}
	if !preserveResolvedCommit {
		return os.RemoveAll(buildRoot)
	}
	resolvedCommitPath := filepath.Join(buildRoot, "resolved-commit")
	raw, err := os.ReadFile(resolvedCommitPath)
	if err != nil || !gitCommitPattern.MatchString(strings.TrimSpace(string(raw))) {
		return os.RemoveAll(buildRoot)
	}
	commit := strings.TrimSpace(string(raw))
	if err := os.RemoveAll(buildRoot); err != nil {
		return err
	}
	if err := os.MkdirAll(buildRoot, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(buildRoot, "resolved-commit"), []byte(commit+"\n"), 0o600)
}

func gitDockerfileBuildSpecFromOperation(operation DockerOperation) (gitDockerfileBuildSpec, error) {
	raw, ok := operation.Payload["gitBuild"].(map[string]any)
	if !ok {
		return gitDockerfileBuildSpec{}, fmt.Errorf("gitBuild payload is required for git_dockerfile source")
	}
	spec := gitDockerfileBuildSpec{
		RepositoryURL:  strings.TrimSpace(fmt.Sprint(raw["repositoryUrl"])),
		Ref:            firstNonEmpty(strings.TrimSpace(fmt.Sprint(raw["ref"])), "main"),
		DockerfilePath: firstNonEmpty(strings.TrimSpace(fmt.Sprint(raw["dockerfilePath"])), "Dockerfile"),
		ContextDir:     firstNonEmpty(strings.TrimSpace(fmt.Sprint(raw["contextDir"])), "."),
		Image:          strings.TrimSpace(fmt.Sprint(operation.Payload["image"])),
		Platform:       strings.TrimSpace(fmt.Sprint(operation.Payload["platform"])),
		Pull:           boolValue(raw["pull"], false),
		NoCache:        boolValue(raw["noCache"], false),
	}
	if err := validateGitDockerfileBuildSpec(spec); err != nil {
		return gitDockerfileBuildSpec{}, err
	}
	return spec, nil
}

func validateGitDockerfileBuildSpec(spec gitDockerfileBuildSpec) error {
	parsed, err := url.Parse(spec.RepositoryURL)
	if err != nil || parsed.Host == "" || !containsString([]string{"http", "https", "ssh"}, strings.ToLower(parsed.Scheme)) {
		return fmt.Errorf("repositoryUrl must use http, https, or ssh")
	}
	if parsed.User != nil && (parsed.Scheme != "ssh" || parsed.User.Username() != "git") {
		return fmt.Errorf("repositoryUrl must not contain credentials")
	}
	if parsed.User != nil {
		if _, hasPassword := parsed.User.Password(); hasPassword {
			return fmt.Errorf("repositoryUrl must not contain credentials")
		}
	}
	if spec.Image == "" {
		return fmt.Errorf("image is required for git Dockerfile build")
	}
	if spec.Ref == "" || strings.HasPrefix(spec.Ref, "-") || strings.ContainsAny(spec.Ref, "\r\n") {
		return fmt.Errorf("git ref is invalid")
	}
	for name, value := range map[string]string{"dockerfilePath": spec.DockerfilePath, "contextDir": spec.ContextDir} {
		cleaned := filepath.Clean(value)
		if filepath.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(os.PathSeparator)) {
			return fmt.Errorf("%s must stay inside the repository", name)
		}
	}
	return nil
}

func resolveRepositoryBuildPath(repositoryDir, relative string, wantDir bool) (string, error) {
	full, err := resolveWorkspacePath(repositoryDir, relative)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(full)
	if err != nil {
		return "", err
	}
	canonicalRoot, err := filepath.EvalSymlinks(repositoryDir)
	if err != nil {
		return "", err
	}
	relativeResolved, err := filepath.Rel(filepath.Clean(canonicalRoot), resolved)
	if err != nil {
		return "", err
	}
	resolved, err = resolveWorkspacePath(canonicalRoot, relativeResolved)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if wantDir && !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", relative)
	}
	if !wantDir && !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a regular file", relative)
	}
	return resolved, nil
}

func maskRepositoryURLLog(logs []string) []string {
	if len(logs) > 0 {
		logs[0] = "$ git remote [repository URL omitted]"
	}
	return logs
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
