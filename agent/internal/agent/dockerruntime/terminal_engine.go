package dockerruntime

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker/go-connections/sockets"
	"github.com/docker/go-connections/tlsconfig"
	"github.com/moby/moby/client"
)

var errTerminalEngine = errors.New("docker native terminal daemon configuration is unavailable")

type terminalEngineEnvironment struct {
	host, apiVersion, context, certPath, tlsVerify, tlsEnabled string
}

// terminalEngine shares one immutable daemon connection configuration between
// the official SDK and the bounded typed exec inspection. SDK v0.6.2 discards
// ExitCode presence, so that single read needs the Engine's pointer-bearing DTO.
type terminalEngine struct {
	client    *client.Client
	inspect   *http.Client
	transport *http.Transport
	host      *url.URL
}

func newTerminalEngine() (*terminalEngine, error) {
	return terminalEngineFromEnvironment(terminalEngineEnvironment{host: os.Getenv("DOCKER_HOST"), apiVersion: os.Getenv("DOCKER_API_VERSION"), context: os.Getenv("DOCKER_CONTEXT"), certPath: os.Getenv("DOCKER_CERT_PATH"), tlsVerify: os.Getenv("DOCKER_TLS_VERIFY"), tlsEnabled: os.Getenv("DOCKER_TLS")})
}

func terminalEngineFromEnvironment(env terminalEngineEnvironment) (*terminalEngine, error) {
	if env.context != "" {
		return nil, errTerminalEngine
	} // Never silently choose a different CLI context's daemon.
	host := env.host
	if host == "" {
		host = client.DefaultDockerHost
	}
	// ParseHostURL normalizes away TCP URL userinfo/query. Reject those on the
	// original value before SDK normalization can hide an ambiguous endpoint.
	original, err := url.Parse(host)
	if err != nil || original.User != nil || original.RawQuery != "" || original.Fragment != "" {
		return nil, errTerminalEngine
	}
	parsed, err := client.ParseHostURL(host)
	if err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errTerminalEngine
	}
	switch parsed.Scheme {
	case "unix", "tcp":
	default:
		return nil, errTerminalEngine
	}
	transport, err := terminalEngineTransport(parsed, env)
	if err != nil {
		return nil, err
	}
	// TLS lives in the context-aware dialer. Keeping the HTTP transport's TLS
	// config empty also makes SDK hijack use this same bounded dialer, rather
	// than its context-insensitive tls.Dial fallback. No plaintext fallback.
	api, err := client.New(client.WithHost(host), client.WithHTTPClient(&http.Client{Transport: transport, Timeout: 5 * time.Second, CheckRedirect: client.CheckRedirect}), client.WithScheme("http"), client.WithAPIVersion(env.apiVersion))
	if err != nil {
		transport.CloseIdleConnections()
		return nil, errTerminalEngine
	}
	return &terminalEngine{client: api, inspect: &http.Client{Transport: transport, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, transport: transport, host: parsed}, nil
}

func terminalEngineTransport(host *url.URL, env terminalEngineEnvironment) (*http.Transport, error) {
	transport := &http.Transport{MaxIdleConns: 6, IdleConnTimeout: 30 * time.Second, ResponseHeaderTimeout: 5 * time.Second}
	if err := sockets.ConfigureTransport(transport, host.Scheme, host.Host); err != nil {
		return nil, errTerminalEngine
	}
	transport.Proxy = nil // A registered daemon endpoint is direct, never an inherited HTTP proxy.
	if host.Scheme != "tcp" {
		if env.certPath != "" || env.tlsVerify != "" || env.tlsEnabled != "" {
			return nil, errTerminalEngine
		}
		return transport, nil
	}
	if env.certPath == "" {
		if env.tlsVerify != "" || env.tlsEnabled != "" {
			return nil, errTerminalEngine
		}
		transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext(ctx, "tcp", host.Host)
		}
		return transport, nil
	}
	if !filepath.IsAbs(env.certPath) || strings.TrimSpace(env.tlsVerify) == "" {
		return nil, errTerminalEngine
	}
	config, err := tlsconfig.Client(tlsconfig.Options{CAFile: filepath.Join(env.certPath, "ca.pem"), CertFile: filepath.Join(env.certPath, "cert.pem"), KeyFile: filepath.Join(env.certPath, "key.pem"), MinVersion: tls.VersionTLS12})
	if err != nil {
		return nil, errTerminalEngine
	}
	dialer := &tls.Dialer{NetDialer: &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}, Config: config}
	transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
		return dialer.DialContext(ctx, "tcp", host.Host)
	}
	return transport, nil
}

func (engine *terminalEngine) Close() error {
	engine.transport.CloseIdleConnections()
	return engine.client.Close()
}
