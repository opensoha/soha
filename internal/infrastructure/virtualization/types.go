package virtualization

import (
	"context"
	"errors"
	"fmt"
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
}

type Connection struct {
	ID                  string
	Name                string
	Provider            string
	Mode                string
	ClusterID           string
	Endpoint            string
	EncryptedCredential []byte
	Credential          map[string]any
	Options             map[string]any
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
