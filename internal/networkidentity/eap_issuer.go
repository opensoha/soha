package networkidentity

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/url"
	"time"

	"github.com/opensoha/soha/internal/platform/securefile"
)

type EAPIssuer struct {
	certificate *x509.Certificate
	key         crypto.Signer
	caPEM       string
}

func LoadEAPIssuer(caFile, keyFile, controlCAFile string) (*EAPIssuer, error) {
	ca, err := securefile.Read(caFile, 16384, false)
	if err != nil {
		return nil, fmt.Errorf("read dedicated EAP CA: %w", err)
	}
	block, rest := pem.Decode(ca)
	if block == nil || block.Type != "CERTIFICATE" || len(bytes.TrimSpace(rest)) != 0 {
		return nil, fmt.Errorf("dedicated EAP CA must contain one certificate")
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil || !certificate.IsCA || !certificate.BasicConstraintsValid || certificate.KeyUsage&x509.KeyUsageCertSign == 0 || len(certificate.SubjectKeyId) < 8 {
		return nil, fmt.Errorf("dedicated EAP CA is invalid for signing")
	}
	control, err := securefile.Read(controlCAFile, maxPEMBytes, false)
	if err != nil {
		return nil, fmt.Errorf("read control CA for EAP separation: %w", err)
	}
	for len(control) > 0 {
		entry, remaining := pem.Decode(control)
		if entry == nil {
			return nil, fmt.Errorf("control CA bundle is invalid")
		}
		if bytes.Equal(entry.Bytes, certificate.Raw) {
			return nil, fmt.Errorf("EAP issuer must be separate from the control CA")
		}
		controlCertificate, err := x509.ParseCertificate(entry.Bytes)
		if err != nil || bytes.Equal(controlCertificate.RawSubjectPublicKeyInfo, certificate.RawSubjectPublicKeyInfo) {
			return nil, fmt.Errorf("EAP issuer must use a dedicated key")
		}
		control = bytes.TrimSpace(remaining)
	}
	keyPEM, err := securefile.Read(keyFile, 16384, true)
	if err != nil {
		return nil, fmt.Errorf("read dedicated EAP issuer key: %w", err)
	}
	defer clear(keyPEM)
	pair, err := tls.X509KeyPair(ca, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("EAP issuer certificate and key do not match")
	}
	key, ok := pair.PrivateKey.(crypto.Signer)
	if !ok {
		return nil, fmt.Errorf("EAP issuer key cannot sign")
	}
	return &EAPIssuer{certificate: certificate, key: key, caPEM: string(ca)}, nil
}

func (i *EAPIssuer) Issue(ctx context.Context, deviceID, subjectID, encodedCSR string, now, expiresAt time.Time) (string, string, error) {
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	if !identityIDPattern.MatchString(deviceID) || !identityIDPattern.MatchString(subjectID) || now.Before(i.certificate.NotBefore) || !now.Before(i.certificate.NotAfter) {
		return "", "", fmt.Errorf("EAP certificate identity or issuer validity is invalid")
	}
	key, err := eapCSRPublicKey(encodedCSR)
	if err != nil {
		return "", "", err
	}
	expiresAt = minEAPExpiry(expiresAt, now.Add(24*time.Hour), i.certificate.NotAfter)
	if !expiresAt.After(now) {
		return "", "", fmt.Errorf("EAP certificate validity is empty")
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return "", "", fmt.Errorf("generate EAP serial: %w", err)
	}
	serial.Add(serial, big.NewInt(1))
	// CSR names and extensions never become authorization identities.
	leaf := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: subjectID},
		URIs:      []*url.URL{{Scheme: "spiffe", Host: "opensoha.local", Path: "/network-eap/endpoint/" + deviceID}},
		NotBefore: now, NotAfter: expiresAt, KeyUsage: x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, leaf, i.certificate, key, i.key)
	if err != nil {
		return "", "", fmt.Errorf("sign EAP certificate: %w", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), i.caPEM, nil
}

func eapCSRPublicKey(encoded string) (*ecdsa.PublicKey, error) {
	if len(encoded) > 4096 {
		return nil, fmt.Errorf("EAP CSR exceeds limit")
	}
	block, rest := pem.Decode([]byte(encoded))
	if block == nil || block.Type != "CERTIFICATE REQUEST" || len(bytes.TrimSpace(rest)) != 0 || len(block.Headers) != 0 {
		return nil, fmt.Errorf("EAP CSR must contain one certificate request")
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil || csr.CheckSignature() != nil {
		return nil, fmt.Errorf("EAP CSR signature is invalid")
	}
	key, ok := csr.PublicKey.(*ecdsa.PublicKey)
	if !ok || key.Curve != elliptic.P256() {
		return nil, fmt.Errorf("EAP CSR requires P-256")
	}
	return key, nil
}

func minEAPExpiry(values ...time.Time) time.Time {
	result := values[0]
	for _, value := range values[1:] {
		if value.Before(result) {
			result = value
		}
	}
	return result
}
