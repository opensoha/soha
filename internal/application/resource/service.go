package resource

import (
	"context"
	"net/http"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainoperation "github.com/opensoha/soha/internal/domain/operation"
	domainsettings "github.com/opensoha/soha/internal/domain/settings"
	agentinfra "github.com/opensoha/soha/internal/infrastructure/agent"
	informerinfra "github.com/opensoha/soha/internal/infrastructure/informer"
	k8sinfra "github.com/opensoha/soha/internal/infrastructure/kubernetes"
	portforwardrepo "github.com/opensoha/soha/internal/repository/portforward"
)

type AuditRecorder interface {
	Record(context.Context, domainaudit.Entry) error
}

type OperationRecorder interface {
	Record(context.Context, domainoperation.Entry) error
}

type ConnectionResolver interface {
	GetConnection(context.Context, string) (domaincluster.Connection, error)
}

type MonitoringSettingsResolver interface {
	ResolveMonitoringSettings(context.Context) (domainsettings.MonitoringSettings, error)
}

type Service struct {
	clusters     *k8sinfra.Manager
	cache        *informerinfra.Service
	agents       *agentinfra.Registry
	resolver     ConnectionResolver
	authorizer   domainaccess.Authorizer
	permissions  *appaccess.PermissionResolver
	audit        AuditRecorder
	operations   OperationRecorder
	settings     MonitoringSettingsResolver
	httpClient   *http.Client
	portForwards PortForwardRepository
}

type PortForwardRepository interface {
	List(ctx context.Context) ([]portforwardrepo.Record, error)
	Upsert(ctx context.Context, rec portforwardrepo.Record) error
	Delete(ctx context.Context, sessionID string) error
	MarkStatus(ctx context.Context, sessionID, status, lastErr string) error
}

type crdResourceDefinition struct {
	CRDName    string
	Kind       string
	Group      string
	Version    string
	Resource   string
	Namespaced bool
}

func New(clusters *k8sinfra.Manager, cache *informerinfra.Service, agents *agentinfra.Registry, resolver ConnectionResolver, authorizer domainaccess.Authorizer, permissions *appaccess.PermissionResolver, audit AuditRecorder, operations OperationRecorder, settings MonitoringSettingsResolver) *Service {
	return &Service{
		clusters:    clusters,
		cache:       cache,
		agents:      agents,
		resolver:    resolver,
		authorizer:  authorizer,
		permissions: permissions,
		audit:       audit,
		operations:  operations,
		settings:    settings,
		httpClient:  &http.Client{Timeout: 10 * time.Second},
	}
}

func (s *Service) SetPortForwardRepository(repo PortForwardRepository) {
	s.portForwards = repo
}
