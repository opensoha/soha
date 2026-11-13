package runner

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func buildpacksGitFixture(t *testing.T, files map[string]string, gitlink string) (string, string) {
	t.Helper()
	directory := t.TempDir()
	for name, value := range files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(directory, name)), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, name), []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	commands := [][]string{{"init", "--quiet", "--template=", "."}, {"add", "."}}
	if gitlink != "" {
		commands = append(commands, []string{"update-index", "--add", "--cacheinfo", "160000," + gitlink + ",lib"})
	}
	commands = append(commands, []string{"-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false", "commit", "--quiet", "-m", "fixture"}, []string{"rev-parse", "HEAD"})
	var result string
	for _, args := range commands {
		var err error
		result, err = buildpacksCommand(t.Context(), directory, []string{"PATH=" + os.Getenv("PATH"), "HOME=" + directory, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null"}, "git", args...)
		if err != nil {
			t.Fatal(err, result)
		}
	}
	return directory, strings.TrimSpace(result)
}

func buildpacksSSHKey(t *testing.T) (ssh.Signer, string) {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(key, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	return signer, string(pem.EncodeToMemory(block))
}

// A real SSH transport serving only the two isolated Git fixtures.
func buildpacksSSHFixture(t *testing.T, repositories map[string]string) (string, map[string]string, *atomic.Int32) {
	t.Helper()
	client, private := buildpacksSSHKey(t)
	host, _ := buildpacksSSHKey(t)
	config := &ssh.ServerConfig{PublicKeyCallback: func(connection ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
		if connection.User() != "git" || !bytes.Equal(key.Marshal(), client.PublicKey().Marshal()) {
			return nil, fmt.Errorf("fixture key denied")
		}
		return nil, nil
	}}
	config.AddHostKey(host)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	var sessions sync.WaitGroup
	count := &atomic.Int32{}
	sessions.Go(func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			sessions.Go(func() {
				defer func() { _ = connection.Close() }()
				stop := context.AfterFunc(ctx, func() { _ = connection.Close() })
				defer stop()
				server, channels, requests, err := ssh.NewServerConn(connection, config)
				if err != nil {
					return
				}
				defer func() { _ = server.Close() }()
				go ssh.DiscardRequests(requests)
				for channel := range channels {
					if channel.ChannelType() != "session" {
						_ = channel.Reject(ssh.UnknownChannelType, "session required")
						continue
					}
					session, requests, err := channel.Accept()
					if err != nil {
						return
					}
					serveBuildpacksGitSSH(ctx, session, requests, repositories, count)
				}
			})
		}
	})
	t.Cleanup(func() { cancel(); _ = listener.Close(); sessions.Wait() })
	address := listener.Addr().String()
	return "ssh://git@" + address, map[string]string{"GIT_SSH_KEY": private, "GIT_KNOWN_HOSTS": knownhosts.Line([]string{address}, host.PublicKey()) + "\n"}, count
}

func serveBuildpacksGitSSH(ctx context.Context, session ssh.Channel, requests <-chan *ssh.Request, repositories map[string]string, count *atomic.Int32) {
	defer func() { _ = session.Close() }()
	for request := range requests {
		var payload struct{ Command string }
		if request.Type != "exec" || ssh.Unmarshal(request.Payload, &payload) != nil {
			_ = request.Reply(false, nil)
			continue
		}
		directory := ""
		for path, local := range repositories {
			if payload.Command == "git-upload-pack '"+path+"'" {
				directory = local
			}
		}
		if directory == "" {
			_ = request.Reply(false, nil)
			return
		}
		_ = request.Reply(true, nil)
		count.Add(1)
		command := exec.CommandContext(ctx, "git-upload-pack", directory)
		command.Stdin, command.Stdout, command.Stderr = session, session, io.Discard
		status := uint32(0)
		if command.Run() != nil {
			status = 1
		}
		_, _ = session.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{status}))
		return
	}
}

func TestBuildpacksSSHAndSubmodulesUseOnlyBoundCommits(t *testing.T) {
	library, libCommit := buildpacksGitFixture(t, map[string]string{"value.txt": "pinned library"}, "")
	main, mainCommit := buildpacksGitFixture(t, map[string]string{".gitmodules": "[submodule \"lib\"]\npath = lib\nurl = ../lib.git\n"}, libCommit)
	base, secrets, count := buildpacksSSHFixture(t, map[string]string{"/app.git": main, "/lib.git": library})
	r := &Runner{}
	t.Setenv("GIT_SSH_COMMAND", "exit 1")
	ctx := context.WithValue(t.Context(), secretValuesContextKey{}, secrets)
	parent := buildpacksCheckout{RepositoryURL: base + "/app.git", RefType: "commit", RefName: mainCommit, Submodules: true}
	child := buildpacksCheckout{RepositoryURL: base + "/lib.git", RefType: "commit", RefName: libCommit, CheckoutPath: "lib"}
	task := func(sources ...buildpacksCheckout) ExecutionTask {
		return ExecutionTask{Payload: map[string]any{"workspace": map[string]any{"checkouts": sources}}}
	}
	root, home := t.TempDir(), t.TempDir()
	if err := r.checkoutBuildpacksSources(ctx, task(child, parent), root, home); err != nil {
		t.Fatal(err)
	}
	content, err := fs.ReadFile(os.DirFS(root), "lib/value.txt")
	if err != nil || string(content) != "pinned library" || count.Load() != 2 {
		t.Fatal("fixed Git submodule was not checked out", err)
	}
	for _, bad := range []buildpacksCheckout{{}, {RepositoryURL: base + "/lib.git", RefType: "commit", RefName: mainCommit, CheckoutPath: "lib"}, {RepositoryURL: "https://other.example/lib.git", RefType: "commit", RefName: libCommit, CheckoutPath: "lib"}} {
		before := count.Load()
		sources := []buildpacksCheckout{parent}
		if bad.RepositoryURL != "" {
			sources = append(sources, bad)
		}
		if err := r.checkoutBuildpacksSources(ctx, task(sources...), t.TempDir(), t.TempDir()); err == nil || count.Load() != before+1 {
			t.Fatal("unbound or mismatched submodule fetched", err)
		}
	}
	wrongHost, _ := buildpacksSSHKey(t)
	secrets["GIT_KNOWN_HOSTS"] = knownhosts.Line([]string{strings.TrimPrefix(base, "ssh://git@")}, wrongHost.PublicKey()) + "\n"
	before := count.Load()
	if err := r.checkoutBuildpacksSources(ctx, task(parent), t.TempDir(), t.TempDir()); err == nil || count.Load() != before {
		t.Fatal("untrusted SSH host accepted", err)
	}
	if err := prepareBuildpacksEnvironment(ctx, home, nil); err != nil {
		t.Fatal(err)
	}
	buildEnv, err := fs.ReadFile(os.DirFS(home), "build.env")
	if err != nil || len(buildEnv) != 0 {
		t.Fatal("source credentials were exposed to Buildpacks", err)
	}
}

func TestBuildpacksGitCredentialHelperIsRepositoryScoped(t *testing.T) {
	r := &Runner{}
	ctx := context.WithValue(t.Context(), secretValuesContextKey{}, map[string]string{"GIT_USERNAME": "fixture", "GIT_PASSWORD": "fixture-password"})
	env, err := r.buildpacksGitEnvironment(ctx, "https://git.example/group/app.git", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, address := range []string{"https://git.example/group/app.git", "https://git.example/other.git", "https://other.example/group/app.git"} {
		command := exec.CommandContext(t.Context(), "git", "credential", "fill")
		command.Env, command.Stdin = env, strings.NewReader("url="+address+"\n\n")
		output, err := command.Output()
		if address == "https://git.example/group/app.git" {
			if err != nil || !strings.Contains(string(output), "password=fixture-password") {
				t.Fatal("authorized repository could not obtain credentials", err)
			}
		} else if err == nil || strings.Contains(string(output), "fixture-password") {
			t.Fatal("credentials escaped their repository")
		}
	}
}

func TestBuildpacksCheckoutCannotCreateThroughSourceSymlink(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	source := buildpacksCheckout{RepositoryURL: "https://git.example/app.git", RefType: "commit", RefName: strings.Repeat("a", 40), CheckoutPath: "link/created"}
	task := ExecutionTask{Payload: map[string]any{"workspace": map[string]any{"checkouts": []buildpacksCheckout{source}}}}
	if err := (&Runner{}).checkoutBuildpacksSources(t.Context(), task, root, t.TempDir()); err == nil {
		t.Fatal("checkout followed a source symlink")
	}
	if _, err := os.Stat(filepath.Join(outside, "created")); !os.IsNotExist(err) {
		t.Fatal("checkout created an external directory before refusing the symlink")
	}
}

func TestBuildpacksRelativeSubmoduleMatchesNativeGit(t *testing.T) {
	for _, relative := range []string{"./child.git", "../child.git"} {
		root, _ := buildpacksGitFixture(t, map[string]string{".gitmodules": "[submodule \"lib\"]\npath = lib\nurl = " + relative + "\n"}, strings.Repeat("a", 40))
		env := (&Runner{}).buildpacksEnvironment(t.TempDir())
		parent := buildpacksCheckout{RepositoryURL: "https://git.example/group/app.git", CheckoutPath: "."}
		for _, args := range [][]string{{"remote", "add", "origin", parent.RepositoryURL}, {"submodule", "init"}} {
			if output, err := buildpacksCommand(t.Context(), root, env, "git", args...); err != nil {
				t.Fatal(err, output)
			}
		}
		address, err := buildpacksCommand(t.Context(), root, env, "git", "config", "--get", "submodule.lib.url")
		if err != nil {
			t.Fatal(err)
		}
		child := buildpacksCheckout{RepositoryURL: strings.TrimSpace(address), CheckoutPath: "lib", RefName: strings.Repeat("a", 40)}
		if err := matchBuildpacksSubmodules(parent, []buildpacksCheckout{parent, child}, map[string]string{"lib": child.RefName}, map[string]map[string]string{"lib": {"path": "lib", "url": relative}}); err != nil {
			t.Fatalf("native Git URL %s disagrees with Buildpacks: %v", address, err)
		}
	}
}
