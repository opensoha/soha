package copilot

import (
	"context"
	"testing"

	appaigateway "github.com/opensoha/soha/internal/application/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type nativeIncompleteStreamGateway struct{ *nativeLoopGateway }

func (g *nativeIncompleteStreamGateway) InvokeWorkbenchModelStream(_ context.Context, _ domainidentity.Principal, _ appaigateway.WorkbenchRelayRequest, emit func(appaigateway.WorkbenchRelayStreamDelta) bool) (appaigateway.WorkbenchRelayResponse, error) {
	emit(appaigateway.WorkbenchRelayStreamDelta{ContentDelta: "Partial answer."})
	return appaigateway.WorkbenchRelayResponse{}, apperrors.ErrClusterUnready
}

func TestNativeIncompleteStreamDoesNotCompleteOrResetUsage(t *testing.T) {
	service, store, gateway, _ := nativeLoopFixture(t)
	service.workbenchInvoker = &nativeIncompleteStreamGateway{gateway}
	if err := service.processNativeRun(t.Context(), store, store.lease); err != nil || store.lease.Run.Status != "failed" || len(store.final) != 1 {
		t.Fatalf("incomplete stream did not fail: status=%s final=%v err=%v", store.lease.Run.Status, store.final, err)
	}
	if store.lease.Run.Output["usageKnown"] != false || store.lease.Run.Output["answer"] == "Partial answer." || gateway.toolCalls != 0 {
		t.Fatal("incomplete stream became a final answer, known usage or tool execution")
	}
}
