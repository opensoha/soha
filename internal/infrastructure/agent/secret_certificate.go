package agent

import (
	"context"
	"net/http"
	"net/url"

	domainresource "github.com/opensoha/soha/internal/domain/resource"
)

func (c *Client) GetSecretCertificate(ctx context.Context, namespace, name string) (domainresource.SecretCertificateView, error) {
	var payload struct {
		Data domainresource.SecretCertificateView `json:"data"`
	}
	err := c.request(ctx, http.MethodGet, basicMutationPath+"/configuration/secrets/"+url.PathEscape(name)+"/certificate?namespace="+url.QueryEscape(namespace), nil, &payload)
	return payload.Data, err
}
