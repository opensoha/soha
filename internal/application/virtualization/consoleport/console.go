package consoleport

import (
	"context"
	"io"

	domainvirtualization "github.com/opensoha/soha/internal/domain/virtualization"
)

// Adapter prepares a provider console for the authorized VM.
type Adapter interface {
	GetConsoleURL(context.Context, domainvirtualization.AdapterConnection, domainvirtualization.AdapterVM) (ConsoleURLResult, error)
}

// Dialer opens the prepared upstream console. The caller owns the stream after a successful dial.
type Dialer func(context.Context, ConsoleURLResult) (Stream, error)

type Stream interface {
	NextReader() (int, io.Reader, error)
	NextWriter(int) (io.WriteCloser, error)
	Close() error
}

// DialError identifies a handshake failure that is safe to report to the browser.
type DialError string

const (
	ErrUnsupportedConsole DialError = "VNC proxy not fully implemented for this provider"
	ErrInvalidBackendURL  DialError = "invalid backend url"
	ErrInvalidBackendTLS  DialError = "invalid backend TLS configuration"
	ErrBackendConnection  DialError = "backend connection failed"
	ErrInvalidKubeVirtTLS DialError = "invalid kubevirt backend TLS configuration"
	ErrKubeVirtConnection DialError = "kubevirt backend connection failed"
)

func (e DialError) Error() string { return string(e) }

type ConsoleURLResult struct {
	Type           string              `json:"type"`
	URL            string              `json:"url"`
	BackendURL     string              `json:"backendUrl,omitempty"`
	Token          string              `json:"token,omitempty"`
	Message        string              `json:"message,omitempty"`
	Ready          bool                `json:"ready"`
	Provider       string              `json:"provider,omitempty"`
	ProxyMode      string              `json:"proxyMode,omitempty"`
	BackendHeaders map[string][]string `json:"-"`
	BackendTLS     BackendTLS          `json:"-"`
}

type BackendTLS struct {
	ServerName         string   `json:"-"`
	InsecureSkipVerify bool     `json:"-"`
	CAData             []byte   `json:"-"`
	CertData           []byte   `json:"-"`
	KeyData            []byte   `json:"-"`
	NextProtos         []string `json:"-"`
}
