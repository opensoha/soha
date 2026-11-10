package copilot

import (
	"testing"

	"errors"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

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
