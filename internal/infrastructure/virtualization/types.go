package virtualization

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
)

var (
	ErrUnsupported = errors.New("virtualization adapter unsupported")
	ErrInvalid     = errors.New("virtualization adapter invalid input")
)

type Adapter interface {
	TestConnection(ctx context.Context, connection Connection) (ConnectionTestResult, error)
	SyncAssets(ctx context.Context, connection Connection) (AssetSyncResult, error)
	CreateVM(ctx context.Context, connection Connection, input CreateVMInput) (VM, error)
	PowerAction(ctx context.Context, connection Connection, vm VM, action PowerAction) (PowerActionResult, error)
	GetVMMetrics(ctx context.Context, connection Connection, vm VM, rangeMinutes, stepSeconds int) (VMMetricsResult, error)
	GetConsoleURL(ctx context.Context, connection Connection, vm VM) (ConsoleURLResult, error)
}

type Connection struct {
	ID                    string
	Name                  string
	Provider              string
	Mode                  string
	ClusterID             string
	Endpoint              string
	EncryptedCredential   []byte
	Credential            map[string]any
	Options               map[string]any
	BackendURL            string
	InsecureSkipTLSVerify bool
}

type ConnectionTestResult struct {
	Healthy bool
	Status  string
	Message string
}

type AssetSyncResult struct {
	Health AssetHealth
	Assets []Asset
}

type AssetHealth struct {
	Status  string
	Message string
}

type Asset struct {
	Type      string
	Name      string
	Namespace string
	Node      string
	Status    string
	Metadata  map[string]string
}

type CreateVMInput struct {
	Name             string
	Architecture     string
	Namespace        string
	Node             string
	CPU              int
	Memory           string
	BootImage        string
	DiskSize         string
	Network          string
	CloudInit        string
	StartAfterCreate bool
	TemplateID       string
	SourceMode       string
	SourceRef        string
	ProviderParams   map[string]any
}

type VM struct {
	ID        string
	Name      string
	Namespace string
	Node      string
	Status    string
	Metadata  map[string]string
}

type PowerAction string

const (
	PowerActionStart   PowerAction = "start"
	PowerActionStop    PowerAction = "stop"
	PowerActionRestart PowerAction = "restart"
	PowerActionDelete  PowerAction = "delete"
)

type PowerActionResult struct {
	Accepted bool
	Action   PowerAction
	Message  string
}

func invalidf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

func unsupportedf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrUnsupported, fmt.Sprintf(format, args...))
}

type MetricPoint struct {
	Timestamp int64   `json:"timestamp"`
	Value     float64 `json:"value"`
}

type MetricSeries struct {
	Key    string        `json:"key"`
	Label  string        `json:"label"`
	Unit   string        `json:"unit"`
	Points []MetricPoint `json:"points"`
}

type VMMetricsResult struct {
	Series  []MetricSeries `json:"series"`
	Message string         `json:"message,omitempty"`
	Ready   bool           `json:"ready"`
	Source  string         `json:"source,omitempty"`
}

type ConsoleURLResult struct {
	Type                  string      `json:"type"`
	URL                   string      `json:"url"`
	BackendURL            string      `json:"backendUrl,omitempty"`
	Token                 string      `json:"token,omitempty"`
	Message               string      `json:"message,omitempty"`
	Ready                 bool        `json:"ready"`
	Provider              string      `json:"provider,omitempty"`
	ProxyMode             string      `json:"proxyMode,omitempty"`
	BackendHeaders        http.Header `json:"-"`
	BackendTLSConfig      *tls.Config `json:"-"`
	InsecureSkipTLSVerify bool        `json:"-"`
}
