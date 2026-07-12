package directorysync

import (
	"context"
	"fmt"
	"sync"

	appdirectorysync "github.com/opensoha/soha/internal/application/directorysync"
	domain "github.com/opensoha/soha/internal/domain/directorysync"
	domainsettings "github.com/opensoha/soha/internal/domain/settings"
	dingtalkconnector "github.com/opensoha/soha/internal/infrastructure/directoryconnector/dingtalk"
	feishuconnector "github.com/opensoha/soha/internal/infrastructure/directoryconnector/feishu"
	ldapconnector "github.com/opensoha/soha/internal/infrastructure/directoryconnector/ldap"
	wecomconnector "github.com/opensoha/soha/internal/infrastructure/directoryconnector/wecom"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type TokenResolver func(context.Context, domain.Connection) (string, error)
type LoginProviderResolver interface {
	ResolveLoginProvider(context.Context, string) (domainsettings.LoginProviderSettings, error)
}

type Registry struct {
	mu        sync.RWMutex
	factories map[string]func() (appdirectorysync.Connector, error)
}

func NewRegistry(resolveFeishuToken TokenResolver, providerResolver LoginProviderResolver, credentialResolver ldapconnector.CredentialResolver) *Registry {
	r := &Registry{factories: make(map[string]func() (appdirectorysync.Connector, error))}
	if resolveFeishuToken != nil {
		r.factories[domain.ProviderFeishu] = func() (appdirectorysync.Connector, error) {
			return feishuconnector.NewAdapter(feishuconnector.TokenResolver(resolveFeishuToken))
		}
	}
	if providerResolver != nil {
		resolver := providerResolver
		r.factories[domain.ProviderWeCom] = func() (appdirectorysync.Connector, error) { return wecomconnector.NewAdapter(resolver, nil, "") }
		r.factories[domain.ProviderDingTalk] = func() (appdirectorysync.Connector, error) { return dingtalkconnector.NewAdapter(resolver, nil, "") }
	}
	if credentialResolver != nil {
		r.factories[domain.ProviderLDAP] = func() (appdirectorysync.Connector, error) { return ldapconnector.NewAdapter(credentialResolver) }
	}
	return r
}

func (r *Registry) Connector(provider string) (appdirectorysync.Connector, error) {
	r.mu.RLock()
	factory := r.factories[provider]
	r.mu.RUnlock()
	if factory == nil {
		return nil, fmt.Errorf("%w: directory provider %q is unavailable", apperrors.ErrUnsupportedOperation, provider)
	}
	connector, err := factory()
	if err != nil {
		return nil, fmt.Errorf("build directory connector: %w", err)
	}
	return connector, nil
}
