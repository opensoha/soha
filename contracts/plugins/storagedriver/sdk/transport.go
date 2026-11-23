package sdk

import (
	"bytes"
	"context"
	"crypto"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
	"unicode/utf8"

	"github.com/opensoha/soha/contracts/plugins/storagedriver"
)

func listen(cfg Bootstrap) (net.Listener, error) {
	if cfg.Network == "tcp" {
		cert, err := tlsCertificate(cfg.TLS)
		if err != nil {
			return nil, err
		}
		listener, err := net.Listen("tcp", cfg.Address)
		if err != nil {
			return nil, ErrTransport
		}
		tlsCfg := tlsConfig()
		tlsCfg.Certificates = []tls.Certificate{cert}
		return tls.NewListener(listener, tlsCfg), nil
	}
	parent, err := os.Lstat(filepath.Dir(cfg.Address))
	if err != nil || !parent.IsDir() || parent.Mode().Perm()&0o007 != 0 {
		return nil, ErrBootstrap
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: cfg.Address, Net: "unix"})
	if err != nil {
		return nil, ErrTransport
	}
	mode := os.FileMode(cfg.SocketMode)
	if mode == 0 {
		mode = 0o600
	}
	if err := os.Chmod(cfg.Address, mode); err != nil {
		_ = listener.Close()
		return nil, ErrTransport
	}
	return listener, nil
}

func tlsCertificate(cfg *TLSBootstrap) (tls.Certificate, error) {
	leaf, err := x509.ParseCertificate(cfg.CertificateDER)
	if err != nil {
		return tls.Certificate{}, ErrBootstrap
	}
	key, err := x509.ParsePKCS8PrivateKey(cfg.PrivateKeyDER)
	if err != nil {
		return tls.Certificate{}, ErrBootstrap
	}
	signer, ok := key.(crypto.Signer)
	if !ok {
		return tls.Certificate{}, ErrBootstrap
	}
	keyPublic, err := x509.MarshalPKIXPublicKey(signer.Public())
	if err != nil || !bytes.Equal(keyPublic, leaf.RawSubjectPublicKeyInfo) {
		return tls.Certificate{}, ErrBootstrap
	}
	return tls.Certificate{Certificate: [][]byte{cfg.CertificateDER}, PrivateKey: signer, Leaf: leaf}, nil
}

func tlsConfig() *tls.Config {
	return &tls.Config{
		MinVersion: tls.VersionTLS13,
		MaxVersion: tls.VersionTLS13,
		NextProtos: []string{"http/1.1"},
	}
}

func http1() *http.Protocols {
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	return protocols
}

func clientTransport(cfg ClientOptions) *http.Transport {
	dialer := net.Dialer{}
	network, address := cfg.Network, cfg.Address
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, address)
		},
		Protocols:              http1(),
		DisableCompression:     true,
		DisableKeepAlives:      true, // A reused HTTP/1.1 connection can transparently replay GET.
		MaxResponseHeaderBytes: storagedriver.MaxHeaderBytes,
		TLSHandshakeTimeout:    5 * time.Second,
	}
	if cfg.Network == "tcp" {
		pin := cfg.TLSLeafSHA256
		tlsCfg := tlsConfig()
		// The supervisor provisions and pins the exact ephemeral certificate.
		tlsCfg.InsecureSkipVerify = true // VerifyConnection performs mandatory pin verification.
		tlsCfg.VerifyConnection = func(state tls.ConnectionState) error {
			if len(state.PeerCertificates) == 0 {
				return ErrTransport
			}
			actual := sha256.Sum256(state.PeerCertificates[0].Raw)
			if subtle.ConstantTimeCompare(pin[:], actual[:]) != 1 {
				return ErrTransport
			}
			return nil
		}
		transport.TLSClientConfig = tlsCfg
	}
	return transport
}

func readJSON(input io.Reader, limit int64, target any) error {
	raw, err := io.ReadAll(io.LimitReader(input, limit+1))
	defer clear(raw)
	if err != nil {
		return rpcError(storagedriver.CodeInvalidRequest)
	}
	if int64(len(raw)) > limit {
		return rpcError(storagedriver.CodeTooLarge)
	}
	value := bytes.TrimSpace(raw)
	if len(value) == 0 || value[0] != '{' || !utf8.Valid(raw) || !json.Valid(raw) {
		return rpcError(storagedriver.CodeInvalidRequest)
	}
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return rpcError(storagedriver.CodeInvalidRequest)
	}
	return nil
}

func rpcError(code storagedriver.ErrorCode) storagedriver.RPCError {
	return storagedriver.RPCError{Code: code}
}

func errorCode(err error, fallback storagedriver.ErrorCode) storagedriver.ErrorCode {
	var value storagedriver.RPCError
	var ptr *storagedriver.RPCError
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return storagedriver.CodeDeadlineExceeded
	case errors.Is(err, context.Canceled):
		return storagedriver.CodeUnavailable
	case errors.As(err, &value) && value.Code.HTTPStatus() != 0:
		return value.Code
	case errors.As(err, &ptr) && ptr != nil && ptr.Code.HTTPStatus() != 0:
		return ptr.Code
	default:
		return fallback
	}
}
