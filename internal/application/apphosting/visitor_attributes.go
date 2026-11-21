package apphosting

import (
	"context"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	"github.com/opensoha/soha/internal/application/access"
	domain "github.com/opensoha/soha/internal/domain/apphosting"
	"github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) GetVisitorAttributes(ctx context.Context, actor identity.Principal, id string) (sohaapi.HostedAppVisitorAttributes, error) {
	app, _, err := s.loadAuthorizedAppRead(ctx, actor, id)
	if err != nil {
		return sohaapi.HostedAppVisitorAttributes{}, err
	}
	if !app.IsOwner(actor.UserID) && s.authorize(ctx, actor, access.PermAppHostingAdminView) != nil &&
		s.authorize(ctx, actor, access.PermAppHostingAdminManage) != nil {
		return sohaapi.HostedAppVisitorAttributes{}, apperrors.ErrAccessDenied
	}
	repo, ok := s.repo.(runtimeConfigRepository)
	if !ok {
		return sohaapi.HostedAppVisitorAttributes{}, apperrors.ErrServiceUnavailable
	}
	cfg, err := repo.GetRuntimeConfig(ctx, app.ID)
	return visitorAttributesDTO(cfg, app.OwnerUserID), err
}

func (s *Service) RequestVisitorAttributes(ctx context.Context, actor identity.Principal, id string, req sohaapi.RequestHostedAppVisitorAttributesRequest) (sohaapi.HostedAppVisitorAttributes, error) {
	if err := s.authorize(ctx, actor, access.PermAppHostingAppsPublish); err != nil {
		return sohaapi.HostedAppVisitorAttributes{}, err
	}
	fields, err := visitorAttributeFields(req.Fields)
	reason := strings.TrimSpace(req.Reason)
	if err != nil || len(fields) == 0 || reason == "" || utf8.RuneCountInString(reason) > 500 {
		return sohaapi.HostedAppVisitorAttributes{}, apperrors.ErrInvalidArgument
	}
	return s.mutateVisitorAttributes(ctx, actor, id, "visitor.attributes.request", fields, func(app domain.App, cfg domain.RuntimeConfig) (domain.RuntimeConfig, error) {
		if !app.IsOwner(actor.UserID) {
			return cfg, apperrors.ErrAccessDenied
		}
		if app.Status == sohaapi.HostedAppStatusSuspendedByAdmin ||
			(cfg.VisitorRequest != nil && cfg.VisitorRequest.OwnerID == actor.UserID) {
			return cfg, apperrors.ErrConflict
		}
		cfg.VisitorRequest = &domain.VisitorAttributeRequest{
			ID: "var_" + uuid.NewString(), OwnerID: actor.UserID, Reason: reason, Fields: fields, RequestedAt: s.now(),
		}
		return cfg, nil
	})
}

func (s *Service) DecideVisitorAttributes(ctx context.Context, actor identity.Principal, id string, req sohaapi.DecideHostedAppVisitorAttributesRequest) (sohaapi.HostedAppVisitorAttributes, error) {
	if err := s.authorize(ctx, actor, access.PermAppHostingAdminManage); err != nil {
		return sohaapi.HostedAppVisitorAttributes{}, err
	}
	fields, err := visitorAttributeFields(req.Fields)
	requestID := strings.TrimSpace(req.RequestID)
	if err != nil || len(requestID) > 128 || (len(fields) > 0 && requestID == "") || req.Fields == nil {
		return sohaapi.HostedAppVisitorAttributes{}, apperrors.ErrInvalidArgument
	}
	return s.mutateVisitorAttributes(ctx, actor, id, "visitor.attributes.review", fields, func(app domain.App, cfg domain.RuntimeConfig) (domain.RuntimeConfig, error) {
		return decideVisitorGrant(actor.UserID, app, cfg, requestID, fields)
	})
}

func decideVisitorGrant(actorID string, app domain.App, cfg domain.RuntimeConfig, requestID string, fields []string) (domain.RuntimeConfig, error) {
	if requestID == "" {
		cfg.VisitorGrant = domain.VisitorGrant{}
		cfg.VisitorRequest = nil
		return cfg, nil
	}
	request := cfg.VisitorRequest
	if request == nil || request.ID != requestID || request.OwnerID != app.OwnerUserID {
		return cfg, apperrors.ErrConflict
	}
	if request.OwnerID == actorID {
		return cfg, apperrors.ErrAccessDenied
	}
	if len(fields) > 0 {
		if app.Status == sohaapi.HostedAppStatusSuspendedByAdmin {
			return cfg, apperrors.ErrConflict
		}
		for _, field := range fields {
			if !slices.Contains(request.Fields, field) {
				return cfg, apperrors.ErrInvalidArgument
			}
		}
		cfg.VisitorGrant = domain.VisitorGrant{OwnerID: app.OwnerUserID, Fields: fields}
	}
	cfg.VisitorRequest = nil
	return cfg, nil
}

func (s *Service) mutateVisitorAttributes(ctx context.Context, actor identity.Principal, id, action string, fields []string, update func(domain.App, domain.RuntimeConfig) (domain.RuntimeConfig, error)) (sohaapi.HostedAppVisitorAttributes, error) {
	repo, ok := s.repo.(runtimeConfigRepository)
	if !ok {
		return sohaapi.HostedAppVisitorAttributes{}, apperrors.ErrServiceUnavailable
	}
	id, err := validateIdentifier(id, "appId")
	if err != nil {
		return sohaapi.HostedAppVisitorAttributes{}, err
	}
	var owner string
	cfg, err := repo.MutateRuntimeConfig(ctx, id, func(app domain.App, current domain.RuntimeConfig) (domain.RuntimeConfig, error) {
		owner = app.OwnerUserID
		return update(app, current)
	})
	if err != nil {
		return sohaapi.HostedAppVisitorAttributes{}, err
	}
	result := visitorAttributesDTO(cfg, owner)
	s.recordAudit(ctx, actor, action, "success", id, "Updated hosted application visitor attribute governance", map[string]any{
		"fields": fields, "granted": result.Granted, "pending": result.Pending != nil,
	})
	return result, nil
}

func visitorAttributeFields(fields []sohaapi.HostedAppVisitorAttribute) ([]string, error) {
	if len(fields) > 2 {
		return nil, apperrors.ErrInvalidArgument
	}
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		if !field.Valid() || slices.Contains(out, string(field)) {
			return nil, apperrors.ErrInvalidArgument
		}
		out = append(out, string(field))
	}
	return out, nil
}

func visitorAttributesDTO(cfg domain.RuntimeConfig, ownerID string) sohaapi.HostedAppVisitorAttributes {
	out := sohaapi.HostedAppVisitorAttributes{Granted: []sohaapi.HostedAppVisitorAttribute{}}
	if ownerID != "" && cfg.VisitorGrant.OwnerID == ownerID {
		out.Granted = toVisitorAttributeEnums(cfg.VisitorGrant.Fields)
	}
	if req := cfg.VisitorRequest; req != nil && req.OwnerID == ownerID {
		out.Pending = &sohaapi.HostedAppVisitorAttributeRequest{
			ID: req.ID, Fields: toVisitorAttributeEnums(req.Fields), Reason: req.Reason, RequestedAt: req.RequestedAt,
		}
	}
	return out
}

func toVisitorAttributeEnums(fields []string) []sohaapi.HostedAppVisitorAttribute {
	out := make([]sohaapi.HostedAppVisitorAttribute, 0, len(fields))
	for _, field := range fields {
		value := sohaapi.HostedAppVisitorAttribute(field)
		if value.Valid() {
			out = append(out, value)
		}
	}
	return out
}

func (s *Service) addOptionalVisitorIdentity(ctx context.Context, app domain.App, visitor domain.VisitorProfile, out *sohaapi.HostedAppVisitorIdentity) {
	repo, ok := s.repo.(runtimeConfigRepository)
	if !ok {
		return
	}
	config, err := repo.GetRuntimeConfig(ctx, app.ID)
	if err != nil || config.VisitorGrant.OwnerID != app.OwnerUserID || app.OwnerUserID == "" {
		return
	}
	if slices.Contains(config.VisitorGrant.Fields, "email") {
		out.Email = safeVisitorAttribute(visitor.Email, 320)
	}
	if slices.Contains(config.VisitorGrant.Fields, "department") {
		out.Department = safeVisitorAttribute(visitor.Department, 256)
	}
}

func safeVisitorAttribute(value string, maxBytes int) string {
	if len(value) > maxBytes || !utf8.ValidString(value) || strings.ContainsFunc(value, unicode.IsControl) {
		return ""
	}
	return strings.TrimSpace(value)
}
