package runner

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"os"
	"path/filepath"
)

func hermesHTTPClient(base *http.Client, caFile string) (*http.Client, error) {
	client := *base
	client.Timeout = 0
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if caFile == "" {
		return &client, nil
	}
	info, err := os.Stat(caFile)
	if !filepath.IsAbs(caFile) || err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return nil, errors.New("invalid Hermes CA file")
	}
	root, err := os.OpenRoot(filepath.Dir(caFile))
	if err != nil {
		return nil, errors.New("cannot open Hermes CA directory")
	}
	defer func() { _ = root.Close() }()
	data, err := root.ReadFile(filepath.Base(caFile))
	if err != nil {
		return nil, errors.New("cannot load Hermes CA file")
	}
	roots, err := x509.SystemCertPool()
	if err != nil {
		return nil, errors.New("cannot load system certificate roots")
	}
	if !roots.AppendCertsFromPEM(data) {
		return nil, errors.New("invalid Hermes CA certificate")
	}
	transport, ok := client.Transport.(*http.Transport)
	if client.Transport == nil {
		transport, ok = http.DefaultTransport.(*http.Transport)
	}
	if !ok {
		return nil, errors.New("hermes custom CA requires an HTTP transport")
	}
	transport = transport.Clone()
	transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	// ponytail: private-CA clients do not pool across probes; cache transports per provider if needed.
	transport.DisableKeepAlives = true
	client.Transport = transport
	return &client, nil
}
