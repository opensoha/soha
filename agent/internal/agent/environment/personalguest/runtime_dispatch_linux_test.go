//go:build linux

package personalguest

import (
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	guest "github.com/opensoha/soha-contracts/agent/guest"
)

func TestRuntimeDispatchSurvivesBrokerRestart(t *testing.T) {
	b := &runtimeBroker{}
	b.cfg.StateFile = filepath.Join(runtimeTestDirectory(t), "state.json")
	op := guest.RuntimeOperation{OperationID: uuid.NewString()}
	if err := b.markDispatched(op, "sha256:original"); err != nil {
		t.Fatal(err)
	}
	restarted := &runtimeBroker{cfg: b.cfg}
	if restarted.markDispatched(op, "sha256:original") == nil {
		t.Fatal("replayed original dispatch after restart")
	}
	op.OperationID = uuid.NewString()
	if err := restarted.markDispatched(op, "sha256:next"); err != nil {
		t.Fatal(err)
	}
}
