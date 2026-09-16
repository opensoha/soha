package manifestruntime

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaindocument "github.com/opensoha/soha/internal/domain/deliverydocument"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func sshFixtureKey(t *testing.T) (ssh.Signer, string) {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := ssh.MarshalPrivateKey(private, "Git fixture")
	if err != nil {
		t.Fatal(err)
	}
	return signer, string(pem.EncodeToMemory(encoded))
}

func deliverySSHFixture(t *testing.T, root string) (domainapp.SourceRepository, domainapp.GitSourceAccess, *atomic.Int32) {
	t.Helper()
	clientKey, privateKey := sshFixtureKey(t)
	hostKey, _ := sshFixtureKey(t)
	config := &ssh.ServerConfig{PublicKeyCallback: func(connection ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
		if connection.User() != "git" || !bytes.Equal(key.Marshal(), clientKey.PublicKey().Marshal()) {
			return nil, fmt.Errorf("fixture key denied")
		}
		return nil, nil
	}}
	config.AddHostKey(hostKey)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var sessions sync.WaitGroup
	commands := &atomic.Int32{}
	sessions.Add(1)
	go func() {
		defer sessions.Done()
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			sessions.Add(1)
			go func() {
				defer sessions.Done()
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
					serveGitSSHSession(ctx, session, requests, root, commands)
				}
			}()
		}
	}()
	t.Cleanup(func() {
		cancel()
		_ = listener.Close()
		sessions.Wait()
	})
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	host := "git.example.invalid"
	cloneURL := "ssh://git@" + net.JoinHostPort(host, port) + root
	repository := domainapp.SourceRepository{ID: "repo", URL: cloneURL, Provider: "gitlab"}
	access := domainapp.GitSourceAccess{RepositoryURL: cloneURL, Scheme: "ssh", Host: host, Port: port, Address: "127.0.0.1", Credentials: map[string]string{"private_key": privateKey, "known_hosts": knownhosts.Line([]string{net.JoinHostPort(host, port)}, hostKey.PublicKey()) + "\n"}}
	return repository, access, commands
}

func serveGitSSHSession(ctx context.Context, session ssh.Channel, requests <-chan *ssh.Request, root string, commands *atomic.Int32) {
	defer func() { _ = session.Close() }()
	protocol := ""
	for request := range requests {
		if request.Type != "exec" {
			var value struct{ Name, Value string }
			accepted := request.Type == "env" && ssh.Unmarshal(request.Payload, &value) == nil && value.Name == "GIT_PROTOCOL" && value.Value == "version=2"
			if accepted {
				protocol = value.Value
			}
			_ = request.Reply(accepted, nil)
			continue
		}
		var payload struct{ Command string }
		if ssh.Unmarshal(request.Payload, &payload) != nil || payload.Command != "git-upload-pack '"+root+"'" {
			_ = request.Reply(false, nil)
			return
		}
		_ = request.Reply(true, nil)
		commands.Add(1)
		command := exec.CommandContext(ctx, "git-upload-pack", root)
		command.Env = append(os.Environ(), "GIT_PROTOCOL="+protocol)
		command.Stdin, command.Stdout, command.Stderr = session, session, io.Discard
		status := uint32(0)
		if err := command.Run(); err != nil {
			status = 1
		}
		_, _ = session.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{status}))
		return
	}
}

func TestDeliveryGitSSHRequiresPinnedHostKeyAndPrivateKey(t *testing.T) {
	root, commit := newDeliveryGitFixture(t, map[string]string{"catalog/flow.soha.yaml": "kind: WorkflowTemplate\n"})
	repository, access, commands := deliverySSHFixture(t, root)
	source := domaindocument.Source{RepositoryID: repository.ID, RefType: "branch", RefValue: "main", Path: "catalog"}
	t.Setenv("GIT_SSH_COMMAND", "exit 1")
	result, err := NewDeliveryGit(deliveryGitAccessStub{access}).ReadDeliveryDocuments(t.Context(), repository, source)
	if err != nil || result.ResolvedCommit != commit || len(result.Files) != 1 {
		t.Fatalf("SSH fixed read failed: %v", err)
	}
	before := commands.Load()
	wrong, _ := sshFixtureKey(t)
	access.Credentials["known_hosts"] = knownhosts.Line([]string{net.JoinHostPort(access.Host, access.Port)}, wrong.PublicKey()) + "\n"
	if _, err := NewDeliveryGit(deliveryGitAccessStub{access}).ReadDeliveryDocuments(t.Context(), repository, source); err == nil || strings.Contains(err.Error(), "PRIVATE KEY") {
		t.Fatal("changed host key accepted or private key disclosed")
	}
	if commands.Load() != before {
		t.Fatal("untrusted SSH host ran a Git command")
	}
	delete(access.Credentials, "known_hosts")
	if _, err := NewDeliveryGit(deliveryGitAccessStub{access}).ReadDeliveryDocuments(t.Context(), repository, source); err == nil {
		t.Fatal("missing known_hosts accepted")
	}
}
