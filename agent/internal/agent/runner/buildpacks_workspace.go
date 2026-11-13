package runner

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

type buildpacksCheckout struct {
	RepositoryURL string `json:"repositoryURL"`
	RefType       string `json:"refType"`
	RefName       string `json:"refName"`
	CheckoutPath  string `json:"checkoutPath"`
	Submodules    bool   `json:"submodules"`
}

var buildpacksEnvironmentKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)

func decodeBuildpacksSpec(task ExecutionTask) (sohaapi.BuildpacksExecutionSpec, error) {
	var spec sohaapi.BuildpacksExecutionSpec
	data, err := json.Marshal(task.Payload["buildpacks"])
	if err != nil {
		return spec, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&spec); err != nil {
		return spec, fmt.Errorf("invalid Buildpacks specification")
	}
	if len(spec.Environment) > 128 || strings.ContainsAny(spec.Configuration.ProcessType, "\x00\r\n") || len(spec.Configuration.ProcessType) > 128 {
		return spec, fmt.Errorf("invalid Buildpacks environment or process type")
	}
	return spec, nil
}

func prepareBuildpacksEnvironment(ctx context.Context, home string, variables map[string]string) error {
	values := make(map[string]string, len(variables))
	for key, value := range variables {
		values[key] = value
	}
	secrets, _ := ctx.Value(secretValuesContextKey{}).(map[string]string)
	for key, value := range secrets {
		if key != "GIT_USERNAME" && key != "GIT_PASSWORD" && key != "GIT_SSH_KEY" && key != "GIT_KNOWN_HOSTS" && key != "REGISTRY_AUTH" {
			values[key] = value
		}
	}
	if len(values) > 128 {
		return fmt.Errorf("too many Buildpacks environment values")
	}
	keys := make([]string, 0, len(values))
	for key, value := range values {
		if !buildpacksEnvironmentKey.MatchString(key) || strings.ContainsAny(value, "\x00\r\n") || len(value) > 16384 {
			return fmt.Errorf("invalid Buildpacks environment value")
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var output strings.Builder
	for _, key := range keys {
		fmt.Fprintf(&output, "%s=%s\n", key, values[key])
	}
	if err := os.WriteFile(filepath.Join(home, "build.env"), []byte(output.String()), 0o600); err != nil {
		return err
	}
	return writeBuildpacksRegistryConfig(home, secrets["REGISTRY_AUTH"])
}

type buildpacksDockerConfig struct {
	Auths map[string]struct {
		Auth          string `json:"auth,omitempty"`
		Username      string `json:"username,omitempty"`
		Password      string `json:"password,omitempty"`
		IdentityToken string `json:"identitytoken,omitempty"`
	} `json:"auths"`
}

func buildpacksRegistrySecrets(value string) []string {
	var config buildpacksDockerConfig
	if json.Unmarshal([]byte(value), &config) != nil {
		return nil
	}
	var secrets []string
	for _, auth := range config.Auths {
		secrets = append(secrets, auth.Auth, auth.Password, auth.IdentityToken)
		if decoded, err := base64.StdEncoding.DecodeString(auth.Auth); err == nil {
			_, password, _ := strings.Cut(string(decoded), ":")
			secrets = append(secrets, string(decoded), password)
		}
	}
	return secrets
}

func writeBuildpacksRegistryConfig(home, value string) error {
	if value == "" {
		value = `{"auths":{}}`
	}
	var config buildpacksDockerConfig
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.DisallowUnknownFields()
	if len(value) > 65536 || decoder.Decode(&config) != nil || config.Auths == nil {
		return fmt.Errorf("REGISTRY_AUTH must contain inline Docker auths without credential helpers")
	}
	data, err := json.Marshal(config)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(home, "config.json"), data, 0o600)
}

func buildpacksCheckouts(task ExecutionTask) ([]buildpacksCheckout, error) {
	data, err := json.Marshal(task.Payload["workspace"])
	if err != nil {
		return nil, err
	}
	var workspace struct {
		Checkout  buildpacksCheckout   `json:"checkout"`
		Checkouts []buildpacksCheckout `json:"checkouts"`
	}
	if json.Unmarshal(data, &workspace) != nil {
		return nil, fmt.Errorf("invalid Buildpacks workspace")
	}
	if len(workspace.Checkouts) == 0 {
		workspace.Checkouts = []buildpacksCheckout{workspace.Checkout}
	}
	if len(workspace.Checkouts) > 32 {
		return nil, fmt.Errorf("too many Buildpacks source repositories")
	}
	paths := make(map[string]bool, len(workspace.Checkouts))
	for index := range workspace.Checkouts {
		checkout := &workspace.Checkouts[index]
		if checkout.RefType != "commit" || !gitCommitPattern.MatchString(checkout.RefName) {
			return nil, fmt.Errorf("buildpacks requires fixed source commits")
		}
		u, err := buildpacksRepositoryURL(checkout.RepositoryURL)
		if err != nil {
			return nil, err
		}
		checkout.RepositoryURL = u.String()
		if _, err := resolveWorkspacePath("/workspace", checkout.CheckoutPath); err != nil || strings.Contains(checkout.CheckoutPath, "\\") {
			return nil, fmt.Errorf("invalid Buildpacks checkout path")
		}
		for _, part := range strings.Split(checkout.CheckoutPath, "/") {
			if part == ".." || strings.EqualFold(part, ".git") {
				return nil, fmt.Errorf("buildpacks checkout path cannot address parent or Git metadata directories")
			}
		}
		checkout.CheckoutPath = filepath.Clean(checkout.CheckoutPath)
		if paths[checkout.CheckoutPath] {
			return nil, fmt.Errorf("duplicate Buildpacks checkout path")
		}
		paths[checkout.CheckoutPath] = true
	}
	// Parents must be checked out before their explicitly authorized submodules.
	sort.Slice(workspace.Checkouts, func(i, j int) bool { return workspace.Checkouts[i].CheckoutPath < workspace.Checkouts[j].CheckoutPath })
	return workspace.Checkouts, nil
}

func buildpacksRepositoryURL(address string) (*url.URL, error) {
	if strings.HasPrefix(address, "git@") && !strings.Contains(address, "://") {
		host, repository, found := strings.Cut(strings.TrimPrefix(address, "git@"), ":")
		if found {
			address = "ssh://git@" + host + "/" + repository
		}
	}
	u, err := url.Parse(address)
	if err != nil || u.Host == "" || u.Path == "" || u.RawQuery != "" || u.Fragment != "" || strings.ContainsAny(address, "\x00\r\n\\") || strings.ContainsAny(u.Path, "\x00\r\n\\") || (u.Scheme != "https" && u.Scheme != "ssh") || (u.User != nil && (u.Scheme != "ssh" || u.User.String() != "git")) {
		return nil, fmt.Errorf("buildpacks source requires a credential-free HTTPS or SSH URL")
	}
	return u, nil
}

func (r *Runner) checkoutBuildpacksSources(ctx context.Context, task ExecutionTask, root, home string) error {
	checkouts, err := buildpacksCheckouts(task)
	if err != nil {
		return err
	}
	workspace, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer func() { _ = workspace.Close() }()
	for _, checkout := range checkouts {
		path, err := resolveWorkspacePath(root, checkout.CheckoutPath)
		if err != nil {
			return err
		}
		ancestor := "."
		for _, part := range strings.Split(checkout.CheckoutPath, "/") {
			ancestor = filepath.Join(ancestor, part)
			info, err := workspace.Lstat(ancestor)
			if os.IsNotExist(err) {
				break
			}
			if err != nil || !info.IsDir() {
				return fmt.Errorf("buildpacks checkout ancestors must be directories without symbolic links")
			}
		}
		if err := workspace.MkdirAll(checkout.CheckoutPath, 0o700); err != nil {
			return err
		}
		if _, err := resolveRepositoryBuildPath(root, checkout.CheckoutPath, true); err != nil {
			return err
		}
		entries, err := os.ReadDir(path)
		if err != nil || len(entries) != 0 {
			return fmt.Errorf("buildpacks checkout would overwrite an existing source path")
		}
		if err := r.checkoutBuildpacksSource(ctx, checkout, path, home); err != nil {
			return err
		}
		if checkout.Submodules {
			if err := r.validateBuildpacksSubmodules(ctx, checkout, checkouts, path, home); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *Runner) checkoutBuildpacksSource(ctx context.Context, source buildpacksCheckout, path, home string) error {
	env, err := r.buildpacksGitEnvironment(ctx, source.RepositoryURL, home)
	if err != nil {
		return err
	}
	commands := [][]string{
		{"init", "--quiet", "--template=", "."}, {"remote", "add", "origin", source.RepositoryURL},
		{"fetch", "--quiet", "--no-tags", "--depth=1", "--", "origin", source.RefName},
		{"checkout", "--quiet", "--detach", "FETCH_HEAD"},
	}
	for _, args := range commands {
		if _, err := buildpacksCommand(ctx, path, env, "git", args...); err != nil {
			return err
		}
	}
	commit, err := buildpacksCommand(ctx, path, env, "git", "rev-parse", "HEAD")
	if err != nil || strings.TrimSpace(commit) != source.RefName {
		return fmt.Errorf("buildpacks source commit mismatch")
	}
	return nil
}
