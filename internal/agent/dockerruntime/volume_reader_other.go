//go:build !linux

package dockerruntime

import (
	"context"
	"errors"

	protocol "github.com/opensoha/soha-contracts/docker/runtime"
)

func readContainerVolume(context.Context, protocol.ReadRequest) (protocol.ReadResult, error) {
	return protocol.ReadResult{}, errors.New("safe volume reader requires Linux openat2 and mount identity support")
}
