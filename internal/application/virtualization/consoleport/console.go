package consoleport

import (
	"context"

	domainvirtualization "github.com/opensoha/soha/internal/domain/virtualization"
)

// Adapter is the application-owned port for opening a provider console.
type Adapter interface {
	GetConsoleURL(context.Context, domainvirtualization.AdapterConnection, domainvirtualization.AdapterVM) (ConsoleURLResult, error)
}

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
