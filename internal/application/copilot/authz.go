package copilot

import (
	"context"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

func (s *Service) authorizePrincipal(ctx context.Context, principal domainidentity.Principal, permissionKey string) error {
	return appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, permissionKey)
}

func systemPrincipal() domainidentity.Principal {
	return domainidentity.Principal{
		UserID:   "system",
		UserName: "system",
		Roles:    []string{"admin"},
	}
}
