package access

import (
	"context"
	"errors"
	"slices"
	"testing"

	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestRuntimePrincipalWithinKeepsFrozenCeiling(t *testing.T) {
	keys := []string{PermAIGatewayInvoke, PermAIKnowledgeView}
	resolver := NewPermissionResolver(stubRolePermissionReader{matrix: map[string][]string{"operator": keys}})
	principal := domainidentity.Principal{UserID: "actor", Roles: []string{"operator"}}
	bounded, err := RuntimePrincipalWithin(context.Background(), resolver, principal, []string{PermAIGatewayInvoke})
	if err != nil || !slices.Equal(bounded.PermissionKeys, []string{PermAIGatewayInvoke}) {
		t.Fatalf("new role grant exceeded frozen ceiling: %+v %v", bounded, err)
	}
	for _, ceiling := range [][]string{nil, {}, {PermObserveAIChatUse}} {
		if _, err := RuntimePrincipalWithin(context.Background(), resolver, principal, ceiling); !errors.Is(err, apperrors.ErrAccessDenied) {
			t.Fatalf("empty intersection restored current role grants: %v", err)
		}
	}
	principal.PermissionKeys = []string{PermAIKnowledgeView}
	if _, err := RuntimePrincipalWithin(context.Background(), resolver, principal, []string{PermAIGatewayInvoke}); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("current token ceiling widened: %v", err)
	}
}
