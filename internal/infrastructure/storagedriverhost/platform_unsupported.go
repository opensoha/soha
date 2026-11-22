//go:build !linux

package storagedriverhost

import (
	"context"
	"os"
	"time"

	"github.com/opensoha/soha/contracts/plugins/storagedriver/sdk"
)

func New(Config) (*Supervisor, error) { return nil, ErrUnsupported }

func LeaseManagedRuntime(string, map[uint32]uint32) (*ManagedRuntime, error) {
	return nil, ErrUnsupported
}

func (*ManagedRuntime) PrepareSlot(uint32) (string, func() error, error) {
	return "", nil, ErrUnsupported
}

func IsSandboxChild() bool { return false }

func RunSandboxChild() int { return 1 }

func takeRuntimeLock(string) (*os.File, error) { return nil, ErrUnsupported }

func (*Supervisor) leaseAccounts() (func(), error) { return nil, ErrUnsupported }

func (*Supervisor) launch(context.Context, *managedDriver) (*launch, error) {
	return nil, ErrUnsupported
}

type launch struct {
	owner     context.Context
	client    *sdk.Client
	exited    chan struct{}
	startedAt time.Time
	active    int
	drained   chan struct{}
}

func (*launch) stop(<-chan struct{}, <-chan struct{}, deadlines) error { return ErrUnsupported }

func (*launch) cleanup() error { return ErrUnsupported }
