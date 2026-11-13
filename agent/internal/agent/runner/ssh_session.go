package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	cfgpkg "github.com/opensoha/soha-agent/internal/agent/config"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"golang.org/x/crypto/ssh"
)

type SSHPTY struct {
	stopContext  func() bool
	client       *ssh.Client
	session      *ssh.Session
	Input        io.WriteCloser
	Output       io.Reader
	outputWriter *io.PipeWriter
	waitDone     chan struct{}
	waitErr      error
}

func (pty *SSHPTY) Resize(cols, rows int) error {
	if cols < 1 || cols > 512 || rows < 1 || rows > 512 {
		return fmt.Errorf("invalid terminal dimensions")
	}
	return pty.session.WindowChange(rows, cols)
}
func (pty *SSHPTY) Wait() error { <-pty.waitDone; return pty.waitErr }
func (pty *SSHPTY) Close() error {
	if pty.stopContext != nil {
		pty.stopContext()
	}
	_ = pty.session.Close()
	_ = pty.outputWriter.Close()
	return pty.client.Close()
}

func ValidateSSHRequest(cfg cfgpkg.ControlPlaneConfig, input sohaapi.AgentSSHSessionRequest) error {
	address, err := netip.ParseAddr(input.Address)
	if err != nil || address.IsUnspecified() || address.IsLoopback() || address.IsLinkLocalUnicast() || address.IsMulticast() {
		return fmt.Errorf("invalid SSH address")
	}
	endpoint, err := url.Parse(cfg.BaseURL)
	if err != nil || endpoint.Host == "" || endpoint.User != nil || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || strings.TrimRight(input.ControlPlaneEndpoint, "/") != strings.TrimRight(cfg.BaseURL, "/") {
		return fmt.Errorf("untrusted control plane endpoint")
	}
	now := time.Now()
	if cfg.AgentID == "" || cfg.BearerToken == "" || input.AgentID != cfg.AgentID || input.ExecutionClusterID == "" || len(input.ExecutionClusterID) > 128 || input.SessionID == "" || len(input.SessionID) > 128 || input.Port < 1 || input.Port > 65535 || !sshUsernamePattern.MatchString(input.Username) || input.Username == "root" || input.Cols < 1 || input.Cols > 512 || input.Rows < 1 || input.Rows > 512 || !input.ExpiresAt.After(now) || input.ExpiresAt.After(now.Add(30*time.Minute+5*time.Second)) || !input.Lease.ExpiresAt.After(now) || input.Lease.ExpiresAt.After(input.ExpiresAt) || input.Lease.ID == "" || len(input.Lease.ID) > 128 || len(input.Lease.Token) < 16 || len(input.Lease.Token) > 256 {
		return fmt.Errorf("invalid SSH session binding")
	}
	if input.CredentialKind != "ssh_keypair" && input.CredentialKind != "ssh_password" {
		return fmt.Errorf("invalid SSH credential kind")
	}
	_, _, options, rest, err := ssh.ParseAuthorizedKey([]byte(input.TrustedHostKey))
	if err != nil || len(options) != 0 || strings.TrimSpace(string(rest)) != "" {
		return fmt.Errorf("invalid trusted host key")
	}
	return nil
}

var sshUsernamePattern = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)

func SSHExitCode(err error) *int {
	code := 0
	if err != nil {
		var exit *ssh.ExitError
		if !errors.As(err, &exit) {
			return nil
		}
		code = exit.ExitStatus()
		if code < 0 || code > 255 {
			return nil
		}
	}
	return &code
}

func OpenSSHPTY(ctx context.Context, cfg cfgpkg.ControlPlaneConfig, input sohaapi.AgentSSHSessionRequest) (*SSHPTY, error) {
	if err := ValidateSSHRequest(cfg, input); err != nil {
		return nil, err
	}
	auth, err := redeemSSHAuth(ctx, cfg, input)
	if err != nil {
		return nil, err
	}
	client, err := dialSSH(ctx, input, auth)
	if err != nil {
		return nil, err
	}
	return newSSHPTY(ctx, client, input.Cols, input.Rows)
}

func newSSHPTY(ctx context.Context, client *ssh.Client, cols, rows int) (*SSHPTY, error) {
	stopContext := context.AfterFunc(ctx, func() { _ = client.Close() })
	session, err := client.NewSession()
	if err != nil {
		stopContext()
		_ = client.Close()
		return nil, err
	}
	failed := true
	defer func() {
		if failed {
			stopContext()
			_ = session.Close()
			_ = client.Close()
		}
	}()
	stdin, err := session.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, outputWriter := io.Pipe()
	session.Stdout, session.Stderr = outputWriter, outputWriter
	// A PTY combines guest stdout/stderr without materializing credentials on disk.
	if err = session.RequestPty("xterm-256color", rows, cols, ssh.TerminalModes{}); err != nil {
		_ = stdout.Close()
		_ = outputWriter.Close()
		return nil, err
	}
	if err = session.Shell(); err != nil {
		_ = stdout.Close()
		_ = outputWriter.Close()
		return nil, err
	}
	failed = false
	pty := &SSHPTY{stopContext: stopContext, client: client, session: session, Input: stdin, Output: stdout, outputWriter: outputWriter, waitDone: make(chan struct{})}
	go func() { pty.waitErr = session.Wait(); _ = outputWriter.Close(); close(pty.waitDone) }()
	return pty, nil
}

func redeemSSHAuth(ctx context.Context, cfg cfgpkg.ControlPlaneConfig, input sohaapi.AgentSSHSessionRequest) (ssh.AuthMethod, error) {
	body, err := json.Marshal(sohaapi.VMSSHCredentialRedemptionInput{ClusterID: input.ExecutionClusterID, AgentID: cfg.AgentID, LeaseID: input.Lease.ID, Token: input.Lease.Token})
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(cfg.BaseURL, "/")+"/api/v1/virtualization/ssh-sessions/"+url.PathEscape(input.SessionID)+"/credentials", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+cfg.BearerToken)
	request.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("SSH credential redemption unavailable")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("SSH credential redemption denied")
	}
	var payload struct {
		Data sohaapi.SecretLeaseRedemption `json:"data"`
	}
	if err = json.NewDecoder(io.LimitReader(response.Body, 2*1024*1024)).Decode(&payload); err != nil || payload.Data.LeaseID != input.Lease.ID || !payload.Data.ExpiresAt.After(time.Now()) {
		return nil, fmt.Errorf("invalid SSH lease response")
	}
	value, ok := payload.Data.Values["SSH_AUTH"]
	if !ok {
		return nil, fmt.Errorf("SSH credential missing")
	}
	return sshAuth(input.CredentialKind, value)
}

func sshAuth(kind sohaapi.SecretKind, value string) (ssh.AuthMethod, error) {
	if kind == "ssh_password" {
		if value == "" || len(value) > 4096 {
			return nil, fmt.Errorf("invalid SSH password")
		}
		return ssh.Password(value), nil
	}
	var material struct {
		PrivateKey string `json:"privateKey"`
		Passphrase string `json:"passphrase,omitempty"`
	}
	if json.Unmarshal([]byte(value), &material) != nil {
		return nil, fmt.Errorf("invalid SSH keypair")
	}
	var signer ssh.Signer
	var err error
	if material.Passphrase == "" {
		signer, err = ssh.ParsePrivateKey([]byte(material.PrivateKey))
	} else {
		signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(material.PrivateKey), []byte(material.Passphrase))
	}
	if err != nil {
		return nil, fmt.Errorf("invalid SSH keypair")
	}
	return ssh.PublicKeys(signer), nil
}

func dialSSH(ctx context.Context, input sohaapi.AgentSSHSessionRequest, auth ssh.AuthMethod) (*ssh.Client, error) {
	hostKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(input.TrustedHostKey))
	if err != nil {
		return nil, err
	}
	endpoint := net.JoinHostPort(input.Address, strconv.Itoa(input.Port))
	connection, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", endpoint)
	if err != nil {
		return nil, fmt.Errorf("SSH connection unavailable")
	}
	deadline := time.Now().Add(10 * time.Second)
	_ = connection.SetDeadline(deadline)
	stop := context.AfterFunc(ctx, func() { _ = connection.Close() })
	defer stop()
	algorithms := []string{hostKey.Type()}
	if hostKey.Type() == ssh.KeyAlgoRSA {
		algorithms = []string{ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256}
	}
	sshConnection, channels, requests, err := ssh.NewClientConn(connection, endpoint, &ssh.ClientConfig{User: input.Username, Auth: []ssh.AuthMethod{auth}, HostKeyCallback: ssh.FixedHostKey(hostKey), HostKeyAlgorithms: algorithms, Timeout: 10 * time.Second})
	if err != nil {
		_ = connection.Close()
		return nil, fmt.Errorf("SSH authentication or host key verification failed")
	}
	_ = connection.SetDeadline(time.Time{})
	return ssh.NewClient(sshConnection, channels, requests), nil
}
