package resource

import (
	"context"
	"fmt"
	"strings"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

// GetSecretCertificate exposes a closed public metadata projection. Secret data
// reads retain their separate runtime permission and are never used here.
func (c *Configuration) GetSecretCertificate(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.SecretCertificateView, error) {
	if strings.TrimSpace(namespace) == "" || strings.TrimSpace(name) == "" {
		return domainresource.SecretCertificateView{}, fmt.Errorf("%w: namespace and name are required", apperrors.ErrInvalidArgument)
	}
	connection, _, err := c.authorize(ctx, principal, clusterID, namespace, "Secret", domainaccess.ActionView)
	if err != nil {
		return domainresource.SecretCertificateView{}, err
	}
	item, _, err := routeModeValue(connection, c.configurationAgentClient, c.directConfiguration,
		func(a ConfigurationAgent) (domainresource.SecretCertificateView, error) {
			return a.GetSecretCertificate(ctx, namespace, name)
		},
		func(d DirectConfiguration) (domainresource.SecretCertificateView, error) {
			return d.GetSecretCertificate(ctx, clusterID, namespace, name)
		})
	if err != nil {
		return domainresource.SecretCertificateView{}, err
	}
	_ = c.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Secret", name, string(domainaccess.ActionView), "success", "viewed public certificate metadata")
	return item, nil
}
