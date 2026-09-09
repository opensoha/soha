package networkidentity

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"strings"

	"github.com/opensoha/soha/internal/platform/securefile"
)

const maxPEMBytes = 1 << 20

func LoadServerTLS(certFile, keyFile, clientCAFile string) (*tls.Config, error) {
	certificatePEM, err := securefile.Read(certFile, maxPEMBytes, false)
	if err != nil {
		return nil, fmt.Errorf("read server certificate: %w", err)
	}
	keyPEM, err := securefile.Read(keyFile, maxPEMBytes, true)
	if err != nil {
		return nil, fmt.Errorf("read server key: %w", err)
	}
	certificate, err := tls.X509KeyPair(certificatePEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("load server certificate: %w", err)
	}
	clientCA, err := securefile.Read(clientCAFile, maxPEMBytes, false)
	if err != nil {
		return nil, fmt.Errorf("read client CA: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(clientCA) {
		return nil, fmt.Errorf("client CA file contains no certificates")
	}
	return &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{certificate},
		ClientCAs:    pool,
		// Health probes may connect without a certificate. Every business route
		// still requires and scopes a verified certificate in its handler chain.
		ClientAuth: tls.VerifyClientCertIfGiven,
	}, nil
}

func LoadClientTLS(certFile, keyFile, caFile, serverName string) (*tls.Config, error) {
	certificatePEM, err := securefile.Read(certFile, maxPEMBytes, false)
	if err != nil {
		return nil, fmt.Errorf("read client certificate: %w", err)
	}
	keyPEM, err := securefile.Read(keyFile, maxPEMBytes, true)
	if err != nil {
		return nil, fmt.Errorf("read client key: %w", err)
	}
	certificate, err := tls.X509KeyPair(certificatePEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("load client certificate: %w", err)
	}
	caPEM, err := securefile.Read(caFile, maxPEMBytes, false)
	if err != nil {
		return nil, fmt.Errorf("read server CA: %w", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("server CA file contains no certificates")
	}
	return &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, Certificates: []tls.Certificate{certificate}, ServerName: strings.TrimSpace(serverName)}, nil
}
