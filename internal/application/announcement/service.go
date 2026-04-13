package announcement

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	domainannouncement "github.com/kubecrux/kubecrux/internal/domain/announcement"
	domainidentity "github.com/kubecrux/kubecrux/internal/domain/identity"
	"github.com/kubecrux/kubecrux/internal/platform/apperrors"
	"gorm.io/gorm"
)

type Service struct {
	repo domainannouncement.Repository
}

func New(repo domainannouncement.Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) List(ctx context.Context, principal domainidentity.Principal, limit int) ([]domainannouncement.Record, error) {
	if err := ensureAdmin(principal); err != nil {
		return nil, err
	}
	return s.repo.List(ctx, limit)
}

func (s *Service) Get(ctx context.Context, principal domainidentity.Principal, announcementID string) (domainannouncement.Record, error) {
	if err := ensureAdmin(principal); err != nil {
		return domainannouncement.Record{}, err
	}
	item, err := s.repo.Get(ctx, strings.TrimSpace(announcementID))
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return domainannouncement.Record{}, fmt.Errorf("%w: announcement not found", apperrors.ErrNotFound)
		}
		return domainannouncement.Record{}, err
	}
	return item, nil
}

func (s *Service) Create(ctx context.Context, principal domainidentity.Principal, input domainannouncement.Input) (domainannouncement.Record, error) {
	if err := ensureAdmin(principal); err != nil {
		return domainannouncement.Record{}, err
	}
	item, err := normalizeInput(input)
	if err != nil {
		return domainannouncement.Record{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if item.Status == "published" {
		item.PublishedAt = &now
	}
	item.ID = normalizeID(item.ID, item.Title)
	item.CreatedBy = principal.UserID
	item.UpdatedBy = principal.UserID
	item.CreatedAt = now
	item.UpdatedAt = now
	return s.repo.Create(ctx, item)
}

func (s *Service) Update(ctx context.Context, principal domainidentity.Principal, announcementID string, input domainannouncement.Input) (domainannouncement.Record, error) {
	if err := ensureAdmin(principal); err != nil {
		return domainannouncement.Record{}, err
	}
	item, err := normalizeInput(input)
	if err != nil {
		return domainannouncement.Record{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if item.Status == "published" {
		item.PublishedAt = &now
	}
	item.ID = strings.TrimSpace(announcementID)
	item.UpdatedBy = principal.UserID
	item.UpdatedAt = now
	updated, err := s.repo.Update(ctx, strings.TrimSpace(announcementID), item)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return domainannouncement.Record{}, fmt.Errorf("%w: announcement not found", apperrors.ErrNotFound)
		}
		return domainannouncement.Record{}, err
	}
	return updated, nil
}

func (s *Service) Delete(ctx context.Context, principal domainidentity.Principal, announcementID string) error {
	if err := ensureAdmin(principal); err != nil {
		return err
	}
	if strings.TrimSpace(announcementID) == "" {
		return fmt.Errorf("%w: announcement id is required", apperrors.ErrInvalidArgument)
	}
	if err := s.repo.Delete(ctx, strings.TrimSpace(announcementID)); err != nil {
		if err == gorm.ErrRecordNotFound {
			return fmt.Errorf("%w: announcement not found", apperrors.ErrNotFound)
		}
		return err
	}
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

func ensureAdmin(principal domainidentity.Principal) error {
	for _, role := range principal.Roles {
		if role == "admin" {
			return nil
		}
	}
	return fmt.Errorf("%w: admin role required", apperrors.ErrAccessDenied)
}
