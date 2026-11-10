package manifest

import (
	"context"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	dm "github.com/opensoha/soha/internal/domain/manifest"
)

func (s *Service) authorizeSensitiveFiles(ctx context.Context, p domainidentity.Principal, files []dm.File) error {
	if dm.SensitiveFiles(files) {
		return s.authorize(ctx, p, appaccess.PermPlatformConfigurationSecretDataView)
	}
	return nil
}
