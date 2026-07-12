package virtualization

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"

	"github.com/opensoha/soha/internal/application/virtualization/consoleport"
	domainvirtualization "github.com/opensoha/soha/internal/domain/virtualization"
)

var (
	ErrUnsupported = domainvirtualization.ErrUnsupported
	ErrInvalid     = domainvirtualization.ErrInvalid
)

type Adapter interface {
	domainvirtualization.Adapter
	consoleport.Adapter
}
type Connection = domainvirtualization.AdapterConnection
type ConnectionTestResult = domainvirtualization.ConnectionTestResult
type AssetSyncResult = domainvirtualization.AssetSyncResult
type AssetHealth = domainvirtualization.AssetHealth
type Asset = domainvirtualization.Asset
type CreateVMInput = domainvirtualization.AdapterCreateVMInput
type VM = domainvirtualization.AdapterVM
type PowerAction = domainvirtualization.PowerAction

const (
	PowerActionStart   = domainvirtualization.PowerActionStart
	PowerActionStop    = domainvirtualization.PowerActionStop
	PowerActionRestart = domainvirtualization.PowerActionRestart
	PowerActionDelete  = domainvirtualization.PowerActionDelete
)

type PowerActionResult = domainvirtualization.PowerActionResult
type AdapterError = domainvirtualization.AdapterError
type MetricPoint = domainvirtualization.MetricPoint
type MetricSeries = domainvirtualization.MetricSeries
type VMMetricsResult = domainvirtualization.VMMetricsResult
type ConsoleURLResult = consoleport.ConsoleURLResult
type BackendTLS = consoleport.BackendTLS

func ConsoleBackendHeaders(result ConsoleURLResult) http.Header {
	headers := make(http.Header, len(result.BackendHeaders))
	for name, values := range result.BackendHeaders {
		headers[name] = append([]string(nil), values...)
	}
	return headers
}

func ConsoleBackendTLSConfig(result ConsoleURLResult) (*tls.Config, error) {
	config := result.BackendTLS
	empty := config.ServerName == "" && !config.InsecureSkipVerify && len(config.CAData) == 0 &&
		len(config.CertData) == 0 && len(config.KeyData) == 0 && len(config.NextProtos) == 0
	if empty {
		return nil, nil
	}
	tlsConfig := &tls.Config{
		ServerName:         config.ServerName,
		InsecureSkipVerify: config.InsecureSkipVerify, //nolint:gosec // Explicit per-connection operator setting.
		NextProtos:         append([]string(nil), config.NextProtos...),
	}
	if len(config.CAData) > 0 {
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(config.CAData) {
			return nil, errors.New("virtualization backend TLS CA data is invalid")
		}
		tlsConfig.RootCAs = roots
	}
	if len(config.CertData) > 0 || len(config.KeyData) > 0 {
		if len(config.CertData) == 0 || len(config.KeyData) == 0 {
			return nil, errors.New("virtualization backend TLS client certificate and key must be provided together")
		}
		certificate, err := tls.X509KeyPair(config.CertData, config.KeyData)
		if err != nil {
			return nil, fmt.Errorf("parse virtualization backend TLS client certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{certificate}
	}
	return tlsConfig, nil
}

func AdapterErrorDetails(err error) (AdapterError, bool) {
	return domainvirtualization.AdapterErrorDetails(err)
}

func invalidf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}
