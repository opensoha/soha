package runner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func (r *Runner) buildpacksGitEnvironment(ctx context.Context, address, home string) ([]string, error) {
	u, err := buildpacksRepositoryURL(address)
	if err != nil {
		return nil, err
	}
	env := append(r.buildpacksEnvironment(home), "LC_ALL=C", "GIT_ATTR_NOSYSTEM=1", "GIT_LFS_SKIP_SMUDGE=1")
	configuration := []string{
		"protocol.allow=never", "protocol.https.allow=always", "protocol.ssh.allow=always",
		"http.followRedirects=false", "http.proxy=", "http.sslVerify=true",
		"credential.helper=", "credential.useHttpPath=true", "core.hooksPath=/dev/null",
		"submodule.recurse=false", "fetch.recurseSubmodules=false",
	}
	secrets, _ := ctx.Value(secretValuesContextKey{}).(map[string]string)
	if u.Scheme == "ssh" {
		if secrets["GIT_SSH_KEY"] == "" || secrets["GIT_KNOWN_HOSTS"] == "" {
			return nil, fmt.Errorf("SSH requires GIT_SSH_KEY and GIT_KNOWN_HOSTS secret references")
		}
		for name, alias := range map[string]string{"git-identity": "GIT_SSH_KEY", "known_hosts": "GIT_KNOWN_HOSTS"} {
			if err := os.WriteFile(filepath.Join(home, name), []byte(secrets[alias]), 0o600); err != nil {
				return nil, err
			}
		}
		command := "ssh -F /dev/null -o BatchMode=yes -o IdentitiesOnly=yes -o IdentityAgent=none -o ForwardAgent=no -o ProxyCommand=none -o ProxyJump=none -o StrictHostKeyChecking=yes -o UpdateHostKeys=no -o GlobalKnownHostsFile=/dev/null"
		command += " -o UserKnownHostsFile=" + buildpacksShellQuote(filepath.Join(home, "known_hosts")) + " -i " + buildpacksShellQuote(filepath.Join(home, "git-identity"))
		env = append(env, "GIT_SSH_VARIANT=ssh", "GIT_SSH_COMMAND="+command)
	} else if secrets["GIT_PASSWORD"] != "" {
		username, password := firstNonEmpty(secrets["GIT_USERNAME"], "oauth2"), secrets["GIT_PASSWORD"]
		if strings.ContainsAny(username+password, "\x00\r\n") {
			return nil, fmt.Errorf("invalid HTTPS Git credentials")
		}
		// A fresh helper authorizes only this exact repository, including its path.
		helper := filepath.Join(home, "git-credential")
		script := `#!/bin/sh
[ "$1" = get ] || exit 0
while IFS='=' read -r key value; do
 case "$key" in protocol) protocol="$value";; host) host="$value";; path) path="$value";; esac
done
[ "$protocol" = https ] && [ "$host" = "$SOHA_GIT_HOST" ] && [ "$path" = "$SOHA_GIT_PATH" ] || exit 0
printf 'username=%s\npassword=%s\n' "$SOHA_GIT_USERNAME" "$SOHA_GIT_PASSWORD"
`
		// #nosec G306 -- Owner-only fixed helper; credentials never enter the script or command arguments.
		if err := os.WriteFile(helper, []byte(script), 0o700); err != nil {
			return nil, err
		}
		configuration = append(configuration, "credential.helper=!"+buildpacksShellQuote(helper))
		env = append(env, "SOHA_GIT_HOST="+u.Host, "SOHA_GIT_PATH="+strings.TrimPrefix(u.Path, "/"), "SOHA_GIT_USERNAME="+username, "SOHA_GIT_PASSWORD="+password)
	}
	env = append(env, "GIT_CONFIG_COUNT="+strconv.Itoa(len(configuration)))
	for i, entry := range configuration {
		key, value, _ := strings.Cut(entry, "=")
		env = append(env, "GIT_CONFIG_KEY_"+strconv.Itoa(i)+"="+key, "GIT_CONFIG_VALUE_"+strconv.Itoa(i)+"="+value)
	}
	return env, nil
}

func buildpacksShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func (r *Runner) validateBuildpacksSubmodules(ctx context.Context, parent buildpacksCheckout, checkouts []buildpacksCheckout, directory, home string) error {
	env := r.buildpacksEnvironment(home)
	tree, err := buildpacksCommand(ctx, directory, env, "git", "ls-tree", "-rz", "HEAD")
	if err != nil {
		return err
	}
	links := map[string]string{}
	for _, entry := range strings.Split(tree, "\x00") {
		header, path, _ := strings.Cut(entry, "\t")
		fields := strings.Fields(header)
		if len(fields) == 3 && fields[0] == "160000" {
			if fields[1] != "commit" || !gitCommitPattern.MatchString(fields[2]) || len(links) >= 32 {
				return fmt.Errorf("invalid or excessive Buildpacks submodules")
			}
			links[path] = fields[2]
		}
	}
	if len(links) == 0 {
		return nil
	}
	// Parse the immutable blob through Git; never evaluate repository commands or includes.
	data, err := buildpacksCommand(ctx, directory, env, "git", "config", "--no-includes", "--blob", "HEAD:.gitmodules", "--null", "--get-regexp", `^submodule\..*\.(path|url)$`)
	if err != nil {
		return fmt.Errorf("buildpacks submodules require a valid .gitmodules blob")
	}
	modules := map[string]map[string]string{}
	for _, entry := range strings.Split(strings.TrimSuffix(data, "\x00"), "\x00") {
		key, value, ok := strings.Cut(entry, "\n")
		index := strings.LastIndex(key, ".")
		if !ok || index < 0 {
			return fmt.Errorf("invalid Buildpacks submodule declaration")
		}
		name, field := key[:index], key[index+1:]
		if modules[name] == nil {
			modules[name] = map[string]string{}
		}
		if _, duplicate := modules[name][field]; duplicate {
			return fmt.Errorf("duplicate Buildpacks submodule field")
		}
		modules[name][field] = value
	}
	return matchBuildpacksSubmodules(parent, checkouts, links, modules)
}

func matchBuildpacksSubmodules(parent buildpacksCheckout, checkouts []buildpacksCheckout, links map[string]string, modules map[string]map[string]string) error {
	base, err := buildpacksRepositoryURL(parent.RepositoryURL)
	if err != nil {
		return err
	}
	base.Path += "/"
	for _, module := range modules {
		path, address := module["path"], module["url"]
		commit, exists := links[path]
		if !exists || address == "" || path == "." || strings.Contains(path, "\\") {
			return fmt.Errorf("buildpacks submodule path is absent or duplicated")
		}
		if _, err := resolveWorkspacePath("/workspace", path); err != nil {
			return err
		}
		if strings.HasPrefix(address, "./") || strings.HasPrefix(address, "../") {
			resolved, err := base.Parse(address)
			if err != nil {
				return fmt.Errorf("invalid relative Buildpacks submodule URL")
			}
			address = resolved.String()
		}
		u, err := buildpacksRepositoryURL(address)
		if err != nil {
			return err
		}
		bound := false
		for _, source := range checkouts {
			if source.CheckoutPath == filepath.Join(parent.CheckoutPath, path) && source.RepositoryURL == u.String() && source.RefName == commit {
				bound = true
				break
			}
		}
		if !bound {
			return fmt.Errorf("buildpacks submodule must be explicitly bound at its path, repository URL and gitlink commit")
		}
		delete(links, path)
	}
	if len(links) != 0 {
		return fmt.Errorf("buildpacks submodule has no source declaration")
	}
	return nil
}
