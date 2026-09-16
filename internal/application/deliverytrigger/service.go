package deliverytrigger

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domaindocument "github.com/opensoha/soha/internal/domain/deliverydocument"
	domain "github.com/opensoha/soha/internal/domain/deliverytrigger"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainoperation "github.com/opensoha/soha/internal/domain/operation"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/operationentry"
)

type Repository interface {
	Get(context.Context, string) (domain.StoredTrigger, error)
	List(context.Context, string, string, int, int) ([]domain.StoredTrigger, error)
	Save(context.Context, domain.StoredTrigger, int) (domain.StoredTrigger, error)
	Enqueue(context.Context, domain.StoredEvent) (domain.Event, error)
	Events(context.Context, string, int, int) ([]domain.Event, error)
	Claim(context.Context, string, time.Time) (domain.StoredEvent, error)
	Checkpoint(context.Context, domain.StoredEvent) error
	Finish(context.Context, domain.StoredEvent) error
}

type Identity interface {
	ParseAccessToken(context.Context, string) (domainidentity.Principal, domainidentity.AccessContext, error)
	CurrentExecutionPrincipal(context.Context, string, string) (domainidentity.Principal, error)
}

type Sources interface {
	Get(context.Context, domainidentity.Principal, string) (domaindocument.Source, error)
	Sync(context.Context, domainidentity.Principal, string, domaindocument.SyncInput) (domaindocument.SyncRun, error)
	Apply(context.Context, domainidentity.Principal, string, string, domaindocument.SyncApplyInput) (domaindocument.SyncRun, error)
}

type Workflows interface {
	GetDeliveryWorkflow(context.Context, domainidentity.Principal, string) (domainworkflow.DeliveryWorkflow, error)
	PrepareDeliveryWorkflow(context.Context, domainidentity.Principal, string, domainworkflow.DeliveryWorkflowInput) (domainworkflow.DeliveryWorkflowInput, error)
	CreateDeliveryBatch(context.Context, domainidentity.Principal, domainworkflow.DeliveryBatchInput) (domainworkflow.DeliveryBatch, error)
	GetDeliveryBatch(context.Context, domainidentity.Principal, string) (domainworkflow.DeliveryBatch, error)
	FindDeliveryBatch(context.Context, domainidentity.Principal, domainworkflow.DeliveryBatchInput) (domainworkflow.DeliveryBatch, error)
}

type Repositories interface {
	GetRepository(context.Context, domainidentity.Principal, string) (domainapp.SourceRepository, error)
}
type Git interface {
	ResolveDeliveryCommit(context.Context, domainapp.SourceRepository, string, string) (string, error)
}
type Audit interface {
	Record(context.Context, domainaudit.Entry) error
}

type Operations interface {
	Record(context.Context, domainoperation.Entry) error
}

type Service struct {
	repo         Repository
	identity     Identity
	sources      Sources
	workflows    Workflows
	repositories Repositories
	git          Git
	permissions  *appaccess.PermissionResolver
	audit        Audit
	operations   Operations
}

func New(repo Repository, identity Identity, sources Sources, workflows Workflows, repositories Repositories, git Git, permissions *appaccess.PermissionResolver, audit Audit, operations Operations) *Service {
	return &Service{repo: repo, identity: identity, sources: sources, workflows: workflows, repositories: repositories, git: git, permissions: permissions, audit: audit, operations: operations}
}

func (s *Service) permission(ctx context.Context, p domainidentity.Principal, action string) error {
	return appaccess.AuthorizeRuntimePermission(ctx, s.permissions, p, "delivery.triggers."+action)
}

func (s *Service) Get(ctx context.Context, p domainidentity.Principal, id string) (domain.Trigger, error) {
	if err := s.permission(ctx, p, "view"); err != nil {
		return domain.Trigger{}, err
	}
	item, err := s.repo.Get(ctx, id)
	if err != nil {
		return domain.Trigger{}, err
	}
	if _, err := s.target(ctx, p, item, false); err != nil {
		return domain.Trigger{}, err
	}
	return item.Trigger, nil
}

func (s *Service) List(ctx context.Context, p domainidentity.Principal, kind, target string, offset, limit int) ([]domain.Trigger, error) {
	if err := s.permission(ctx, p, "view"); err != nil {
		return nil, err
	}
	if offset < 0 || limit < 1 || limit > 200 || kind != "" && kind != "template_source" && kind != "workflow" || len(target) > 200 {
		return nil, apperrors.ErrInvalidArgument
	}
	items := []domain.Trigger{}
	visible := 0
	for scanned := 0; ; scanned += 200 {
		batch, err := s.repo.List(ctx, kind, target, scanned, 200)
		if err != nil {
			return nil, err
		}
		for _, item := range batch {
			if _, err := s.target(ctx, p, item, false); err != nil {
				if errors.Is(err, apperrors.ErrAccessDenied) || errors.Is(err, apperrors.ErrNotFound) {
					continue
				}
				return nil, err
			}
			if visible >= offset {
				items = append(items, item.Trigger)
			}
			visible++
			if len(items) == limit {
				return items, nil
			}
		}
		if len(batch) < 200 {
			return items, nil
		}
	}
}

func (s *Service) Events(ctx context.Context, p domainidentity.Principal, id string, offset, limit int) ([]domain.Event, error) {
	if _, err := s.Get(ctx, p, id); err != nil {
		return nil, err
	}
	if offset < 0 || limit < 1 || limit > 200 {
		return nil, apperrors.ErrInvalidArgument
	}
	return s.repo.Events(ctx, id, offset, limit)
}

func (s *Service) Save(ctx context.Context, p domainidentity.Principal, id string, input domain.Input) (domain.Trigger, error) {
	action := "create"
	if id != "" {
		action = "update"
	}
	if err := s.permission(ctx, p, action); err != nil {
		return domain.Trigger{}, err
	}
	if err := validateInput(id, input); err != nil {
		return domain.Trigger{}, err
	}
	now := time.Now().UTC()
	item := domain.StoredTrigger{Trigger: domain.Trigger{ID: uuid.NewString(), CreatedBy: p.UserID, CreatedAt: now}}
	if id != "" {
		var err error
		item, err = s.repo.Get(ctx, id)
		if err != nil {
			return domain.Trigger{}, err
		}
		if _, err := s.target(ctx, p, item, false); err != nil {
			return domain.Trigger{}, err
		}
		if item.Revision != input.ExpectedRevision {
			return domain.Trigger{}, apperrors.ErrConflict
		}
	}
	item.Name, item.Enabled, item.Revision = strings.TrimSpace(input.Name), input.Enabled, input.ExpectedRevision+1
	item.TargetKind, item.TargetID, item.Type = sohaapi.DeliveryTriggerTargetKind(input.TargetKind), input.TargetID, sohaapi.DeliveryTriggerType(input.Type)
	item.WorkflowVersion, item.Webhook, item.Schedule = input.WorkflowVersion, input.Webhook, nil
	if input.Schedule != nil {
		item.Schedule = &domain.Schedule{Cron: input.Schedule.Cron, TimeZone: input.Schedule.TimeZone, RunAt: input.Schedule.RunAt, ExcludedDates: input.Schedule.ExcludedDates}
	}
	item.UpdatedBy, item.UpdatedByTokenID, item.UpdatedAt = p.UserID, p.AccessTokenID, now
	executor, err := s.configureIdentity(ctx, &item, input, id == "")
	if err != nil {
		return domain.Trigger{}, err
	}
	digest, err := s.target(ctx, p, item, input.Enabled || id == "")
	if err != nil {
		return domain.Trigger{}, err
	}
	if input.Enabled || id == "" {
		executorDigest, err := s.target(ctx, executor, item, true)
		if err != nil {
			return domain.Trigger{}, err
		}
		if digest != executorDigest {
			return domain.Trigger{}, fmt.Errorf("%w: target changed during authorization", apperrors.ErrConflict)
		}
	}
	item.TargetDigest = digest
	saved, err := s.repo.Save(ctx, item, input.ExpectedRevision)
	if err != nil {
		return domain.Trigger{}, err
	}
	s.record(ctx, p, "delivery.trigger."+action, saved.ID, "success", map[string]any{"revision": saved.Revision, "targetKind": saved.TargetKind, "targetId": saved.TargetID, "serviceAccountId": saved.ServiceAccountID, "enabled": saved.Enabled})
	return saved.Trigger, nil
}

func (s *Service) executionIdentity(ctx context.Context, item *domain.StoredTrigger, token string) (domainidentity.Principal, error) {
	var principal domainidentity.Principal
	var err error
	if token != "" {
		var access domainidentity.AccessContext
		principal, access, err = s.identity.ParseAccessToken(ctx, token)
		if err != nil {
			return principal, err
		}
		if access.TokenKind != "service_account_token" || principal.AccessTokenID == "" {
			return principal, fmt.Errorf("%w: use a service account token for automation", apperrors.ErrInvalidArgument)
		}
		item.ServiceAccountID, item.ServiceAccountName, item.ExecutionTokenID = access.SubjectID, principal.UserName, principal.AccessTokenID
	} else {
		if item.ExecutionTokenID == "" {
			return principal, fmt.Errorf("%w: serviceAccountToken is required", apperrors.ErrInvalidArgument)
		}
		principal, err = s.identity.CurrentExecutionPrincipal(ctx, "service_account:"+item.ServiceAccountID, item.ExecutionTokenID)
	}
	return principal, err
}

func digest(value any) (string, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func (s *Service) record(ctx context.Context, p domainidentity.Principal, action, id, result string, metadata map[string]any) {
	metadata["targetId"] = id
	if s.audit != nil {
		_ = s.audit.Record(ctx, domainaudit.Entry{ActorID: p.UserID, ActorName: p.UserName, ResourceKind: "DeliveryTrigger", ResourceName: id, Action: action, Result: result, Summary: "delivery trigger configuration or dispatch", Metadata: metadata})
	}
	if s.operations != nil {
		_ = s.operations.Record(ctx, operationentry.New(ctx, p, action, map[string]any{"module": "delivery", "resourceKind": "DeliveryTrigger", "targetId": id}, result, "delivery trigger configuration or dispatch", metadata))
	}
}

func (s *Service) configureIdentity(ctx context.Context, item *domain.StoredTrigger, input domain.Input, creating bool) (domainidentity.Principal, error) {
	var executor domainidentity.Principal
	var err error
	if input.Enabled || creating || input.ServiceAccountToken != "" {
		executor, err = s.executionIdentity(ctx, item, input.ServiceAccountToken)
		if err != nil {
			return executor, err
		}
	}
	if input.WebhookSigningSecret != "" {
		item.SigningSecret = input.WebhookSigningSecret
	}
	if item.Type == "webhook" {
		if _, err := decodeSigningSecret(item.SigningSecret); err != nil {
			return executor, err
		}
	} else {
		item.SigningSecret = ""
	}
	item.SigningSecretConfigured = item.SigningSecret != ""
	return executor, nil
}
