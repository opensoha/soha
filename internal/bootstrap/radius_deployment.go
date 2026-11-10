package bootstrap

import (
	"context"
	"fmt"
	appaccess "github.com/opensoha/soha/internal/application/access"
	appaudit "github.com/opensoha/soha/internal/application/audit"
	execution "github.com/opensoha/soha/internal/application/execution"
	appproxy "github.com/opensoha/soha/internal/application/networkproxy"
	operation "github.com/opensoha/soha/internal/application/operation"
	app "github.com/opensoha/soha/internal/application/radiusdeployment"
	access "github.com/opensoha/soha/internal/domain/access"
	audit "github.com/opensoha/soha/internal/domain/audit"
	"github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/radiusdeployment"
	config "github.com/opensoha/soha/internal/infrastructure/config"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/operationentry"
	"github.com/opensoha/soha/internal/platform/requestctx"
	repo "github.com/opensoha/soha/internal/repository/radiusdeployment"
	"time"
)

type radiusDeploymentServices struct {
	management *app.Service
	executor   *app.ExecutorService
}

func newRadiusDeploymentServices(cfg config.Config, infra *infrastructure, p *appaccess.PermissionResolver, scope *appaccess.Service, a *appaudit.Service, o *operation.Service, e *execution.Service) (*radiusDeploymentServices, error) {
	slots := []domain.Slot{}
	for _, s := range cfg.Runtime.RadiusSlots {
		slots = append(slots, s.Slot())
	}
	authority := radiusAuthority{p, scope, a, o}
	s, err := app.New(repo.New(infra.databaseStore.DB()), authority, slots)
	if err != nil {
		return nil, err
	}
	creds, err := radiusExecutorCredentials(cfg)
	if err != nil {
		return nil, err
	}
	x, err := app.NewExecutorService(s, e, creds)
	if err != nil {
		return nil, err
	}
	e.AddExecutionTaskSink(s)
	return &radiusDeploymentServices{s, x}, nil
}
func radiusExecutorCredentials(cfg config.Config) ([]app.ExecutorCredential, error) {
	reserved := map[string]bool{cfg.Runtime.ExecutionRunnerToken: true}
	for _, v := range cfg.Runtime.OutpostRuntimeExecutors {
		t, e := v.ReadToken()
		if e != nil {
			return nil, e
		}
		reserved[t] = true
	}
	for _, v := range cfg.Runtime.ProxyRuntimeExecutors {
		t, e := v.ReadToken()
		if e != nil {
			return nil, e
		}
		reserved[t] = true
	}
	out := []app.ExecutorCredential{}
	for _, v := range cfg.Runtime.RadiusRuntimeExecutors {
		t, e := v.ReadToken()
		if e != nil {
			return nil, e
		}
		if reserved[t] {
			return nil, fmt.Errorf("RADIUS executor requires dedicated credential")
		}
		reserved[t] = true
		out = append(out, app.ExecutorCredential{ExecutorID: v.ExecutorID, Token: t})
	}
	return out, nil
}

type radiusAuthority struct {
	permissions *appaccess.PermissionResolver
	scope       interface {
		Authorize(context.Context, access.Request) (access.Decision, error)
	}
	audit      *appaudit.Service
	operations *operation.Service
}

func (a radiusAuthority) AuthorizeRADIUS(ctx context.Context, p identity.Principal, site, binding, action string) error {
	key, known := map[string]string{"view": appaccess.PermNetworkAccessNASBindingsView, "create": appaccess.PermNetworkAccessNASBindingsCreate, "update": appaccess.PermNetworkAccessNASBindingsUpdate}[action]
	if !known {
		return apperrors.ErrAccessDenied
	}
	if err := appaccess.AuthorizeRuntimePermission(ctx, a.permissions, p, key); err != nil {
		return err
	}
	if site == "" {
		return nil
	}
	d, e := a.scope.Authorize(ctx, access.Request{Principal: p, PermissionKey: key, Subject: access.SubjectAttributes{UserID: p.UserID, Roles: p.Roles, Teams: p.Teams, Projects: p.Projects, Tags: p.Tags}, Resource: access.ResourceAttributes{Group: "network_access", Kind: "NetworkNASBinding", Name: binding, Labels: map[string]string{"network.siteId": site}}, Context: access.ContextAttributes{Source: "api", OccurredAt: time.Now().UTC()}})
	if e != nil {
		return e
	}
	if !d.Allowed {
		return apperrors.ErrAccessDenied
	}
	return nil
}
func (a radiusAuthority) RecordRADIUS(ctx context.Context, p identity.Principal, pool, action string, revision int64) {
	m := requestctx.FromContext(ctx)
	key := "network_access.radius_pools." + action
	meta := map[string]any{"poolId": pool, "revision": revision}
	_ = a.audit.Record(ctx, audit.Entry{ActorID: p.UserID, ActorName: p.UserName, Roles: p.Roles, Teams: p.Teams, ResourceKind: "NetworkRADIUSPool", ResourceName: pool, Action: key, Result: "success", RequestID: m.RequestID, RequestPath: m.Path, RequestMethod: m.Method, SourceIP: m.SourceIP, Metadata: meta})
	_ = a.operations.Record(ctx, operationentry.New(ctx, p, key, map[string]any{"module": "network_access", "resourceKind": "NetworkRADIUSPool", "targetId": pool}, "success", key+" succeeded", meta))
}

func newAdmissionRuntimeServices(cfg config.Config, infra *infrastructure, repos *repositories, p *appaccess.PermissionResolver, scope *appaccess.Service, a *appaudit.Service, o *operation.Service, e *execution.Service, proxy *appproxy.Management) (*proxyWorkspaceServices, *radiusDeploymentServices, error) {
	workspace, err := newProxyWorkspaceServices(cfg, infra, repos, p, a, o, e, proxy)
	if err != nil {
		return nil, nil, err
	}
	radius, err := newRadiusDeploymentServices(cfg, infra, p, scope, a, o, e)
	return workspace, radius, err
}
