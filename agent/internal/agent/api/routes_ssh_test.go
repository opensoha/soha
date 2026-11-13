package api

import (
	"context"
	"testing"
	"time"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

func TestSSHSessionOneUseCapacityAndShutdown(t *testing.T) {
	sessions := &sshSessions{pending: map[string]sshStagedSession{}, active: map[string]context.CancelFunc{}, limit: 1}
	input := sohaapi.AgentSSHSessionRequest{SessionID: "one", ExpiresAt: time.Now().Add(time.Minute)}
	if !sessions.stage(input) || sessions.stage(input) {
		t.Fatal("duplicate staged session")
	}
	second := input
	second.SessionID = "two"
	if sessions.stage(second) {
		t.Fatal("capacity exceeded")
	}
	_, ctx, release, ok := sessions.claim(context.Background(), "one")
	if !ok {
		t.Fatal("staged session unavailable")
	}
	defer release()
	if _, _, _, ok = sessions.claim(context.Background(), "one"); ok {
		t.Fatal("session replay accepted")
	}
	sessions.Close()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("shutdown did not cancel active session")
	}
	if sessions.stage(second) {
		t.Fatal("session staged after shutdown")
	}
}
