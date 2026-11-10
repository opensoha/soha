package resource

import (
	"context"
	"io"

	contractresource "github.com/opensoha/soha-contracts/resource"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
)

type PodTerminalRepository interface {
	Create(context.Context, domainresource.PodTerminalRecord) error
	Get(context.Context, string, string) (domainresource.PodTerminalRecord, error)
	Consume(context.Context, string, string, string, string) (domainresource.PodTerminalRecord, error)
	RequestClose(context.Context, string, string) (domainresource.PodTerminalRecord, error)
	Finish(context.Context, domainresource.PodTerminalRecord) error
	Cleanup(context.Context) error
}

type PodTerminalAgent interface {
	CheckPodTerminal(context.Context) error
	InspectPodExecutionTarget(context.Context, string, string, string, string) (contractresource.PodExecutionTarget, error)
	StreamNativePodTerminal(context.Context, string, string, contractresource.PodTerminalInput, io.ReadCloser, io.Writer, contractresource.NativeTerminalSizeQueue) (contractresource.PodTerminalResult, error)
}

type PodTerminalDirect interface {
	CheckPodTerminal(context.Context, string) error
	InspectPodExecutionTarget(context.Context, string, string, string, string, string) (contractresource.PodExecutionTarget, error)
	StreamNativePodTerminal(context.Context, string, string, string, contractresource.PodTerminalInput, io.ReadCloser, io.Writer, contractresource.NativeTerminalSizeQueue) (contractresource.PodTerminalResult, error)
}

// NativePodTerminalSession owns one admitted stream, including its persistence
// and shutdown fence. Closing it is mandatory even if WebSocket upgrade fails.
type NativePodTerminalSession interface {
	Context() context.Context
	Lease() contractresource.PodTerminalLease
	InitialSize() contractresource.NativeTerminalSize
	Stream(io.ReadCloser, io.Writer, contractresource.NativeTerminalSizeQueue) (contractresource.PodTerminalResult, error)
	Close() error
}
