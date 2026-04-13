package build

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	domainaccess "github.com/kubecrux/kubecrux/internal/domain/access"
	domainapp "github.com/kubecrux/kubecrux/internal/domain/application"
	domainaudit "github.com/kubecrux/kubecrux/internal/domain/audit"
	domainbuild "github.com/kubecrux/kubecrux/internal/domain/build"
	domainevent "github.com/kubecrux/kubecrux/internal/domain/event"
	domainidentity "github.com/kubecrux/kubecrux/internal/domain/identity"
	"github.com/kubecrux/kubecrux/internal/platform/apperrors"
	"github.com/kubecrux/kubecrux/internal/platform/requestctx"
)

type BuildRepository interface {
	List(context.Context, domainbuild.Filter) ([]domainbuild.Record, error)
	Create(context.Context, domainbuild.TriggerInput, map[string]any) (domainbuild.Record, error)
}

type ApplicationReader interface {
	Get(context.Context, string) (domainapp.App, error)
}

type EventWriter interface {
	Create(context.Context, domainevent.Envelope) error
}

type AuditRecorder interface {
	Record(context.Context, domainaudit.Entry) error
}

type Service struct {
	repo       BuildRepository
	apps       ApplicationReader
	authorizer domainaccess.Authorizer
	events     EventWriter
	audit      AuditRecorder
}

func New(repo BuildRepository, apps ApplicationReader, authorizer domainaccess.Authorizer, events EventWriter, audit AuditRecorder) *Service {
	return &Service{repo: repo, apps: apps, authorizer: authorizer, events: events, audit: audit}
}

func (s *Service) List(ctx context.Context, principal domainidentity.Principal, filter domainbuild.Filter) ([]domainbuild.Record, error) {
	if err := s.authorize(ctx, principal, domainaccess.ActionList, filter.ApplicationID); err != nil {
		return nil, err
	}
	items, err := s.repo.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Service) Trigger(ctx context.Context, principal domainidentity.Principal, input domainbuild.TriggerInput) (domainbuild.Record, error) {
	if input.ApplicationID == "" {
		return domainbuild.Record{}, fmt.Errorf("%w: applicationId is required", apperrors.ErrInvalidArgument)
	}
	if input.RefType == "" {
		input.RefType = "branch"
	}
	if input.RefName == "" {
		return domainbuild.Record{}, fmt.Errorf("%w: refName is required", apperrors.ErrInvalidArgument)
	}
	app, err := s.apps.Get(ctx, input.ApplicationID)
	if err != nil {
		return domainbuild.Record{}, err
	}
	if err := s.authorize(ctx, principal, domainaccess.ActionUpdate, app.ID); err != nil {
		return domainbuild.Record{}, err
	}
	metadata := map[string]any{
		"applicationName": app.Name,
		"refType":         input.RefType,
		"refName":         input.RefName,
		"imageTag":        input.ImageTag,
		"buildArgs":       input.BuildArgs,
		"repositoryPath":  app.RepositoryPath,
		"pipelineStages": []map[string]any{
			{"name": "queued", "status": "completed", "timestamp": time.Now().UTC().Format(time.RFC3339)},
			{"name": "planning", "status": "running", "timestamp": time.Now().UTC().Format(time.RFC3339)},
		},
		"logs": []map[string]any{
			{"level": "info", "message": fmt.Sprintf("Manual build requested for %s on %s:%s", app.Name, input.RefType, input.RefName), "timestamp": time.Now().UTC().Format(time.RFC3339)},
			{"level": "info", "message": "Build execution worker has not started yet; record is queued for future runner integration", "timestamp": time.Now().UTC().Format(time.RFC3339)},
		},
		"artifacts": []map[string]any{
			{"kind": "image", "ref": fmt.Sprintf("%s:%s", app.BuildImage, input.ImageTag), "status": "planned"},
		},
		"imageDigest": "pending",
	}
	record, err := s.repo.Create(ctx, input, metadata)
	if err != nil {
		return domainbuild.Record{}, err
	}
	if s.events != nil {
		_ = s.events.Create(ctx, domainevent.Envelope{
			ID:       "event:" + record.ID,
			Source:   "build",
			Category: "build",
			Severity: "info",
			Summary:  fmt.Sprintf("Build queued for %s on %s:%s", app.Name, input.RefType, input.RefName),
			Payload: map[string]any{
				"buildId":       record.ID,
				"applicationId": app.ID,
				"status":        record.Status,
			},
		})
	}
	_ = s.recordAudit(ctx, principal, app.Name, string(domainaccess.ActionUpdate), "success", "triggered manual build")
	return record, nil
}

func (s *Service) authorize(ctx context.Context, principal domainidentity.Principal, action domainaccess.Action, resourceName string) error {
	if s.authorizer == nil {
		return nil
	}
	app, _ := s.apps.Get(ctx, resourceName)
	decision, err := s.authorizer.Authorize(ctx, domainaccess.Request{
		Principal: principal,
		Action:    action,
		Subject: domainaccess.SubjectAttributes{
			UserID:   principal.UserID,
			Roles:    principal.Roles,
			Teams:    principal.Teams,
			Projects: principal.Projects,
			Tags:     principal.Tags,
		},
		Resource: domainaccess.ResourceAttributes{
			Kind: "Build",
			Name: resourceName,
		},
		Delivery: domainaccess.DeliveryAttributes{
			BusinessLineID: app.BusinessLineID,
			ApplicationID:  app.ID,
		},
		Context: domainaccess.ContextAttributes{
			Source:     requestctx.FromContext(ctx).Source,
			OccurredAt: time.Now().UTC(),
		},
	})
	if err != nil {
		return err
	}
	if !decision.Allowed {
		return fmt.Errorf("%w: %s", apperrors.ErrAccessDenied, decision.Reason)
	}
	return nil
}

func (s *Service) recordAudit(ctx context.Context, principal domainidentity.Principal, resourceName, action, result, summary string) error {
	if s.audit == nil {
		return nil
	}
	meta := requestctx.FromContext(ctx)
	return s.audit.Record(ctx, domainaudit.Entry{
		ID:            uuid.NewString(),
		ActorID:       principal.UserID,
		ActorName:     principal.UserName,
		Roles:         principal.Roles,
		Teams:         principal.Teams,
		ResourceKind:  "Build",
		ResourceName:  resourceName,
		Action:        action,
		Result:        result,
		Summary:       summary,
		RequestPath:   meta.Path,
		RequestMethod: meta.Method,
		RequestID:     meta.RequestID,
		SourceIP:      meta.SourceIP,
		Metadata: map[string]any{
			"source": meta.Source,
		},
	})
}
