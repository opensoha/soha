package announcement

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainannouncement "github.com/opensoha/soha/internal/domain/announcement"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainoperation "github.com/opensoha/soha/internal/domain/operation"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/operationentry"
	"github.com/opensoha/soha/internal/platform/requestctx"
)

type AuditRecorder interface {
	Record(context.Context, domainaudit.Entry) error
}

type OperationRecorder interface {
	Record(context.Context, domainoperation.Entry) error
}

type Service struct {
	repo        domainannouncement.Repository
	permissions *appaccess.PermissionResolver
	audit       AuditRecorder
	operations  OperationRecorder
}

func New(
	repo domainannouncement.Repository,
	permissions *appaccess.PermissionResolver,
	audit AuditRecorder,
	operations OperationRecorder,
) *Service {
	return &Service{
		repo:        repo,
		permissions: permissions,
		audit:       audit,
		operations:  operations,
	}
}

func (s *Service) List(ctx context.Context, principal domainidentity.Principal, limit int) ([]domainannouncement.Record, error) {
	if err := s.authorize(ctx, principal, appaccess.PermSystemAnnouncementsView); err != nil {
		return nil, err
	}
	return s.repo.List(ctx, limit)
}

func (s *Service) Get(ctx context.Context, principal domainidentity.Principal, announcementID string) (domainannouncement.Record, error) {
	if err := s.authorize(ctx, principal, appaccess.PermSystemAnnouncementsView); err != nil {
		return domainannouncement.Record{}, err
	}
	return s.getAnnouncement(ctx, announcementID)
}

func (s *Service) Inbox(ctx context.Context, principal domainidentity.Principal, limit int) (domainannouncement.Inbox, error) {
	if err := s.authorize(ctx, principal, appaccess.PermSystemAnnouncementsView); err != nil {
		return domainannouncement.Inbox{}, err
	}
	if limit <= 0 {
		limit = 10
	}
	return s.repo.ListInbox(ctx, principal.UserID, limit, time.Now().UTC())
}

func (s *Service) MarkRead(ctx context.Context, principal domainidentity.Principal, announcementID string) error {
	if err := s.authorize(ctx, principal, appaccess.PermSystemAnnouncementsView); err != nil {
		return err
	}
	item, err := s.getAnnouncement(ctx, announcementID)
	if err != nil {
		return err
	}
	if !isActiveInboxAnnouncement(item, time.Now().UTC()) {
		return fmt.Errorf("%w: announcement is not available for read receipt", apperrors.ErrInvalidArgument)
	}
	return s.repo.MarkRead(ctx, item.ID, principal.UserID, time.Now().UTC())
}

func (s *Service) Create(ctx context.Context, principal domainidentity.Principal, input domainannouncement.Input) (domainannouncement.Record, error) {
	if err := s.authorize(ctx, principal, appaccess.PermSystemAnnouncementsManage); err != nil {
		return domainannouncement.Record{}, err
	}
	item, err := normalizeInput(input)
	if err != nil {
		return domainannouncement.Record{}, err
	}
	now := time.Now().UTC()
	nowValue := now.Format(time.RFC3339)
	if item.Status == "published" && item.PublishedAt == nil {
		item.PublishedAt = &nowValue
	}
	item.ID = normalizeID(item.ID, item.Title)
	item.CreatedBy = principal.UserID
	item.UpdatedBy = principal.UserID
	item.CreatedAt = nowValue
	item.UpdatedAt = nowValue
	created, err := s.repo.Create(ctx, item)
	if err != nil {
		return domainannouncement.Record{}, err
	}
	s.recordWriteLogs(ctx, principal, "system.announcement.create", created, "success", "created announcement")
	return created, nil
}

func (s *Service) Update(ctx context.Context, principal domainidentity.Principal, announcementID string, input domainannouncement.Input) (domainannouncement.Record, error) {
	if err := s.authorize(ctx, principal, appaccess.PermSystemAnnouncementsManage); err != nil {
		return domainannouncement.Record{}, err
	}
	existing, err := s.getAnnouncement(ctx, announcementID)
	if err != nil {
		return domainannouncement.Record{}, err
	}
	item, err := normalizeInput(input)
	if err != nil {
		return domainannouncement.Record{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	item.ID = strings.TrimSpace(announcementID)
	item.CreatedBy = existing.CreatedBy
	item.CreatedAt = existing.CreatedAt
	item.PublishedAt = existing.PublishedAt
	item.UpdatedBy = principal.UserID
	item.UpdatedAt = now
	updated, err := s.repo.Update(ctx, strings.TrimSpace(announcementID), item)
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			return domainannouncement.Record{}, fmt.Errorf("%w: announcement not found", apperrors.ErrNotFound)
		}
		return domainannouncement.Record{}, err
	}
	s.recordWriteLogs(ctx, principal, "system.announcement.update", updated, "success", "updated announcement")
	return updated, nil
}

func (s *Service) Publish(ctx context.Context, principal domainidentity.Principal, announcementID string) (domainannouncement.Record, error) {
	if err := s.authorize(ctx, principal, appaccess.PermSystemAnnouncementsManage); err != nil {
		return domainannouncement.Record{}, err
	}
	announcementID = strings.TrimSpace(announcementID)
	if announcementID == "" {
		return domainannouncement.Record{}, fmt.Errorf("%w: announcement id is required", apperrors.ErrInvalidArgument)
	}
	published, err := s.repo.Publish(ctx, announcementID, time.Now().UTC(), principal.UserID)
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			return domainannouncement.Record{}, fmt.Errorf("%w: announcement not found", apperrors.ErrNotFound)
		}
		return domainannouncement.Record{}, err
	}
	s.recordWriteLogs(ctx, principal, "system.announcement.publish", published, "success", "published announcement")
	return published, nil
}

func (s *Service) Withdraw(ctx context.Context, principal domainidentity.Principal, announcementID string) (domainannouncement.Record, error) {
	if err := s.authorize(ctx, principal, appaccess.PermSystemAnnouncementsManage); err != nil {
		return domainannouncement.Record{}, err
	}
	announcementID = strings.TrimSpace(announcementID)
	if announcementID == "" {
		return domainannouncement.Record{}, fmt.Errorf("%w: announcement id is required", apperrors.ErrInvalidArgument)
	}
	withdrawn, err := s.repo.Withdraw(ctx, announcementID, time.Now().UTC(), principal.UserID)
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			return domainannouncement.Record{}, fmt.Errorf("%w: announcement not found", apperrors.ErrNotFound)
		}
		return domainannouncement.Record{}, err
	}
	s.recordWriteLogs(ctx, principal, "system.announcement.withdraw", withdrawn, "success", "withdrew announcement")
	return withdrawn, nil
}

func (s *Service) Delete(ctx context.Context, principal domainidentity.Principal, announcementID string) error {
	if err := s.authorize(ctx, principal, appaccess.PermSystemAnnouncementsManage); err != nil {
		return err
	}
	item, err := s.getAnnouncement(ctx, announcementID)
	if err != nil {
		return err
	}
	if err := s.repo.Delete(ctx, item.ID); err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			return fmt.Errorf("%w: announcement not found", apperrors.ErrNotFound)
		}
		return err
	}
	s.recordWriteLogs(ctx, principal, "system.announcement.delete", item, "success", "deleted announcement")
	return nil
}

func normalizeInput(input domainannouncement.Input) (domainannouncement.Record, error) {
	title := strings.TrimSpace(input.Title)
	content := strings.TrimSpace(input.Content)
	if title == "" {
		return domainannouncement.Record{}, fmt.Errorf("%w: title is required", apperrors.ErrInvalidArgument)
	}
	if content == "" {
		return domainannouncement.Record{}, fmt.Errorf("%w: content is required", apperrors.ErrInvalidArgument)
	}
	level := strings.TrimSpace(input.Level)
	if level == "" {
		level = "info"
	}
	status := strings.TrimSpace(input.Status)
	if status == "" {
		status = "draft"
	}
	audience := strings.TrimSpace(input.Audience)
	if audience == "" {
		audience = "all"
	}
	return domainannouncement.Record{
		ID:       strings.TrimSpace(input.ID),
		Title:    title,
		Summary:  strings.TrimSpace(input.Summary),
		Content:  content,
		Level:    level,
		Status:   status,
		Audience: audience,
		Sticky:   input.Sticky,
		StartsAt: input.StartsAt,
		EndsAt:   input.EndsAt,
	}, nil
}

func normalizeID(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value != "" {
		return strings.ToLower(strings.ReplaceAll(value, " ", "-"))
	}
	fallback = strings.TrimSpace(fallback)
	if fallback == "" {
		return uuid.NewString()
	}
	return strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(fallback, " ", "-"), "_", "-"))
}

func isActiveInboxAnnouncement(item domainannouncement.Record, now time.Time) bool {
	if item.Status != "published" || item.PublishedAt == nil || *item.PublishedAt == "" {
		return false
	}
	if item.StartsAt != nil && *item.StartsAt != "" {
		startsAt, err := time.Parse(time.RFC3339, *item.StartsAt)
		if err == nil && startsAt.After(now) {
			return false
		}
	}
	if item.EndsAt != nil && *item.EndsAt != "" {
		endsAt, err := time.Parse(time.RFC3339, *item.EndsAt)
		if err == nil && endsAt.Before(now) {
			return false
		}
	}
	return true
}

func (s *Service) getAnnouncement(ctx context.Context, announcementID string) (domainannouncement.Record, error) {
	item, err := s.repo.Get(ctx, strings.TrimSpace(announcementID))
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			return domainannouncement.Record{}, fmt.Errorf("%w: announcement not found", apperrors.ErrNotFound)
		}
		return domainannouncement.Record{}, err
	}
	return item, nil
}

func (s *Service) recordWriteLogs(ctx context.Context, principal domainidentity.Principal, operationType string, item domainannouncement.Record, result string, summary string) {
	meta := requestctx.FromContext(ctx)
	if s.audit != nil {
		_ = s.audit.Record(ctx, domainaudit.Entry{
			ActorID:       principal.UserID,
			ActorName:     principal.UserName,
			Roles:         principal.Roles,
			Teams:         principal.Teams,
			ResourceKind:  "Announcement",
			ResourceName:  item.Title,
			Action:        strings.TrimPrefix(operationType, "system.announcement."),
			Result:        result,
			Summary:       summary,
			RequestPath:   meta.Path,
			RequestMethod: meta.Method,
			RequestID:     meta.RequestID,
			SourceIP:      meta.SourceIP,
			Metadata: map[string]any{
				"announcementId": item.ID,
				"source":         meta.Source,
			},
		})
	}
	if s.operations != nil {
		_ = s.operations.Record(ctx, operationentry.New(
			ctx,
			principal,
			operationType,
			map[string]any{
				"module":       "system",
				"resourceKind": "Announcement",
				"targetId":     item.ID,
				"targetLabel":  item.Title,
			},
			result,
			summary,
			map[string]any{
				"announcementId": item.ID,
			},
		))
	}
}

func (s *Service) authorize(ctx context.Context, principal domainidentity.Principal, permissionKey string) error {
	return appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, permissionKey)
}
