// Package sdk serves and calls Soha storage drivers over a private local transport.
package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/opensoha/soha/contracts/plugins/storagedriver"
)

const (
	maxBootstrapBytes  = 64 << 10
	defaultCallTimeout = 5 * time.Minute
)

var (
	ErrBootstrap = errors.New("invalid storage driver bootstrap")
	ErrTransport = errors.New("storage driver transport failed")
	ErrProtocol  = errors.New("invalid storage driver RPC response")
)

// Bootstrap is the launch configuration conveyed through a private stdin pipe.
// It contains secret material and must never be logged or saved to disk.
type Bootstrap struct {
	Network      string        `json:"network"` // unix or tcp
	Address      string        `json:"address"`
	LaunchSecret []byte        `json:"launchSecret"`
	TLS          *TLSBootstrap `json:"tls,omitempty"`
	SocketMode   uint32        `json:"socketMode,omitempty"` // Unix only. Default 0600, optionally 0660.
}

// TLSBootstrap carries a fresh leaf certificate and its unencrypted PKCS#8 key in DER.
type TLSBootstrap struct {
	CertificateDER []byte `json:"certificateDER"`
	PrivateKeyDER  []byte `json:"privateKeyDER"`
}

// WriteBootstrap writes the startup configuration. The host closes the pipe afterward.
func WriteBootstrap(output io.Writer, cfg Bootstrap) error {
	if err := cfg.validate(); err != nil {
		return err
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return ErrBootstrap
	}
	defer clear(raw)
	if len(raw) > maxBootstrapBytes {
		return ErrBootstrap
	}
	if n, err := output.Write(raw); err != nil || n != len(raw) {
		return ErrTransport
	}
	return nil
}

func readBootstrap(ctx context.Context, input io.Reader) (Bootstrap, error) {
	if ctx.Err() != nil {
		return Bootstrap{}, ctx.Err()
	}
	if input == nil {
		return Bootstrap{}, ErrBootstrap
	}
	if closer, ok := input.(io.Closer); ok {
		stop := context.AfterFunc(ctx, func() { _ = closer.Close() })
		defer stop()
	}
	var cfg Bootstrap
	if err := readJSON(input, maxBootstrapBytes, &cfg); err != nil {
		cfg.erase()
		if ctx.Err() != nil {
			return Bootstrap{}, ctx.Err()
		}
		return Bootstrap{}, ErrBootstrap
	}
	if err := cfg.validate(); err != nil {
		cfg.erase()
		return Bootstrap{}, err
	}
	return cfg, nil
}

func (b Bootstrap) validate() error {
	if len(b.LaunchSecret) != storagedriver.LaunchSecretBytes || !validAddress(b.Network, b.Address) {
		return ErrBootstrap
	}
	if b.Network == "tcp" {
		if b.TLS == nil || len(b.TLS.CertificateDER) == 0 || len(b.TLS.PrivateKeyDER) == 0 || b.SocketMode != 0 ||
			len(b.TLS.CertificateDER)+len(b.TLS.PrivateKeyDER) > maxBootstrapBytes/2 {
			return ErrBootstrap
		}
	} else if b.TLS != nil || (b.SocketMode != 0 && b.SocketMode != 0o600 && b.SocketMode != 0o660) {
		return ErrBootstrap
	}
	return nil
}

func (b Bootstrap) erase() {
	clear(b.LaunchSecret)
	if b.TLS != nil {
		clear(b.TLS.PrivateKeyDER)
	}
}

func validAddress(network, address string) bool {
	switch network {
	case "unix":
		return len(address) <= 4096 && filepath.IsAbs(address) && filepath.Clean(address) == address &&
			filepath.Dir(address) != address && !strings.ContainsRune(address, 0)
	case "tcp":
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return false
		}
		ip := net.ParseIP(host)
		p, err := strconv.ParseUint(port, 10, 16)
		return ip != nil && ip.IsLoopback() && err == nil && p != 0
	default:
		return false
	}
}
