package apphosting

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainapphosting "github.com/opensoha/soha/internal/domain/apphosting"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const maxMaintainersPerApp = 20

func (s *Service) UpdateMaintainers(
	ctx context.Context,
	principal domainidentity.Principal,
	appID string,
	req sohaapi.UpdateHostedAppMaintainersRequest,
) (sohaapi.HostedApp, error) {
	if err := s.authorize(ctx, principal, appaccess.PermAppHostingAppsPublish); err != nil {
		return sohaapi.HostedApp{}, err
	}
	trimmedAppID, err := validateIdentifier(appID, "appId")
	if err != nil {
		return sohaapi.HostedApp{}, err
	}
	cleaned, err := sanitizeMaintainerUserIDs(req.MaintainerUserIDs)
	if err != nil {
		return sohaapi.HostedApp{}, err
	}
	app, err := s.repo.GetAppByID(ctx, trimmedAppID)
	if err != nil {
		return sohaapi.HostedApp{}, err
	}
	if !app.IsOwner(principal.UserID) {
		return sohaapi.HostedApp{}, apperrors.ErrAccessDenied
	}
	if slices.Contains(cleaned, app.OwnerUserID) {
		return sohaapi.HostedApp{}, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "owner_cannot_be_maintainer", "application owner cannot be added as a collaborator maintainer", "应用归属人不能同时加入协作者列表")
	}
	if err := s.verifyActiveUsers(ctx, cleaned, "invalid_maintainer_user"); err != nil {
		return sohaapi.HostedApp{}, err
	}

	settings, err := s.repo.GetSettings(ctx)
	if err != nil {
		return sohaapi.HostedApp{}, err
	}
	now := s.now()
	updated, err := s.repo.MutateApp(ctx, trimmedAppID, func(current domainapphosting.App) (domainapphosting.App, error) {
		if !current.IsOwner(principal.UserID) {
			return domainapphosting.App{}, apperrors.ErrAccessDenied
		}
		if slices.Contains(cleaned, current.OwnerUserID) {
			return domainapphosting.App{}, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "owner_cannot_be_maintainer", "application owner cannot be added as a collaborator maintainer", "应用归属人不能同时加入协作者列表")
		}
		next := current
		next.MaintainerUserIDs = slices.Clone(cleaned)
		next.UpdatedAt = now
		return next, nil
	})
	if err != nil {
		return sohaapi.HostedApp{}, err
	}
	s.recordAudit(ctx, principal, "app.maintainers.update", "success", updated.ID, "Updated hosted application collaborator maintainers", map[string]any{
		"appId":             updated.ID,
		"maintainerUserIds": updated.MaintainerUserIDs,
	})
	return s.enrichHostedApp(ctx, settings, updated)
}

func sanitizeMaintainerUserIDs(rawIDs []string) ([]string, error) {
	if rawIDs == nil {
		return nil, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "missing_maintainers", "maintainerUserIds array is required", "必须提供 maintainerUserIds 列表")
	}
	out := make([]string, 0, len(rawIDs))
	for _, raw := range rawIDs {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || len(trimmed) > 128 {
			return nil, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_maintainer_id", "maintainerUserIds entries must be 1..128 non-empty characters", "协作者标识不能为空或超过 128 个字符")
		}
		if !slices.Contains(out, trimmed) {
			out = append(out, trimmed)
		}
	}
	if len(out) > maxMaintainersPerApp {
		return nil, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "too_many_maintainers", fmt.Sprintf("at most %d collaborator maintainers are allowed per application", maxMaintainersPerApp), fmt.Sprintf("单个应用最多支持添加 %d 位协作维护人", maxMaintainersPerApp))
	}
	return out, nil
}

func (s *Service) verifyActiveUsers(ctx context.Context, userIDs []string, errCode string) error {
	if s.audience == nil {
		return nil
	}
	for _, uid := range userIDs {
		_, active, err := s.audience.ResolveUserPrincipal(ctx, uid)
		if errors.Is(err, apperrors.ErrNotFound) || (err == nil && !active) {
			return apperrors.NewBusiness(
				apperrors.ErrInvalidArgument,
				errCode,
				fmt.Sprintf("user %q does not exist or is inactive", uid),
				fmt.Sprintf("用户 %q 不存在或已停用", uid),
			)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) TransferOwnership(
	ctx context.Context,
	principal domainidentity.Principal,
	appID string,
	req sohaapi.TransferHostedAppOwnershipRequest,
) (sohaapi.HostedApp, error) {
	if err := s.authorize(ctx, principal, appaccess.PermAppHostingAdminManage); err != nil {
		return sohaapi.HostedApp{}, err
	}
	trimmedAppID, err := validateIdentifier(appID, "appId")
	if err != nil {
		return sohaapi.HostedApp{}, err
	}
	newOwnerID, err := validateIdentifier(req.NewOwnerUserID, "newOwnerUserId")
	if err != nil {
		return sohaapi.HostedApp{}, err
	}
	trimmedReason := strings.TrimSpace(req.Reason)
	if utf8.RuneCountInString(trimmedReason) > 500 {
		return sohaapi.HostedApp{}, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_reason", "reason must not exceed 500 characters", "转交原因长度不能超过 500 个字符")
	}
	if err := s.verifyActiveUsers(ctx, []string{newOwnerID}, "invalid_new_owner"); err != nil {
		return sohaapi.HostedApp{}, err
	}
	settings, err := s.repo.GetSettings(ctx)
	if err != nil {
		return sohaapi.HostedApp{}, err
	}
	updated, previousOwnerID, err := s.repo.TransferAppOwnership(ctx, trimmedAppID, newOwnerID, s.now())
	if err != nil {
		return sohaapi.HostedApp{}, err
	}
	s.recordAudit(ctx, principal, "app.transfer", "success", updated.ID, "Transferred hosted application ownership", map[string]any{
		"appId":               updated.ID,
		"previousOwnerUserId": previousOwnerID,
		"newOwnerUserId":      newOwnerID,
		"reason":              trimmedReason,
		"routeGeneration":     updated.RouteGeneration,
	})
	return s.enrichHostedApp(ctx, settings, updated)
}

func (s *Service) DeleteApp(
	ctx context.Context,
	principal domainidentity.Principal,
	appID string,
	req sohaapi.DeleteHostedAppRequest,
) (sohaapi.DeleteHostedAppResult, error) {
	isAdmin := s.authorize(ctx, principal, appaccess.PermAppHostingAdminManage) == nil
	if !isAdmin {
		if err := s.authorize(ctx, principal, appaccess.PermAppHostingAppsPublish); err != nil {
			return sohaapi.DeleteHostedAppResult{}, err
		}
	}
	trimmedAppID, err := validateIdentifier(appID, "appId")
	if err != nil {
		return sohaapi.DeleteHostedAppResult{}, err
	}
	confirmName := strings.TrimSpace(req.ConfirmationName)
	if confirmName == "" || utf8.RuneCountInString(confirmName) > 128 {
		return sohaapi.DeleteHostedAppResult{}, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_confirmation_name", "confirmationName must be 1..128 characters", "确认名称长度必须在 1 到 128 个字符之间")
	}
	trimmedReason := strings.TrimSpace(req.Reason)
	if utf8.RuneCountInString(trimmedReason) > 500 {
		return sohaapi.DeleteHostedAppResult{}, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_reason", "reason must not exceed 500 characters", "删除原因长度不能超过 500 个字符")
	}

	deletedApp, revisions, err := s.repo.DeleteApp(ctx, trimmedAppID, func(current domainapphosting.App) error {
		if !isAdmin && !current.IsOwner(principal.UserID) {
			return apperrors.ErrAccessDenied
		}
		if confirmName != current.Name {
			return apperrors.NewBusiness(apperrors.ErrConflict, "confirmation_name_mismatch", "confirmationName must exactly match the application name", "确认应用名称不匹配")
		}
		return nil
	})
	if err != nil {
		return sohaapi.DeleteHostedAppResult{}, err
	}

	meta := map[string]any{
		"appId":       deletedApp.ID,
		"dnsLabel":    deletedApp.DNSLabel,
		"ownerUserId": deletedApp.OwnerUserID,
		"reason":      trimmedReason,
	}
	if deletedApp.Kind == sohaapi.HostedAppKindDynamic {
		meta["runtimeCleanupMode"] = "background"
	} else {
		purgedObjects, cleanupErr := s.purgeDeletedAppRevisions(ctx, revisions)
		meta["purgedObjectCount"] = purgedObjects
		if cleanupErr != nil {
			meta["storageCleanupError"] = cleanupErr.Error()
		}
	}
	s.recordAudit(ctx, principal, "app.delete", "success", deletedApp.ID, "Permanently deleted hosted application", meta)
	return sohaapi.DeleteHostedAppResult{AppID: deletedApp.ID, Deleted: true}, nil
}

func (s *Service) purgeDeletedAppRevisions(ctx context.Context, revisions []domainapphosting.Revision) (int, error) {
	if s.objectStore == nil || len(revisions) == 0 {
		return 0, nil
	}
	byIntegration := make(map[string][]string)
	totalKeys := 0
	for _, rev := range revisions {
		integrationID := strings.TrimSpace(rev.StorageIntegrationID)
		if integrationID == "" {
			continue
		}
		keys := buildRevisionStoreKeys(rev)
		if len(keys) > 0 {
			byIntegration[integrationID] = append(byIntegration[integrationID], keys...)
			totalKeys += len(keys)
		}
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	var firstErr error
	for integrationID, keys := range byIntegration {
		if err := s.objectStore.DeleteKeys(cleanupCtx, integrationID, keys); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return totalKeys, firstErr
}

func (s *Service) WithdrawVisibilityApproval(
	ctx context.Context,
	principal domainidentity.Principal,
	appID, reason string,
) (sohaapi.HostedApp, error) {
	if err := s.authorize(ctx, principal, appaccess.PermAppHostingAppsPublish); err != nil {
		return sohaapi.HostedApp{}, err
	}
	trimmedAppID, trimmedReason, err := validateLifecycleArgs(appID, reason, false)
	if err != nil {
		return sohaapi.HostedApp{}, err
	}
	settings, err := s.repo.GetSettings(ctx)
	if err != nil {
		return sohaapi.HostedApp{}, err
	}
	app, withdrawn, err := s.repo.WithdrawVisibilityApproval(ctx, trimmedAppID, strings.TrimSpace(principal.UserID), s.now())
	if err != nil {
		return sohaapi.HostedApp{}, err
	}
	s.recordAudit(ctx, principal, "app.visibility.withdraw_approval", "success", app.ID, "Withdrew pending organization visibility approval request", map[string]any{
		"appId":      app.ID,
		"approvalId": withdrawn.ID,
		"reason":     trimmedReason,
	})
	return s.enrichHostedApp(ctx, settings, app)
}

func (s *Service) ListVisibilityApprovals(
	ctx context.Context,
	principal domainidentity.Principal,
	params sohaapi.ListHostedAppVisibilityApprovalsParams,
) (sohaapi.HostedAppVisibilityApprovalList, error) {
	filter, err := s.authorizeAndNormalizeApprovalFilter(ctx, principal, params)
	if err != nil {
		return sohaapi.HostedAppVisibilityApprovalList{}, err
	}
	approvals, total, err := s.repo.ListVisibilityApprovals(ctx, filter)
	if err != nil {
		return sohaapi.HostedAppVisibilityApprovalList{}, err
	}
	requesterIDs := make([]string, 0, len(approvals))
	for _, item := range approvals {
		if !slices.Contains(requesterIDs, item.RequesterUserID) {
			requesterIDs = append(requesterIDs, item.RequesterUserID)
		}
	}
	summaries, err := s.resolveSummariesMap(ctx, requesterIDs)
	if err != nil {
		return sohaapi.HostedAppVisibilityApprovalList{}, err
	}
	items := make([]sohaapi.HostedAppVisibilityApproval, 0, len(approvals))
	for _, item := range approvals {
		var profile *sohaapi.HostedAppUserSummary
		if summary, ok := summaries[item.RequesterUserID]; ok {
			cloned := domainapphosting.CloneUserSummary(summary)
			profile = &cloned
		}
		items = append(items, item.ToAPI(profile))
	}
	return sohaapi.HostedAppVisibilityApprovalList{Items: items, Total: total}, nil
}

func (s *Service) authorizeAndNormalizeApprovalFilter(
	ctx context.Context,
	principal domainidentity.Principal,
	params sohaapi.ListHostedAppVisibilityApprovalsParams,
) (domainapphosting.VisibilityApprovalFilter, error) {
	scope := params.Scope
	if scope == "" {
		scope = sohaapi.HostedAppApprovalListScopeMine
	}
	if !scope.Valid() {
		return domainapphosting.VisibilityApprovalFilter{}, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_scope", "scope must be mine or all", "审批查询范围必须为 mine 或 all")
	}
	requesterID := ""
	if scope == sohaapi.HostedAppApprovalListScopeAll {
		if err := s.authorizeAny(ctx, principal, appaccess.PermAppHostingAdminView, appaccess.PermAppHostingAdminManage); err != nil {
			return domainapphosting.VisibilityApprovalFilter{}, err
		}
	} else {
		if err := s.authorizeAny(ctx, principal,
			appaccess.PermAppHostingAppsView,
			appaccess.PermAppHostingAppsPublish,
			appaccess.PermAppHostingAdminView,
			appaccess.PermAppHostingAdminManage,
		); err != nil {
			return domainapphosting.VisibilityApprovalFilter{}, err
		}
		requesterID = strings.TrimSpace(principal.UserID)
	}
	if params.Status != "" && !params.Status.Valid() {
		return domainapphosting.VisibilityApprovalFilter{}, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_approval_status", "status filter is invalid", "审批状态过滤参数无效")
	}
	trimmedAppID := strings.TrimSpace(params.AppID)
	if len(trimmedAppID) > 128 {
		return domainapphosting.VisibilityApprovalFilter{}, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_appid", "appId must not exceed 128 characters", "应用标识过长")
	}
	limit := params.Limit
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 200 || params.Offset < 0 {
		return domainapphosting.VisibilityApprovalFilter{}, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_pagination", "limit must be 1..200 and offset >= 0", "分页参数无效")
	}
	return domainapphosting.VisibilityApprovalFilter{
		Scope:           scope,
		RequesterUserID: requesterID,
		Status:          params.Status,
		AppID:           trimmedAppID,
		Limit:           limit,
		Offset:          params.Offset,
	}, nil
}

func (s *Service) ApproveVisibilityApproval(
	ctx context.Context,
	principal domainidentity.Principal,
	approvalID, comment string,
) (sohaapi.HostedAppVisibilityApprovalDecisionResult, error) {
	if err := s.authorize(ctx, principal, appaccess.PermAppHostingAdminManage); err != nil {
		return sohaapi.HostedAppVisibilityApprovalDecisionResult{}, err
	}
	trimmedApprovalID, err := validateIdentifier(approvalID, "approvalId")
	if err != nil {
		return sohaapi.HostedAppVisibilityApprovalDecisionResult{}, err
	}
	trimmedComment := strings.TrimSpace(comment)
	if utf8.RuneCountInString(trimmedComment) > 500 {
		return sohaapi.HostedAppVisibilityApprovalDecisionResult{}, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_review_comment", "comment must not exceed 500 characters", "审批意见长度不能超过 500 个字符")
	}
	reviewerID := strings.TrimSpace(principal.UserID)
	if reviewerID == "" {
		return sohaapi.HostedAppVisibilityApprovalDecisionResult{}, apperrors.ErrUnauthorized
	}

	app, approval, err := s.repo.ApproveVisibilityApproval(ctx, trimmedApprovalID, reviewerID, trimmedComment, s.now(), s.verifyApprovalRequesterEligible)
	if err != nil {
		return sohaapi.HostedAppVisibilityApprovalDecisionResult{}, err
	}
	s.recordAudit(ctx, principal, "app.visibility.approve", "success", app.ID, "Approved organization-wide visibility for hosted application", map[string]any{
		"appId":           app.ID,
		"approvalId":      approval.ID,
		"requesterUserId": approval.RequesterUserID,
		"routeGeneration": app.RouteGeneration,
		"comment":         trimmedComment,
	})
	return s.buildApprovalDecisionResult(ctx, app, approval)
}

func (s *Service) verifyApprovalRequesterEligible(ctx context.Context, requesterUserID string) error {
	reqPrincipal := domainidentity.Principal{UserID: requesterUserID}
	if s.audience != nil {
		resolved, active, err := s.audience.ResolveUserPrincipal(ctx, requesterUserID)
		if errors.Is(err, apperrors.ErrNotFound) || (err == nil && !active) {
			return apperrors.NewBusiness(apperrors.ErrConflict, "requester_ineligible", "requester account is inactive or no longer exists", "申请人账号已停用或不存在，无法批准该申请")
		}
		if err != nil {
			return err
		}
		reqPrincipal = resolved
	}
	if err := s.authorize(ctx, reqPrincipal, appaccess.PermAppHostingAppsPublish); err != nil {
		return apperrors.NewBusiness(apperrors.ErrConflict, "requester_ineligible", "requester no longer holds publish permission", "申请人当前已不具备应用发布权限，无法批准该申请")
	}
	return nil
}

func (s *Service) RejectVisibilityApproval(
	ctx context.Context,
	principal domainidentity.Principal,
	approvalID, comment string,
) (sohaapi.HostedAppVisibilityApprovalDecisionResult, error) {
	if err := s.authorize(ctx, principal, appaccess.PermAppHostingAdminManage); err != nil {
		return sohaapi.HostedAppVisibilityApprovalDecisionResult{}, err
	}
	trimmedApprovalID, err := validateIdentifier(approvalID, "approvalId")
	if err != nil {
		return sohaapi.HostedAppVisibilityApprovalDecisionResult{}, err
	}
	trimmedComment := strings.TrimSpace(comment)
	if trimmedComment == "" || utf8.RuneCountInString(trimmedComment) > 500 {
		return sohaapi.HostedAppVisibilityApprovalDecisionResult{}, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_review_comment", "comment is required and must be 1..500 characters", "拒绝公开申请必须填写 1 到 500 字的审批意见")
	}
	reviewerID := strings.TrimSpace(principal.UserID)
	if reviewerID == "" {
		return sohaapi.HostedAppVisibilityApprovalDecisionResult{}, apperrors.ErrUnauthorized
	}

	app, approval, err := s.repo.RejectVisibilityApproval(ctx, trimmedApprovalID, reviewerID, trimmedComment, s.now())
	if err != nil {
		return sohaapi.HostedAppVisibilityApprovalDecisionResult{}, err
	}
	s.recordAudit(ctx, principal, "app.visibility.reject", "success", app.ID, "Rejected organization-wide visibility request for hosted application", map[string]any{
		"appId":           app.ID,
		"approvalId":      approval.ID,
		"requesterUserId": approval.RequesterUserID,
		"comment":         trimmedComment,
	})
	return s.buildApprovalDecisionResult(ctx, app, approval)
}

func (s *Service) buildApprovalDecisionResult(
	ctx context.Context,
	app domainapphosting.App,
	approval domainapphosting.VisibilityApproval,
) (sohaapi.HostedAppVisibilityApprovalDecisionResult, error) {
	settings, err := s.repo.GetSettings(ctx)
	if err != nil {
		return sohaapi.HostedAppVisibilityApprovalDecisionResult{}, err
	}
	appDTO, err := s.enrichHostedApp(ctx, settings, app)
	if err != nil {
		return sohaapi.HostedAppVisibilityApprovalDecisionResult{}, err
	}
	summaries, err := s.resolveSummariesMap(ctx, []string{approval.RequesterUserID})
	if err != nil {
		return sohaapi.HostedAppVisibilityApprovalDecisionResult{}, err
	}
	var reqProfile *sohaapi.HostedAppUserSummary
	if summary, ok := summaries[approval.RequesterUserID]; ok {
		cloned := domainapphosting.CloneUserSummary(summary)
		reqProfile = &cloned
	}
	return sohaapi.HostedAppVisibilityApprovalDecisionResult{
		App:      appDTO,
		Approval: approval.ToAPI(reqProfile),
	}, nil
}
