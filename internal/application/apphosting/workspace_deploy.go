package apphosting

import (
	"context"
	"io"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainapphosting "github.com/opensoha/soha/internal/domain/apphosting"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) DeployFromWorkspace(
	ctx context.Context,
	principal domainidentity.Principal,
	req sohaapi.WorkspaceHostedAppDeploymentRequest,
) (sohaapi.WorkspaceHostedAppDeploymentResult, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveAIChatUse); err != nil {
		return sohaapi.WorkspaceHostedAppDeploymentResult{}, err
	}
	if err := s.authorize(ctx, principal, appaccess.PermAppHostingAppsPublish); err != nil {
		return sohaapi.WorkspaceHostedAppDeploymentResult{}, err
	}
	if strings.TrimSpace(principal.UserID) == "" {
		return sohaapi.WorkspaceHostedAppDeploymentResult{}, apperrors.ErrUnauthorized
	}
	if s.workspaceSource == nil {
		return sohaapi.WorkspaceHostedAppDeploymentResult{}, apperrors.NewBusiness(apperrors.ErrServiceUnavailable, "workspace_bundle_unavailable", "assistant workspace publishing is not configured", "当前未启用个人助手工作空间直接发布能力")
	}
	if err := s.validateWorkspaceDeployRequest(ctx, principal, req); err != nil {
		return sohaapi.WorkspaceHostedAppDeploymentResult{}, err
	}

	bundleRC, inferredTitle, err := s.openWorkspaceSourceBundle(ctx, principal, req.Source)
	if err != nil {
		return sohaapi.WorkspaceHostedAppDeploymentResult{}, err
	}
	defer func() { _ = bundleRC.Close() }()

	idemKey := strings.TrimSpace(req.IdempotencyKey)
	targetAppID, createdNew, err := s.resolveWorkspaceTargetApp(ctx, principal, req, inferredTitle, idemKey)
	if err != nil {
		return sohaapi.WorkspaceHostedAppDeploymentResult{}, err
	}

	uploadKey := deriveChildIdempotencyKey("ws-upload", principal.UserID, idemKey)
	receipt, err := s.UploadRevision(ctx, principal, targetAppID, domainapphosting.UploadRevisionInput{
		IdempotencyKey: uploadKey,
		SpaFallback:    req.SpaFallback,
		Description:    strings.TrimSpace(req.Description),
	}, bundleRC)
	if err != nil {
		return sohaapi.WorkspaceHostedAppDeploymentResult{}, err
	}

	if strings.TrimSpace(req.AppID) != "" && req.Visibility != nil && !receipt.ReusedIdempotencyKey {
		if _, err := s.UpdateVisibility(ctx, principal, targetAppID, sohaapi.UpdateHostedAppVisibilityRequest{
			Visibility: *req.Visibility,
			Reason:     req.VisibilityReason,
		}); err != nil {
			return sohaapi.WorkspaceHostedAppDeploymentResult{}, err
		}
	}

	finalApp, err := s.GetApp(ctx, principal, targetAppID)
	if err != nil {
		return sohaapi.WorkspaceHostedAppDeploymentResult{}, err
	}
	return sohaapi.WorkspaceHostedAppDeploymentResult{
		App:      finalApp,
		Revision: receipt.Revision,
		Created:  createdNew && !receipt.ReusedIdempotencyKey,
	}, nil
}

func (s *Service) validateWorkspaceDeployRequest(
	ctx context.Context,
	principal domainidentity.Principal,
	req sohaapi.WorkspaceHostedAppDeploymentRequest,
) error {
	idemKey := strings.TrimSpace(req.IdempotencyKey)
	if idemKey == "" || len(idemKey) > 128 {
		return apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_idempotency_key", "idempotencyKey must be 1..128 characters", "幂等键长度必须在 1 到 128 个字符之间")
	}
	if !req.Source.Kind.Valid() {
		return apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_workspace_source", "source.kind must be personal_environment or conversation_artifact", "发布来源必须为 personal_environment 或 conversation_artifact")
	}
	if req.Kind != "" && !req.Kind.Valid() {
		return apperrors.ErrInvalidArgument
	}
	if req.Kind == sohaapi.HostedAppKindDynamic && req.Source.Kind != sohaapi.WorkspaceHostedAppSourceKindPersonalEnvironment {
		return apperrors.NewBusiness(apperrors.ErrInvalidArgument, "dynamic_workspace_source_required", "dynamic hosting requires a personal workspace source", "动态应用需要个人工作空间源码目录")
	}
	if utf8.RuneCountInString(strings.TrimSpace(req.VisibilityReason)) > 500 {
		return apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_reason", "visibilityReason must not exceed 500 characters", "公开申请原因不能超过 500 个字符")
	}
	settings, err := s.repo.GetSettings(ctx)
	if err != nil {
		return err
	}
	settings = settings.WithDefaults()
	if !settings.Enabled || settings.StorageIntegrationID == "" || s.objectStore == nil {
		return apperrors.NewBusiness(apperrors.ErrServiceUnavailable, "app_hosting_unavailable", "workspace App Hosting or object storage is not enabled", "工作区应用托管或对象存储尚未启用")
	}
	if req.Visibility != nil {
		if _, err := domainapphosting.NormalizeVisibilityInput(req.Visibility, settings); err != nil {
			return err
		}
	}
	return s.validateExistingTargetAppPreconditions(ctx, principal, req)
}

func (s *Service) validateExistingTargetAppPreconditions(
	ctx context.Context,
	principal domainidentity.Principal,
	req sohaapi.WorkspaceHostedAppDeploymentRequest,
) error {
	targetID := strings.TrimSpace(req.AppID)
	if targetID == "" {
		return nil
	}
	if strings.TrimSpace(req.Slug) != "" {
		return apperrors.NewBusiness(apperrors.ErrInvalidArgument, "slug_immutable", "slug can only be specified when creating a new hosted application", "更新已有应用时不可修改子域标识前缀")
	}
	existing, err := s.repo.GetAppByID(ctx, targetID)
	if err != nil {
		return err
	}
	if !existing.IsOwnerOrMaintainer(principal.UserID) {
		return apperrors.ErrAccessDenied
	}
	if req.Kind != "" && req.Kind != existing.Kind {
		return apperrors.NewBusiness(apperrors.ErrInvalidArgument, "app_kind_immutable", "application kind cannot be changed", "现有应用的类型不能修改")
	}
	if existing.Kind == sohaapi.HostedAppKindDynamic && req.Source.Kind != sohaapi.WorkspaceHostedAppSourceKindPersonalEnvironment {
		return apperrors.NewBusiness(apperrors.ErrInvalidArgument, "dynamic_workspace_source_required", "dynamic hosting requires a personal workspace source", "动态应用需要个人工作空间源码目录")
	}
	if (strings.TrimSpace(req.Name) != "" || req.Visibility != nil) && !existing.IsOwner(principal.UserID) {
		return apperrors.ErrAccessDenied
	}
	return nil
}

func (s *Service) openWorkspaceSourceBundle(
	ctx context.Context,
	principal domainidentity.Principal,
	src sohaapi.WorkspaceHostedAppSource,
) (io.ReadCloser, string, error) {
	switch src.Kind {
	case sohaapi.WorkspaceHostedAppSourceKindPersonalEnvironment:
		if src.EnvironmentVersion < 0 || len(strings.TrimSpace(src.AssistantID)) > 128 || len(src.SourcePath) > 512 {
			return nil, "", apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_workspace_source", "personal_environment source parameters are invalid", "个人工作空间发布参数不合法")
		}
		rc, err := s.workspaceSource.BundlePersonalWorkspace(ctx, principal, src.SourcePath, src.AssistantID, src.EnvironmentVersion)
		return rc, inferWorkspaceAppName(src.SourcePath), err
	case sohaapi.WorkspaceHostedAppSourceKindConversationArtifact:
		artID := strings.TrimSpace(src.ArtifactID)
		if artID == "" || len(artID) > 128 || len(strings.TrimSpace(src.SessionID)) > 128 || len(strings.TrimSpace(src.EntryFileName)) > 128 {
			return nil, "", apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_workspace_source", "conversation_artifact requires a valid artifactId", "对话成果发布必须指定有效的 artifactId")
		}
		return s.workspaceSource.BundleConversationArtifact(ctx, principal, src.SessionID, artID, src.EntryFileName)
	default:
		return nil, "", apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_workspace_source", "unsupported workspace source kind", "不支持的发布来源类型")
	}
}

func (s *Service) resolveWorkspaceTargetApp(
	ctx context.Context,
	principal domainidentity.Principal,
	req sohaapi.WorkspaceHostedAppDeploymentRequest,
	inferredTitle, idemKey string,
) (string, bool, error) {
	if existingID := strings.TrimSpace(req.AppID); existingID != "" {
		if trimmedName := strings.TrimSpace(req.Name); trimmedName != "" {
			if _, err := s.UpdateApp(ctx, principal, existingID, sohaapi.UpdateHostedAppRequest{Name: &trimmedName}); err != nil {
				return "", false, err
			}
		}
		return existingID, false, nil
	}
	createKey := deriveChildIdempotencyKey("ws-create", principal.UserID, idemKey)
	appName := firstNonEmpty(req.Name, inferredTitle, "Workspace App")
	kind := req.Kind
	if kind == "" {
		kind = sohaapi.HostedAppKindStatic
	}
	created, reused, err := s.createAppWithReason(ctx, principal, sohaapi.CreateHostedAppRequest{
		Name:           appName,
		Slug:           req.Slug,
		Description:    req.Description,
		Kind:           kind,
		SpaFallback:    req.SpaFallback,
		Visibility:     req.Visibility,
		IdempotencyKey: createKey,
	}, req.VisibilityReason)
	if err != nil {
		return "", false, err
	}
	return created.ID, !reused, nil
}

func deriveChildIdempotencyKey(stage, userID, parentKey string) string {
	digest := sha256HexString(stage + ":" + strings.TrimSpace(userID) + ":" + strings.TrimSpace(parentKey))
	return stage[:3] + "_" + digest[:48]
}

func inferWorkspaceAppName(sourcePath string) string {
	cleaned := strings.Trim(strings.TrimSpace(sourcePath), "./")
	if cleaned == "" {
		return "Personal Workspace App"
	}
	parts := strings.Split(cleaned, "/")
	base := parts[len(parts)-1]
	if (base == "dist" || base == "build" || base == "public" || base == "index.html") && len(parts) >= 2 {
		base = parts[len(parts)-2]
	} else if ext := path.Ext(base); ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	base = strings.TrimSpace(base)
	if base == "" || base == "." {
		return "Personal Workspace App"
	}
	return base
}
