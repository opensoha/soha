package copilot

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"errors"
	appaccess "github.com/opensoha/soha/internal/application/access"
	appaigateway "github.com/opensoha/soha/internal/application/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/policy"
)

func TestNativeChatAcceptsJWTAndDatabaseEmptyMemberships(t *testing.T) {
	service, store, gateway, _ := nativeLoopFixture(t)
	authorization := appaccess.New(policy.NewEngine(), nil, nil, nil)
	authorization.SetPermissionResolver(service.permissions)
	service.nativeAuthorization = authorization
	principal := domainidentity.Principal{UserID: "reader", Roles: []string{"reader"}}
	revision, err := authorization.ExecutionAuthorizationRevision(t.Context(), principal)
	if err != nil {
		t.Fatal(err)
	}
	store.lease.Run.Input["_sohaAuthorizationCeiling"] = revision
	principal.Teams, principal.Projects = []string{}, []string{}
	service.agentPrincipals = &chatToolPrincipalResolver{principal: principal}
	service.personalEnvironment = &PersonalEnvironmentDependencies{}
	calls := 0
	gateway.model = func(context.Context, appaigateway.WorkbenchRelayRequest) (appaigateway.WorkbenchRelayResponse, error) {
		calls++
		return appaigateway.WorkbenchRelayResponse{Content: "Hello", Usage: map[string]float64{"totalTokens": 10}}, nil
	}
	if err := service.processNativeRun(t.Context(), store, store.lease); err != nil || calls != 1 || store.lease.Run.Status != "completed" || len(store.final) != 1 {
		t.Fatalf("equivalent subject failed Native chat: models=%d status=%s messages=%d err=%v", calls, store.lease.Run.Status, len(store.final), err)
	}
}

func TestNativeAuthorizationAcceptsOnlyLegacyAbsenceAndCompleteFrozenCeilings(t *testing.T) {
	for _, state := range []string{"legacy", "complete", "partial", "empty"} {
		t.Run(state, func(t *testing.T) {
			service, store, _, _ := nativeLoopFixture(t)
			switch state {
			case "legacy":
				delete(store.lease.Run.Input, "_sohaAuthorizationCeiling")
				delete(store.lease.Run.Input, "_sohaGatewayAuthorizationCeiling")
			case "partial":
				delete(store.lease.Run.Input, "_sohaAuthorizationCeiling")
			case "empty":
				store.lease.Run.Input["_sohaAuthorizationCeiling"] = ""
			}
			principal, err := service.nativePrincipal(t.Context(), store.lease.Run)
			if err != nil {
				t.Fatal(err)
			}
			_, err = service.nativeAuthorizationContext(t.Context(), principal, store.lease.Run, false)
			if (err == nil) != (state == "legacy" || state == "complete") {
				t.Fatalf("invalid read ceiling accepted: state=%s err=%v", state, err)
			}
			_, err = service.nativeAuthorizationContext(t.Context(), principal, store.lease.Run, true)
			if state == "complete" {
				if err != nil {
					t.Fatal(err)
				}
			} else if !errors.Is(err, apperrors.ErrAccessDenied) {
				t.Fatalf("write used missing ceiling: state=%s err=%v", state, err)
			}
		})
	}
}

func TestNativeAuthorizationChangeStopsBeforeAnotherModelOrTool(t *testing.T) {
	for _, change := range []string{"access", "gateway", "missing_reader"} {
		t.Run(change, func(t *testing.T) {
			service, store, gateway, _ := nativeLoopFixture(t)
			calls := 0
			gateway.model = func(context.Context, appaigateway.WorkbenchRelayRequest) (appaigateway.WorkbenchRelayResponse, error) {
				calls++
				switch change {
				case "access":
					reader, ok := service.nativeAuthorization.(*nativeAuthorizationFixture)
					if !ok {
						t.Fatal("Native authorization fixture is unavailable")
					}
					reader.revision = strings.Repeat("c", 64)
				case "gateway":
					gateway.authorizationRevision = strings.Repeat("c", 64)
				case "missing_reader":
					service.nativeAuthorization = nil
				}
				return appaigateway.WorkbenchRelayResponse{Usage: map[string]float64{"totalTokens": 10}, ToolCalls: []appaigateway.WorkbenchToolCall{{ID: "call", Name: "knowledge_search", Arguments: json.RawMessage(`{"query":"read"}`)}}}, nil
			}
			if err := service.processNativeRun(t.Context(), store, store.lease); err != nil || calls != 1 || gateway.toolCalls != 0 || store.lease.Run.Status != "failed" || len(store.final) != 1 || store.final[0].Role != "system" {
				t.Fatalf("changed authorization continued: models=%d tools=%d status=%s err=%v", calls, gateway.toolCalls, store.lease.Run.Status, err)
			}
		})
	}
}
