package virtualization

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrUnsupported = errors.New("virtualization adapter unsupported")
	ErrInvalid     = errors.New("virtualization adapter invalid input")
)

type Adapter interface {
	TestConnection(context.Context, AdapterConnection) (ConnectionTestResult, error)
	SyncAssets(context.Context, AdapterConnection) (AssetSyncResult, error)
	CreateVM(context.Context, AdapterConnection, AdapterCreateVMInput) (AdapterVM, error)
	PowerAction(context.Context, AdapterConnection, AdapterVM, PowerAction) (PowerActionResult, error)
	GetVMMetrics(context.Context, AdapterConnection, AdapterVM, int, int) (VMMetricsResult, error)
}

type AdapterConnection struct {
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
	Healthy    bool
	Status     string
	Message    string
	Reason     string
	HTTPStatus int
	NextAction string
}

type AssetSyncResult struct {
	Health AssetHealth
	Assets []Asset
}

type AssetHealth struct {
	Status     string
	Message    string
	Reason     string
	HTTPStatus int
	NextAction string
}

type Asset struct {
	Type      string
	Name      string
	Namespace string
	Node      string
	Status    string
	Metadata  map[string]string
}

type AdapterCreateVMInput struct {
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

type AdapterVM struct {
	ID          string
	Name        string
	Namespace   string
	Node        string
	Status      string
	IPAddresses []string
	Endpoint    string
	Metadata    map[string]string
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
	UPID     string
}

type AdapterError struct {
	Cause      error
	Reason     string
	Message    string
	HTTPStatus int
	NextAction string
}

func (e *AdapterError) Error() string {
	if e == nil {
		return ""
	}
	if strings.TrimSpace(e.Message) != "" {
		return e.Message
	}
	if e.HTTPStatus > 0 && strings.TrimSpace(e.Reason) != "" {
		return fmt.Sprintf("virtualization adapter failed: %s (status %d)", e.Reason, e.HTTPStatus)
	}
	if e.HTTPStatus > 0 {
		return fmt.Sprintf("virtualization adapter failed with status %d", e.HTTPStatus)
	}
	if strings.TrimSpace(e.Reason) != "" {
		return "virtualization adapter failed: " + e.Reason
	}
	return "virtualization adapter failed"
}

func (e *AdapterError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func AdapterErrorDetails(err error) (AdapterError, bool) {
	var adapterErr *AdapterError
	if !errors.As(err, &adapterErr) || adapterErr == nil {
		return AdapterError{}, false
	}
	return *adapterErr, true
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
