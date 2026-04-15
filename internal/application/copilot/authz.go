package copilot

import (
	"fmt"

	appaccess "github.com/kubecrux/kubecrux/internal/application/access"
	domainidentity "github.com/kubecrux/kubecrux/internal/domain/identity"
	aperrors "github.com/kubecrux/kubecrux/internal/platform/apperrors"
)

func authorizePrincipal(principal domainidentity.Principal, permissionKey string) error {
	if appaccess.HasPermission(principal.Roles, permissionKey) {
		return nil
	}
	return fmt.Errorf("%w: missing permission %s", aperrors.ErrAccessDenied, permissionKey)
}

func systemPrincipal() domainidentity.Principal {
	return domainidentity.Principal{
		UserID:   "system",
		UserName: "system",
		Roles:    []string{"admin"},
	}
}
