package access

import (
	"context"
	"errors"
	"slices"
	"testing"

	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestDockerFilesDefaultGrantsAndCustomRoleIsolation(t *testing.T) {
	defaults := defaultRolePermissions()
	for _, role := range []string{"admin", "ops", "developer"} {
		if !slices.Contains(defaults[role], PermDockerServicesFiles) {
			t.Fatalf("default %s lacks explicit file permission", role)
		}
	}
	for _, role := range []string{"readonly", "tester", "auditor"} {
		if slices.Contains(defaults[role], PermDockerServicesFiles) {
			t.Fatalf("default %s unexpectedly grants file content", role)
		}
	}
	for _, coarse := range []string{PermDockerServicesView, PermDockerServicesManage, "docker.services.actions"} {
		t.Run(coarse, func(t *testing.T) {
			resolver := NewPermissionResolver(stubRolePermissionReader{matrix: map[string][]string{"custom": {coarse}}})
			if err := resolver.Authorize(context.Background(), domainidentity.Principal{UserID: "reader", Roles: []string{"custom"}}, PermDockerServicesFiles); !errors.Is(err, apperrors.ErrAccessDenied) {
				t.Fatalf("coarse custom grant authorized files: %v", err)
			}
		})
	}
}
