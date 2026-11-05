package runtime

import (
	"bytes"
	"crypto/x509"
	"encoding/pem"
	"time"

	cr "github.com/opensoha/soha-contracts/resource"
)

// SecretCertificateMetadata reads only tls.crt, never a private key. Both the
// direct and Agent adapters use this bounded projection of the first certificate.
func SecretCertificateMetadata(name, namespace, secretType string, certificatePEM []byte) cr.SecretCertificateView {
	view := cr.SecretCertificateView{Name: name, Namespace: namespace, Status: "unsupported_type"}
	if secretType != "kubernetes.io/tls" {
		return view
	}
	view.Status = "missing"
	if len(certificatePEM) == 0 {
		return view
	}
	view.Status = "invalid"
	if len(certificatePEM) > 1<<20 {
		return view
	}
	block, _ := pem.Decode(certificatePEM)
	if block == nil || block.Type != "CERTIFICATE" {
		return view
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return view
	}
	view.Status = "available"
	view.NotBefore = certificate.NotBefore.UTC().Format(time.RFC3339)
	view.NotAfter = certificate.NotAfter.UTC().Format(time.RFC3339)
	selfSigned := bytes.Equal(certificate.RawIssuer, certificate.RawSubject) &&
		certificate.CheckSignature(certificate.SignatureAlgorithm, certificate.RawTBSCertificate, certificate.Signature) == nil
	view.SelfSigned = &selfSigned
	return view
}
