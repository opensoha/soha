// Package personalguest owns authenticated VM liveness only; it grants no execution or data readiness.
package personalguest

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"time"

	"github.com/opensoha/soha-agent/internal/agent/config"
)

func guestHTTPClient(cfg config.PersonalGuestConfig) (*http.Client, error) {
	return guestHTTPClientWithCertificate(cfg, "")
}

func guestHTTPClientWithCertificate(cfg config.PersonalGuestConfig, certificate string) (*http.Client, error) {
	certificatePEM, err := config.ReadPrivateFile(cfg.CertificateFile, 8192)
	if certificate != "" {
		certificatePEM, err = []byte(certificate), nil
	}
	if err != nil {
		return nil, err
	}
	keyPEM, err := config.ReadPrivateFile(cfg.KeyFile, 8192)
	if err != nil {
		return nil, err
	}
	pair, err := tls.X509KeyPair(certificatePEM, keyPEM)
	if err != nil {
		return nil, errors.New("personal guest certificate and key do not match")
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil || leaf.IsCA || len(leaf.URIs) != 1 || leaf.URIs[0].String() != "spiffe://opensoha.local/personal-guest/guest/"+cfg.BindingID || len(leaf.ExtKeyUsage) != 1 || leaf.ExtKeyUsage[0] != x509.ExtKeyUsageClientAuth {
		return nil, errors.New("personal guest certificate has the wrong scope")
	}
	ca, err := config.ReadPrivateFile(cfg.CAFile, 8192)
	if err != nil {
		return nil, err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		return nil, errors.New("personal guest CA is invalid")
	}
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
		return nil, errors.New("personal guest certificate is not currently trusted")
	}
	transport := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, Certificates: []tls.Certificate{pair}},
		MaxConnsPerHost: 1, MaxIdleConnsPerHost: 1, ResponseHeaderTimeout: 10 * time.Second, TLSHandshakeTimeout: 5 * time.Second}
	return &http.Client{Transport: transport, Timeout: 12 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}, nil
}
