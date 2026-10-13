package personalguest

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	guest "github.com/opensoha/soha-contracts/agent/guest"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type Issuer struct {
	certificate    *x509.Certificate
	key            crypto.Signer
	caPEM          string
	CertificateTTL time.Duration
}

func LoadIssuer(caFile, keyFile string) (*Issuer, error) {
	ca, err := readProtectedFile(caFile, 0022, 8192)
	if err != nil || len(ca) > 8192 {
		return nil, fmt.Errorf("load dedicated guest CA: invalid certificate file")
	}
	block, rest := pem.Decode(ca)
	if block == nil || block.Type != "CERTIFICATE" || len(bytes.TrimSpace(rest)) != 0 {
		return nil, fmt.Errorf("dedicated guest CA must contain one certificate")
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil || !certificate.IsCA || certificate.KeyUsage&x509.KeyUsageCertSign == 0 || time.Now().Before(certificate.NotBefore) || !time.Now().Before(certificate.NotAfter) {
		return nil, fmt.Errorf("dedicated guest CA is not currently valid for signing")
	}
	keyPEM, err := readPrivateKey(keyFile)
	if err != nil {
		return nil, err
	}
	pair, err := tls.X509KeyPair(ca, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("dedicated guest issuer certificate and key do not match")
	}
	key, ok := pair.PrivateKey.(crypto.Signer)
	if !ok {
		return nil, fmt.Errorf("dedicated guest issuer key cannot sign")
	}
	return &Issuer{certificate: certificate, key: key, caPEM: string(ca)}, nil
}

func readPrivateKey(name string) ([]byte, error) {
	if !filepath.IsAbs(name) {
		return nil, fmt.Errorf("guest issuer private key must use an absolute protected path")
	}
	return readProtectedFile(name, 0077, 8192)
}

func (i *Issuer) Issue(ctx context.Context, binding guest.Binding, encoded string) (string, string, string, error) {
	if err := ctx.Err(); err != nil {
		return "", "", "", err
	}
	if binding.Validate() != nil || binding.IssuedAt.Before(i.certificate.NotBefore) || binding.ExpiresAt.After(i.certificate.NotAfter) {
		return "", "", "", apperrors.ErrInvalidArgument
	}
	public, err := guestPublicKey(encoded)
	if err != nil {
		return "", "", "", err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return "", "", "", fmt.Errorf("generate guest certificate serial: %w", err)
	}
	serial.Add(serial, big.NewInt(1))
	identity := &url.URL{Scheme: "spiffe", Host: "opensoha.local", Path: "/personal-guest/guest/" + binding.ID}
	leaf := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "personal-guest"},
		NotBefore: binding.IssuedAt, NotAfter: binding.ExpiresAt, KeyUsage: x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, URIs: []*url.URL{identity}, BasicConstraintsValid: true}
	if i.CertificateTTL > 0 && leaf.NotAfter.After(binding.IssuedAt.Add(i.CertificateTTL)) {
		leaf.NotAfter = binding.IssuedAt.Add(i.CertificateTTL)
	}
	der, err := x509.CreateCertificate(rand.Reader, leaf, i.certificate, public, i.key)
	if err != nil {
		return "", "", "", fmt.Errorf("sign dedicated personal guest certificate: %w", err)
	}
	digest := sha256.Sum256(der)
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), i.caPEM, fmt.Sprintf("sha256:%x", digest), nil
}

func guestPublicKey(encoded string) (ed25519.PublicKey, error) {
	if len(encoded) > 4096 {
		return nil, apperrors.ErrInvalidArgument
	}
	block, rest := pem.Decode([]byte(encoded))
	if block == nil || block.Type != "CERTIFICATE REQUEST" || strings.TrimSpace(string(rest)) != "" {
		return nil, apperrors.ErrInvalidArgument
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil || csr.CheckSignature() != nil {
		return nil, apperrors.ErrInvalidArgument
	}
	key, ok := csr.PublicKey.(ed25519.PublicKey)
	if !ok || len(key) != ed25519.PublicKeySize {
		return nil, apperrors.ErrInvalidArgument
	}
	// CSR names/extensions are untrusted. Only Core's frozen guest URI enters the leaf.
	return key, nil
}
