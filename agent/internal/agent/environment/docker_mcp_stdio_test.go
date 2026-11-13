package environment

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestPersonalMCPStdioKeepsCommandFenceUntilParentStopConfirmed(t *testing.T) {
	for _, test := range []struct {
		name                        string
		stopErr                     error
		dispatched, supervisorOnly  bool
		parentRemains, fenceRemains bool
		replaced                    bool
		status                      PersonalMCPStdioStatus
	}{
		{name: "graceful exit", dispatched: true, supervisorOnly: true, parentRemains: true, status: PersonalMCPStdioClosed},
		{name: "canceled command confirmed stopped", dispatched: true, status: PersonalMCPStdioStopped},
		{name: "original absent with replacement running", dispatched: true, replaced: true, parentRemains: true, status: PersonalMCPStdioStopped},
		{name: "refused before dispatch", parentRemains: true, status: PersonalMCPStdioUnavailable},
		{name: "unconfirmed stop", dispatched: true, stopErr: errors.New("lost stop receipt"), parentRemains: true, fenceRemains: true, status: PersonalMCPStdioUnknown},
	} {
		t.Run(test.name, func(t *testing.T) {
			record, handle := personalDockerRecord(personalDockerRequest("owner", "lease"))
			fixture := &dockerCommandFixture{parent: record, before: func(args []string) error {
				if args[0] == "container" && args[1] == "rm" && args[len(args)-1] == handle.ID {
					return test.stopErr
				}
				return nil
			}}
			executor, err := NewDockerExecutor(testPersonalImage, testQuotaImage)
			if err != nil {
				t.Fatal(err)
			}
			executor.run = fixture.run
			ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
			defer cancel()
			fence, err := executor.acquireCommandFence(ctx, handle)
			if err != nil {
				t.Fatal(err)
			}
			if test.replaced {
				fixture.parent, _ = personalDockerRecord(personalDockerRequest("owner", "new-lease"))
				fixture.parent.ID = strings.Repeat("d", 64)
			}
			status := executor.finishPersonalMCPStdio(fence, test.dispatched, test.supervisorOnly)
			if status != test.status || (fixture.parent.ID != "") != test.parentRemains || (fixture.fence.ID != "") != test.fenceRemains {
				t.Fatalf("cleanup=%v parent=%q fence=%q", status, fixture.parent.ID, fixture.fence.ID)
			}
		})
	}
}
