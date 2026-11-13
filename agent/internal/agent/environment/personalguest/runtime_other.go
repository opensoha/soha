//go:build !linux

package personalguest

import (
	"context"
	"errors"
	guest "github.com/opensoha/soha-contracts/agent/guest"
)

type browserPipe struct{}

func (*browserPipe) close() {}

func loadRuntimePlan(string) (guest.ToolsPlan, error) {
	return guest.ToolsPlan{}, errors.New("personal VM runtime requires Linux")
}
func (b *runtimeBroker) execute(context.Context, guest.RuntimeOperation) (guest.RuntimeResult, error) {
	return guest.RuntimeResult{}, errors.New("personal VM runtime requires Linux")
}
func (b *runtimeBroker) markDispatched(guest.RuntimeOperation, string) error {
	return errors.New("personal VM runtime requires Linux")
}
