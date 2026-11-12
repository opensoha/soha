package delivery

import (
	"context"
	"errors"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"testing"
)

func TestProxyDeploymentPrivateContextsAreMutuallyExclusive(t *testing.T) {
	proxy := ExecutionTask{TaskKind: "proxy_deployment_control", ProviderKind: "proxy_deployment.exec", ClaimedByAgentID: "exec"}
	outpost := ExecutionTask{TaskKind: "outpost_runtime_control", ProviderKind: "outpost_runtime.exec", ClaimedByAgentID: "exec"}
	generic := ExecutionTask{TaskKind: "release", ProviderKind: "ci_agent_runner"}
	for _, tc := range []struct {
		name    string
		ctx     context.Context
		task    ExecutionTask
		allowed bool
	}{{"proxy owns proxy", WithProxyDeploymentClaim(context.Background(), "exec"), proxy, true}, {"outpost owns outpost", WithOutpostRuntimeClaim(context.Background(), "exec"), outpost, true}, {"generic denied proxy", context.Background(), proxy, false}, {"generic denied outpost", context.Background(), outpost, false}, {"outpost denied proxy", WithOutpostRuntimeClaim(context.Background(), "exec"), proxy, false}, {"proxy denied outpost", WithProxyDeploymentClaim(context.Background(), "exec"), outpost, false}, {"proxy denied generic", WithProxyDeploymentClaim(context.Background(), "exec"), generic, false}, {"generic allowed own", context.Background(), generic, true}} {
		t.Run(tc.name, func(t *testing.T) {
			err := AuthorizePrivateRuntimeTask(tc.ctx, tc.task)
			if tc.allowed && err != nil || !tc.allowed && !errors.Is(err, apperrors.ErrAccessDenied) {
				t.Fatal(err)
			}
		})
	}
}
