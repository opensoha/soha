package manifestruntime

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaindocument "github.com/opensoha/soha/internal/domain/deliverydocument"
)

type DeliveryGitAccessResolver interface {
	ResolveDeliveryGitAccess(context.Context, domainapp.SourceRepository) (domainapp.GitSourceAccess, error)
}

type DeliveryGit struct{ access DeliveryGitAccessResolver }

func NewDeliveryGit(access DeliveryGitAccessResolver) *DeliveryGit {
	return &DeliveryGit{access: access}
}

func (g *DeliveryGit) ReadDeliveryDocuments(ctx context.Context, repository domainapp.SourceRepository, source domaindocument.Source) (domaindocument.GitDocuments, error) {
	var result domaindocument.GitDocuments
	if source.RepositoryID != repository.ID {
		return result, fmt.Errorf("source repository changed")
	}
	err := g.withReader(ctx, repository, func(ctx context.Context, reader deliveryGitReader) error {
		var err error
		result, err = reader.read(ctx, repository.URL, source)
		return err
	})
	return result, err
}

// ResolveDeliveryCommit uses the same pinned network, provider identity and host trust as template reads.
func (g *DeliveryGit) ResolveDeliveryCommit(ctx context.Context, repository domainapp.SourceRepository, refType, refName string) (string, error) {
	var commit string
	err := g.withReader(ctx, repository, func(ctx context.Context, reader deliveryGitReader) error {
		var err error
		commit, err = reader.fetch(ctx, repository.URL, refType, refName)
		return err
	})
	return commit, err
}

func (g *DeliveryGit) withReader(ctx context.Context, repository domainapp.SourceRepository, read func(context.Context, deliveryGitReader) error) error {
	if g.access == nil {
		return fmt.Errorf("source repository access is unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, gitSyncTimeout)
	defer cancel()
	access, err := g.access.ResolveDeliveryGitAccess(ctx, repository)
	if err != nil {
		return err
	}
	if access.RepositoryURL != repository.URL || access.Address == "" {
		return fmt.Errorf("source repository access changed")
	}
	workspace, err := os.MkdirTemp("", "soha-delivery-git-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(workspace) }()
	environment, err := deliveryGitEnvironment(repository, access, workspace)
	if err != nil {
		return err
	}
	reader := deliveryGitReader{directory: filepath.Join(workspace, "repository"), environment: environment}
	return read(ctx, reader)
}

func deliveryGitEnvironment(repository domainapp.SourceRepository, access domainapp.GitSourceAccess, workspace string) ([]string, error) {
	environment := []string{
		"PATH=" + os.Getenv("PATH"), "HOME=" + workspace, "XDG_CONFIG_HOME=" + workspace, "LC_ALL=C",
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_ATTR_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "GIT_LFS_SKIP_SMUDGE=1",
	}
	configuration := []string{
		"protocol.allow=never", "protocol.https.allow=always", "protocol.ssh.allow=always",
		"http.followRedirects=false", "http.proxy=", "http.sslVerify=true", "http.lowSpeedLimit=1", "http.lowSpeedTime=15",
		"credential.helper=", "core.hooksPath=" + os.DevNull,
		"submodule.recurse=false", "fetch.recurseSubmodules=false", "core.pager=cat",
	}
	if len(access.Credentials) > 0 || access.Scheme == "ssh" {
		if access.Scheme == "ssh" && firstGitCredential(access.Credentials, "known_hosts") == "" {
			return nil, fmt.Errorf("SSH template sources require a configured known_hosts entry")
		}
		credentials, err := prepareGitCredentialEnvironment(repository, access.Credentials, workspace)
		if err != nil {
			return nil, err
		}
		environment = append(environment, credentials...)
	}
	switch access.Scheme {
	case "https":
		address := access.Address
		if strings.Contains(address, ":") {
			address = "[" + address + "]"
		}
		configuration = append(configuration, "http.curloptResolve="+access.Host+":"+access.Port+":"+address)
		if access.CertificateAuthority != "" {
			certificatePath := filepath.Join(workspace, "source-ca.pem")
			if err := os.WriteFile(certificatePath, []byte(access.CertificateAuthority), 0o600); err != nil {
				return nil, err
			}
			configuration = append(configuration, "http.sslCAInfo="+certificatePath)
		}
	case "ssh":
		// Git invokes this command through a shell. All interpolated options use
		// shell quoting; repository contents never reach the command string.
		command := "ssh -F " + gitShellQuote(os.DevNull) + " -o BatchMode=yes -o IdentitiesOnly=yes -o IdentityAgent=none -o ForwardAgent=no -o ProxyCommand=none -o ProxyJump=none -o StrictHostKeyChecking=yes -o CheckHostIP=no -o UpdateHostKeys=no -o GlobalKnownHostsFile=" + gitShellQuote(os.DevNull)
		hostAlias := access.Host
		if access.Port != "22" {
			hostAlias = "[" + access.Host + "]:" + access.Port
		}
		command += " -o HostName=" + gitShellQuote(access.Address) + " -o HostKeyAlias=" + gitShellQuote(hostAlias)
		command += " -o UserKnownHostsFile=" + gitShellQuote(filepath.Join(workspace, "known_hosts")) + " -i " + gitShellQuote(filepath.Join(workspace, "git-identity"))
		// Replace the older helper command with the pinned, strict SSH command.
		for index, entry := range environment {
			if strings.HasPrefix(entry, "GIT_SSH_COMMAND=") {
				environment[index] = "GIT_SSH_COMMAND=" + command
			}
		}
	default:
		return nil, fmt.Errorf("template source transport must be HTTPS or SSH")
	}
	environment = append(environment, "GIT_CONFIG_COUNT="+strconv.Itoa(len(configuration)))
	for index, entry := range configuration {
		key, value, _ := strings.Cut(entry, "=")
		environment = append(environment, "GIT_CONFIG_KEY_"+strconv.Itoa(index)+"="+key, "GIT_CONFIG_VALUE_"+strconv.Itoa(index)+"="+value)
	}
	return environment, nil
}

func gitShellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }

type deliveryGitReader struct {
	directory   string
	environment []string
}

func (r deliveryGitReader) read(ctx context.Context, repositoryURL string, source domaindocument.Source) (domaindocument.GitDocuments, error) {
	commit, err := r.fetch(ctx, repositoryURL, string(source.RefType), source.RefValue)
	if err != nil {
		return domaindocument.GitDocuments{}, err
	}
	tree, err := r.output(ctx, 128, "rev-parse", commit+"^{tree}")
	if err != nil {
		return domaindocument.GitDocuments{}, err
	}
	files, err := r.files(ctx, commit, source)
	if err != nil {
		return domaindocument.GitDocuments{}, err
	}
	return domaindocument.GitDocuments{ResolvedCommit: commit, TreeDigest: strings.TrimSpace(tree), Files: files}, nil
}

func (r deliveryGitReader) fetch(ctx context.Context, repositoryURL, refType, refName string) (string, error) {
	ref, err := buildGitRef(refType, refName)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(r.directory, 0o700); err != nil {
		return "", err
	}
	// A bare repository never runs checkout hooks, LFS/smudge filters or code
	// from the source. Only selected immutable blob objects are read below.
	commands := [][]string{{"init", "--bare", "--template=", "."}, {"remote", "add", "origin", repositoryURL}}
	if refType != "commit" {
		commands = append(commands, []string{"check-ref-format", ref})
	}
	commands = append(commands, []string{"fetch", "--no-tags", "--depth=1", "--filter=blob:none", "--", "origin", ref})
	for _, args := range commands {
		if _, err := r.output(ctx, 64<<10, args...); err != nil {
			return "", err
		}
	}
	commit, err := r.output(ctx, 128, "rev-parse", "FETCH_HEAD^{commit}")
	if err != nil {
		return "", err
	}
	commit = strings.TrimSpace(commit)
	if !gitCommitPattern.MatchString(commit) || refType == "commit" && commit != ref {
		return "", fmt.Errorf("Git did not return the requested commit")
	}
	return commit, nil
}

func (r deliveryGitReader) output(ctx context.Context, limit int, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir, command.Env = r.directory, r.environment
	command.WaitDelay = time.Second
	output := &boundedGitOutput{limit: limit}
	command.Stdout, command.Stderr = output, io.Discard
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("Git source %s failed; verify the repository, ref, connection credentials and host trust", args[0])
	}
	if output.overflow {
		return "", fmt.Errorf("Git source output exceeds the bounded read limit")
	}
	return output.String(), nil
}

type boundedGitOutput struct {
	buffer   bytes.Buffer
	limit    int
	overflow bool
}

func (b *boundedGitOutput) String() string { return b.buffer.String() }

func (b *boundedGitOutput) Write(value []byte) (int, error) {
	size := len(value)
	remaining := b.limit - b.buffer.Len()
	if size > remaining {
		b.overflow = true
		value = value[:remaining]
	}
	_, _ = b.buffer.Write(value)
	return size, nil
}
