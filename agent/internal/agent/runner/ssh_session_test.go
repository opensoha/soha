package runner

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"io"
	"net"
	"strconv"
	"testing"
	"time"

	cfgpkg "github.com/opensoha/soha-agent/internal/agent/config"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"golang.org/x/crypto/ssh"
)

func testSSHSigner(t *testing.T) ssh.Signer {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return signer
}

func TestSSHRequestTrustBoundary(t *testing.T) {
	signer := testSSHSigner(t)
	cfg := cfgpkg.ControlPlaneConfig{AgentID: "agent-one", BearerToken: "test-token", BaseURL: "https://control.example"}
	// #nosec G101 -- Synthetic lease token for request validation.
	valid := sohaapi.AgentSSHSessionRequest{SessionID: "session-one", AgentID: cfg.AgentID, ExecutionClusterID: "cluster-one", ControlPlaneEndpoint: cfg.BaseURL, Address: "10.0.0.20", Port: 22, Username: "soha", TrustedHostKey: string(ssh.MarshalAuthorizedKey(signer.PublicKey())), CredentialKind: "ssh_keypair", Cols: 80, Rows: 24, ExpiresAt: time.Now().Add(time.Minute), Lease: sohaapi.SecretLeaseGrant{ID: "lease-one", Token: "test-lease-token-long", ExpiresAt: time.Now().Add(30 * time.Second)}}
	if err := ValidateSSHRequest(cfg, valid); err != nil {
		t.Fatal(err)
	}
	tests := map[string]func(*sohaapi.AgentSSHSessionRequest){"other agent": func(v *sohaapi.AgentSSHSessionRequest) { v.AgentID = "agent-two" }, "other control plane": func(v *sohaapi.AgentSSHSessionRequest) { v.ControlPlaneEndpoint = "https://attacker.example" }, "loopback": func(v *sohaapi.AgentSSHSessionRequest) { v.Address = "127.0.0.1" }, "metadata endpoint": func(v *sohaapi.AgentSSHSessionRequest) { v.Address = "169.254.169.254" }, "expired": func(v *sohaapi.AgentSSHSessionRequest) { v.ExpiresAt = time.Now().Add(-time.Second) }, "unbounded": func(v *sohaapi.AgentSSHSessionRequest) { v.ExpiresAt = time.Now().Add(time.Hour) }, "root": func(v *sohaapi.AgentSSHSessionRequest) { v.Username = "root" }, "host key options": func(v *sohaapi.AgentSSHSessionRequest) { v.TrustedHostKey = "command=\"bad\" " + v.TrustedHostKey }}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			input := valid
			mutate(&input)
			if ValidateSSHRequest(cfg, input) == nil {
				t.Fatal("invalid session accepted")
			}
		})
	}
}

func TestSSHHostKeyAndPTY(t *testing.T) {
	for _, mismatch := range []bool{false, true} {
		t.Run(strconv.FormatBool(mismatch), func(t *testing.T) {
			signer := testSSHSigner(t)
			otherKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			otherSigner, err := ssh.NewSignerFromKey(otherKey)
			if err != nil {
				t.Fatal(err)
			}
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = listener.Close() }()
			done := make(chan struct{})
			resized := make(chan struct{}, 1)
			go func() {
				defer close(done)
				connection, err := listener.Accept()
				if err != nil {
					return
				}
				defer func() { _ = connection.Close() }()
				_ = connection.SetDeadline(time.Now().Add(5 * time.Second))
				config := &ssh.ServerConfig{PasswordCallback: func(_ ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) { return &ssh.Permissions{}, nil }}
				config.AddHostKey(otherSigner)
				config.AddHostKey(signer)
				server, channels, requests, err := ssh.NewServerConn(connection, config)
				if err != nil {
					return
				}
				defer func() { _ = server.Close() }()
				go ssh.DiscardRequests(requests)
				for incoming := range channels {
					channel, requests, err := incoming.Accept()
					if err != nil {
						return
					}
					for request := range requests {
						switch request.Type {
						case "pty-req":
							_ = request.Reply(true, nil)
						case "window-change":
							resized <- struct{}{}
						case "shell":
							_ = request.Reply(true, nil)
							go func() {
								buffer := make([]byte, 5)
								_, _ = io.ReadFull(channel, buffer)
								_, _ = channel.Write(buffer)
								_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
								_ = channel.Close()
							}()
						}
					}
				}
			}()
			host, port, _ := net.SplitHostPort(listener.Addr().String())
			portNumber, _ := strconv.Atoi(port)
			key := signer.PublicKey()
			if mismatch {
				key = testSSHSigner(t).PublicKey()
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			client, err := dialSSH(ctx, sohaapi.AgentSSHSessionRequest{Address: host, Port: portNumber, Username: "soha", TrustedHostKey: string(ssh.MarshalAuthorizedKey(key))}, ssh.Password("test-only"))
			if mismatch {
				if err == nil {
					_ = client.Close()
					t.Fatal("host key mismatch accepted")
				}
				<-done
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			pty, err := newSSHPTY(ctx, client, 80, 24)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = pty.Close() }()
			if err = pty.Resize(100, 30); err != nil {
				t.Fatal(err)
			}
			select {
			case <-resized:
			case <-ctx.Done():
				t.Fatal("resize not received")
			}
			if _, err = io.WriteString(pty.Input, "hello"); err != nil {
				t.Fatal(err)
			}
			output, err := io.ReadAll(pty.Output)
			if err != nil || string(output) != "hello" {
				t.Fatal("PTY output failed", err)
			}
			if err = pty.Wait(); err != nil {
				t.Fatal(err)
			}
			_ = pty.Close()
			<-done
		})
	}
}
