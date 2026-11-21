package appgateway

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/opensoha/soha/internal/platform/securefile"
)

const (
	maxSecretFileBytes = 8 << 10
	maxTLSPEMFileBytes = 256 << 10
)

type secretProvider struct {
	tokenFile        string
	tokenFallback    string
	cookieSecretFile string
	cookieFallback   string
	s3AccessKeyFile  string
	s3SecretKeyFile  string
	s3AccessFallback string
	s3SecretFallback string
	tlsCertFile      string
	tlsKeyFile       string

	tlsMu     sync.Mutex
	tlsDigest [sha256.Size]byte
	tlsCached *tls.Certificate
}

func newSecretProvider(cfg Config) *secretProvider {
	return &secretProvider{
		tokenFile:        strings.TrimSpace(cfg.TokenFile),
		tokenFallback:    strings.TrimSpace(cfg.Token),
		cookieSecretFile: strings.TrimSpace(cfg.CookieSecretFile),
		cookieFallback:   strings.TrimSpace(cfg.CookieSecret),
		s3AccessKeyFile:  strings.TrimSpace(cfg.S3AccessKeyFile),
		s3SecretKeyFile:  strings.TrimSpace(cfg.S3SecretKeyFile),
		s3AccessFallback: strings.TrimSpace(cfg.S3AccessKeyID),
		s3SecretFallback: strings.TrimSpace(cfg.S3SecretAccessKey),
		tlsCertFile:      strings.TrimSpace(cfg.TLSCertFile),
		tlsKeyFile:       strings.TrimSpace(cfg.TLSKeyFile),
	}
}

func (p *secretProvider) GatewayToken() (string, error) {
	token, err := readFileOrFallback(p.tokenFile, p.tokenFallback, "gateway token")
	if err != nil {
		return "", err
	}
	if token == "" {
		return "", errors.New("gateway token is required")
	}
	return token, nil
}

func (p *secretProvider) CookieSecret() ([]byte, error) {
	secret, err := readFileOrFallback(p.cookieSecretFile, p.cookieFallback, "cookie secret")
	if err != nil {
		return nil, err
	}
	if len(secret) < MinCookieSecretBytes {
		return nil, fmt.Errorf("cookie secret must be at least %d bytes", MinCookieSecretBytes)
	}
	return []byte(secret), nil
}

func (p *secretProvider) S3Credentials() (string, string, error) {
	accessKey, err := readFileOrFallback(p.s3AccessKeyFile, p.s3AccessFallback, "S3 access key")
	if err != nil {
		return "", "", err
	}
	secretKey, err := readFileOrFallback(p.s3SecretKeyFile, p.s3SecretFallback, "S3 secret key")
	if err != nil {
		return "", "", err
	}
	if accessKey == "" || secretKey == "" {
		return "", "", errors.New("S3 access key and secret key are required")
	}
	return accessKey, secretKey, nil
}

func (p *secretProvider) LoadTLSCertificate() (*tls.Certificate, error) {
	if p.tlsCertFile == "" || p.tlsKeyFile == "" {
		return nil, errors.New("TLS certificate and key files are not configured")
	}
	certPEM, err := securefile.Read(p.tlsCertFile, maxTLSPEMFileBytes, false)
	if err != nil {
		return nil, fmt.Errorf("read TLS certificate file: %w", err)
	}
	keyPEM, err := securefile.Read(p.tlsKeyFile, maxTLSPEMFileBytes, false)
	if err != nil {
		return nil, fmt.Errorf("read TLS key file: %w", err)
	}
	var combined bytes.Buffer
	combined.Write(certPEM)
	combined.WriteByte(0)
	combined.Write(keyPEM)
	digest := sha256.Sum256(combined.Bytes())

	p.tlsMu.Lock()
	defer p.tlsMu.Unlock()
	if p.tlsCached != nil && p.tlsDigest == digest {
		return p.tlsCached, nil
	}
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("parse TLS key pair: %w", err)
	}
	p.tlsDigest = digest
	p.tlsCached = &pair
	return &pair, nil
}

func (p *secretProvider) ValidateAll(requireS3 bool) error {
	if _, err := p.GatewayToken(); err != nil {
		return err
	}
	if _, err := p.CookieSecret(); err != nil {
		return err
	}
	if requireS3 {
		if _, _, err := p.S3Credentials(); err != nil {
			return err
		}
	}
	if p.tlsCertFile != "" || p.tlsKeyFile != "" {
		if _, err := p.LoadTLSCertificate(); err != nil {
			return err
		}
	}
	return nil
}

func readFileOrFallback(filePath, fallback, label string) (string, error) {
	if filePath != "" {
		raw, err := securefile.Read(filePath, maxSecretFileBytes, false)
		if err != nil {
			return "", fmt.Errorf("read %s file: %w", label, err)
		}
		trimmed := strings.TrimSpace(string(raw))
		if trimmed == "" {
			return "", fmt.Errorf("%s file is empty", label)
		}
		return trimmed, nil
	}
	return strings.TrimSpace(fallback), nil
}
