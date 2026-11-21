package apphosting

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	appapphosting "github.com/opensoha/soha/internal/application/apphosting"
	domainapphosting "github.com/opensoha/soha/internal/domain/apphosting"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

const visibilityApprovalColumns = `id, app_id, app_name, app_dns_label, requester_user_id,
	current_visibility, requested_visibility, base_route_generation, policy_version,
	status, reason, reviewer_user_id, review_comment, invalidation_reason,
	created_at, updated_at, decided_at`

func rejectUnactivatableRevisionTx(tx *gorm.DB, app domainapphosting.App, input appapphosting.ActivateRevisionInput) error {
	if trimmedActor := strings.TrimSpace(input.ActorUserID); trimmedActor != "" && !app.IsOwnerOrMaintainer(trimmedActor) {
		_ = tx.Exec(`
			UPDATE public.hosted_app_revisions
			SET status = 'failed', failure_stage = 'activate', failure_reason = 'uploader lost owner or maintainer permission before activation'
			WHERE app_id = ? AND id = ?
		`, input.AppID, input.RevisionID).Error
		return apperrors.NewBusiness(apperrors.ErrAccessDenied, "publisher_permission_revoked", "uploader is no longer an authorized owner or maintainer of this application", "上传期间您的作者或协作者权限已变更，本次版本激活已取消")
	}
	if app.Status == sohaapi.HostedAppStatusOffline || app.Status == sohaapi.HostedAppStatusSuspendedByAdmin {
		_ = tx.Exec(`
			UPDATE public.hosted_app_revisions
			SET status = 'failed', failure_stage = 'activate', failure_reason = 'application became offline or suspended before activation'
			WHERE app_id = ? AND id = ?
		`, input.AppID, input.RevisionID).Error
		return apperrors.NewBusiness(apperrors.ErrConflict, "app_not_deployable", "application became offline or suspended before activation", "激活前应用已下线或被管理员封停")
	}
	return nil
}

func (r *PostgresRepository) ListVisiblePortalApps(ctx context.Context, principal domainidentity.Principal) ([]domainapphosting.App, error) {
	userID := strings.TrimSpace(principal.UserID)
	if userID == "" {
		return []domainapphosting.App{}, nil
	}
	whereSQL, args := buildPortalVisibilityWhere(userID, principal.Teams)
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT `+hostedAppColumns+`
		FROM public.hosted_apps
		`+whereSQL+`
		ORDER BY updated_at DESC, id ASC
	`, args...).Rows()
	if err != nil {
		return nil, fmt.Errorf("list portal hosted apps: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]domainapphosting.App, 0)
	for rows.Next() {
		app, scanErr := scanHostedApp(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, app)
	}
	return items, rows.Err()
}

func buildPortalVisibilityWhere(userID string, teams []string) (string, []any) {
	userJSON, _ := json.Marshal([]string{userID})
	selectedChecks := []string{"visibility_user_ids @> ?::jsonb"}
	args := []any{userID, string(userJSON)}
	for _, rawTeam := range teams {
		trimmed := strings.TrimSpace(rawTeam)
		if trimmed == "" {
			continue
		}
		teamJSON, _ := json.Marshal([]string{trimmed})
		selectedChecks = append(selectedChecks, "visibility_team_ids @> ?::jsonb")
		args = append(args, string(teamJSON))
	}
	audienceExpr := fmt.Sprintf(
		"(visibility_mode = 'organization' OR owner_user_id = ? OR (visibility_mode = 'selected' AND (%s)))",
		strings.Join(selectedChecks, " OR "),
	)
	where := "WHERE status = 'online' AND active_revision_id <> '' AND visibility_mode <> 'private' AND " + audienceExpr
	return where, args
}

func (r *PostgresRepository) LoadAppEnrichments(ctx context.Context, appIDs []string) (map[string]appapphosting.AppEnrichment, error) {
	out := make(map[string]appapphosting.AppEnrichment, len(appIDs))
	if len(appIDs) == 0 {
		return out, nil
	}
	if err := r.populateActiveRevisionStats(ctx, appIDs, out); err != nil {
		return nil, err
	}
	if err := r.populatePendingApprovals(ctx, appIDs, out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *PostgresRepository) populateActiveRevisionStats(ctx context.Context, appIDs []string, out map[string]appapphosting.AppEnrichment) error {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT a.id, r.revision_number, r.total_bytes
		FROM public.hosted_apps a
		JOIN public.hosted_app_revisions r ON r.app_id = a.id AND r.id = a.active_revision_id
		WHERE a.id IN ? AND a.active_revision_id <> ''
	`, appIDs).Rows()
	if err != nil {
		return fmt.Errorf("load hosted app active revision stats: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var appID string
		var revNum, totalBytes int64
		if err := rows.Scan(&appID, &revNum, &totalBytes); err != nil {
			return err
		}
		e := out[appID]
		e.ActiveRevisionNumber = revNum
		e.TotalBytes = totalBytes
		out[appID] = e
	}
	return rows.Err()
}

func (r *PostgresRepository) populatePendingApprovals(ctx context.Context, appIDs []string, out map[string]appapphosting.AppEnrichment) error {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT `+visibilityApprovalColumns+`
		FROM public.hosted_app_visibility_approvals
		WHERE app_id IN ? AND status = 'pending'
	`, appIDs).Rows()
	if err != nil {
		return fmt.Errorf("load pending visibility approvals: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		apv, scanErr := scanVisibilityApproval(rows)
		if scanErr != nil {
			return scanErr
		}
		e := out[apv.AppID]
		copied := apv
		e.PendingApproval = &copied
		out[apv.AppID] = e
	}
	return rows.Err()
}

func (r *PostgresRepository) UpdateAppVisibility(ctx context.Context, input appapphosting.UpdateVisibilityTxInput) (appapphosting.UpdateVisibilityTxOutput, error) {
	var out appapphosting.UpdateVisibilityTxOutput
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended('app_hosting_settings/default', 0))`).Error; err != nil {
			return err
		}
		settings, err := loadSettingsRow(ctx, tx)
		if err != nil {
			return err
		}
		app, err := getAppWhere(ctx, tx, "id = ?", strings.TrimSpace(input.AppID), true)
		if err != nil {
			return err
		}
		if !app.IsOwner(input.ActorUserID) {
			return apperrors.ErrAccessDenied
		}
		nowUTC := input.Now.UTC()
		if input.RequestedScope.Mode == sohaapi.HostedAppVisibilityModeOrganization {
			if settings.EffectiveOrganizationPolicy() == sohaapi.AppHostingOrganizationVisibilityPolicyDisabled {
				return apperrors.NewBusiness(apperrors.ErrAccessDenied, "organization_visibility_disabled", "organization-wide visibility is disabled by platform policy", "平台管理员当前已禁用全公司公开可见范围")
			}
			if settings.EffectiveOrganizationPolicy() == sohaapi.AppHostingOrganizationVisibilityPolicyRequireApproval &&
				app.Visibility.Mode != sohaapi.HostedAppVisibilityModeOrganization {
				pending, upsertErr := upsertPostgresPendingApprovalTx(ctx, tx, app, settings, input, nowUTC)
				if upsertErr != nil {
					return upsertErr
				}
				out = appapphosting.UpdateVisibilityTxOutput{
					App:               app,
					Settings:          settings,
					PendingApproval:   &pending,
					RequestedApproval: true,
				}
				return nil
			}
		}

		app.Visibility = domainapphosting.CloneVisibilityScope(input.RequestedScope)
		app.RouteGeneration++
		app.UpdatedAt = nowUTC
		if err := persistHostedAppUpdate(tx, app); err != nil {
			return err
		}
		if err := invalidateAppPendingApprovalsTx(tx, app.ID, "visibility_changed", nowUTC); err != nil {
			return err
		}
		out = appapphosting.UpdateVisibilityTxOutput{App: app, Settings: settings, RequestedApproval: false}
		return nil
	})
	if err != nil {
		return appapphosting.UpdateVisibilityTxOutput{}, err
	}
	return out, nil
}

func upsertPostgresPendingApprovalTx(
	ctx context.Context,
	tx *gorm.DB,
	app domainapphosting.App,
	settings domainapphosting.Settings,
	input appapphosting.UpdateVisibilityTxInput,
	nowUTC time.Time,
) (domainapphosting.VisibilityApproval, error) {
	existing, err := loadPendingApprovalForAppTx(ctx, tx, app.ID, true)
	if err != nil {
		return domainapphosting.VisibilityApproval{}, err
	}
	if existing != nil {
		if existing.RequesterUserID == input.ActorUserID &&
			existing.BaseRouteGeneration == app.RouteGeneration &&
			existing.PolicyVersion == settings.PolicyVersion {
			updated := *existing
			updated.AppName = app.Name
			updated.AppDNSLabel = app.DNSLabel
			if input.Reason != "" {
				updated.Reason = input.Reason
			}
			updated.UpdatedAt = nowUTC
			if err := tx.Exec(`
				UPDATE public.hosted_app_visibility_approvals
				SET app_name = ?, app_dns_label = ?, reason = ?, updated_at = ?
				WHERE id = ?
			`, updated.AppName, updated.AppDNSLabel, updated.Reason, nowUTC, updated.ID).Error; err != nil {
				return domainapphosting.VisibilityApproval{}, err
			}
			return updated, nil
		}
		if err := invalidateAppPendingApprovalsTx(tx, app.ID, "visibility_changed", nowUTC); err != nil {
			return domainapphosting.VisibilityApproval{}, err
		}
	}

	created := domainapphosting.VisibilityApproval{
		ID:                  input.NewApprovalID,
		AppID:               app.ID,
		AppName:             app.Name,
		AppDNSLabel:         app.DNSLabel,
		RequesterUserID:     input.ActorUserID,
		CurrentVisibility:   domainapphosting.CloneVisibilityScope(app.Visibility),
		RequestedVisibility: sohaapi.HostedAppVisibilityScope{Mode: sohaapi.HostedAppVisibilityModeOrganization},
		BaseRouteGeneration: app.RouteGeneration,
		PolicyVersion:       settings.PolicyVersion,
		Status:              sohaapi.HostedAppVisibilityApprovalStatusPending,
		Reason:              input.Reason,
		CreatedAt:           nowUTC,
		UpdatedAt:           nowUTC,
	}
	if err := insertVisibilityApprovalRowTx(tx, created); err != nil {
		return domainapphosting.VisibilityApproval{}, err
	}
	return created, nil
}

func (r *PostgresRepository) WithdrawVisibilityApproval(ctx context.Context, appID, ownerUserID string, now time.Time) (domainapphosting.App, domainapphosting.VisibilityApproval, error) {
	var app domainapphosting.App
	var withdrawn domainapphosting.VisibilityApproval
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		loadedApp, err := getAppWhere(ctx, tx, "id = ?", strings.TrimSpace(appID), true)
		if err != nil {
			return err
		}
		if !loadedApp.IsOwner(ownerUserID) {
			return apperrors.ErrAccessDenied
		}
		pending, err := loadPendingApprovalForAppTx(ctx, tx, loadedApp.ID, true)
		if err != nil {
			return err
		}
		if pending == nil {
			return apperrors.NewBusiness(apperrors.ErrConflict, "approval_not_pending", "no pending organization visibility approval exists for this application", "当前应用没有待处理的公开审批申请")
		}
		nowUTC := now.UTC()
		if err := tx.Exec(`
			UPDATE public.hosted_app_visibility_approvals
			SET status = 'withdrawn', updated_at = ?, decided_at = ?
			WHERE id = ? AND status = 'pending'
		`, nowUTC, nowUTC, pending.ID).Error; err != nil {
			return err
		}
		pending.Status = sohaapi.HostedAppVisibilityApprovalStatusWithdrawn
		pending.UpdatedAt = nowUTC
		pending.DecidedAt = &nowUTC
		app = loadedApp
		withdrawn = *pending
		return nil
	})
	if err != nil {
		return domainapphosting.App{}, domainapphosting.VisibilityApproval{}, err
	}
	return app, withdrawn, nil
}

func (r *PostgresRepository) ListVisibilityApprovals(ctx context.Context, filter domainapphosting.VisibilityApprovalFilter) ([]domainapphosting.VisibilityApproval, int, error) {
	whereSQL, args := buildVisibilityApprovalsWhere(filter)
	return queryPaginatedRows(
		ctx,
		r.db,
		"public.hosted_app_visibility_approvals apv",
		visibilityApprovalColumns,
		whereSQL,
		"apv.created_at DESC, apv.id ASC",
		args,
		filter.Limit,
		filter.Offset,
		"visibility approvals",
		scanVisibilityApproval,
	)
}

func buildVisibilityApprovalsWhere(filter domainapphosting.VisibilityApprovalFilter) (string, []any) {
	clauses := []string{"1 = 1"}
	var args []any
	if filter.Scope != sohaapi.HostedAppApprovalListScopeAll {
		clauses = append(clauses, "(apv.requester_user_id = ? OR EXISTS (SELECT 1 FROM public.hosted_apps ha WHERE ha.id = apv.app_id AND ha.owner_user_id = ?))")
		args = append(args, filter.RequesterUserID, filter.RequesterUserID)
	}
	if filter.Status != "" {
		clauses = append(clauses, "apv.status = ?")
		args = append(args, string(filter.Status))
	}
	if filter.AppID != "" {
		clauses = append(clauses, "apv.app_id = ?")
		args = append(args, filter.AppID)
	}
	return "WHERE " + strings.Join(clauses, " AND "), args
}

func (r *PostgresRepository) ApproveVisibilityApproval(
	ctx context.Context,
	approvalID, reviewerUserID, comment string,
	now time.Time,
	verifyRequester func(ctx context.Context, requesterUserID string) error,
) (domainapphosting.App, domainapphosting.VisibilityApproval, error) {
	var updatedApp domainapphosting.App
	var updatedApproval domainapphosting.VisibilityApproval
	var invalidatedErr error

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended('app_hosting_settings/default', 0))`).Error; err != nil {
			return err
		}
		settings, err := loadSettingsRow(ctx, tx)
		if err != nil {
			return err
		}
		apv, err := getVisibilityApprovalTx(ctx, tx, strings.TrimSpace(approvalID), true)
		if err != nil {
			return err
		}
		if err := checkReviewerAndPendingStatus(apv, reviewerUserID); err != nil {
			return err
		}
		app, err := getAppWhere(ctx, tx, "id = ?", apv.AppID, true)
		if err != nil {
			return err
		}
		nowUTC := now.UTC()
		if reason := detectApprovalInvalidationReason(settings, app, apv); reason != "" {
			if err := markApprovalInvalidatedByIDTx(tx, apv.ID, reason, nowUTC); err != nil {
				return err
			}
			invalidatedErr = newApprovalInvalidatedError(reason)
			return nil
		}
		if app.Status == sohaapi.HostedAppStatusSuspendedByAdmin {
			return apperrors.NewBusiness(apperrors.ErrConflict, "app_suspended_by_admin", "cannot approve visibility for an administratively suspended application", "该应用已被管理员强制封停，无法批准公开申请")
		}
		if verifyRequester != nil {
			if err := verifyRequester(ctx, apv.RequesterUserID); err != nil {
				return err
			}
		}
		return commitApprovedVisibilityTx(tx, app, apv, reviewerUserID, comment, nowUTC, &updatedApp, &updatedApproval)
	})
	if err != nil {
		return domainapphosting.App{}, domainapphosting.VisibilityApproval{}, err
	}
	if invalidatedErr != nil {
		return domainapphosting.App{}, domainapphosting.VisibilityApproval{}, invalidatedErr
	}
	return updatedApp, updatedApproval, nil
}

func commitApprovedVisibilityTx(
	tx *gorm.DB,
	app domainapphosting.App,
	apv domainapphosting.VisibilityApproval,
	reviewerUserID, comment string,
	nowUTC time.Time,
	outApp *domainapphosting.App,
	outApproval *domainapphosting.VisibilityApproval,
) error {
	app.Visibility = sohaapi.HostedAppVisibilityScope{Mode: sohaapi.HostedAppVisibilityModeOrganization}
	app.RouteGeneration++
	app.UpdatedAt = nowUTC
	if err := persistHostedAppUpdate(tx, app); err != nil {
		return err
	}
	if err := tx.Exec(`
		UPDATE public.hosted_app_visibility_approvals
		SET status = 'approved', reviewer_user_id = ?, review_comment = ?, updated_at = ?, decided_at = ?
		WHERE id = ? AND status = 'pending'
	`, reviewerUserID, comment, nowUTC, nowUTC, apv.ID).Error; err != nil {
		return err
	}
	apv.Status = sohaapi.HostedAppVisibilityApprovalStatusApproved
	apv.ReviewerUserID = reviewerUserID
	apv.ReviewComment = comment
	apv.UpdatedAt = nowUTC
	apv.DecidedAt = &nowUTC
	*outApp = app
	*outApproval = apv
	return nil
}

func (r *PostgresRepository) RejectVisibilityApproval(ctx context.Context, approvalID, reviewerUserID, comment string, now time.Time) (domainapphosting.App, domainapphosting.VisibilityApproval, error) {
	var app domainapphosting.App
	var rejected domainapphosting.VisibilityApproval
	var invalidatedErr error
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended('app_hosting_settings/default', 0))`).Error; err != nil {
			return err
		}
		settings, err := loadSettingsRow(ctx, tx)
		if err != nil {
			return err
		}
		apv, err := getVisibilityApprovalTx(ctx, tx, strings.TrimSpace(approvalID), true)
		if err != nil {
			return err
		}
		if err := checkReviewerAndPendingStatus(apv, reviewerUserID); err != nil {
			return err
		}
		loadedApp, err := getAppWhere(ctx, tx, "id = ?", apv.AppID, true)
		if err != nil {
			return err
		}
		nowUTC := now.UTC()
		if reason := detectApprovalInvalidationReason(settings, loadedApp, apv); reason != "" {
			if err := markApprovalInvalidatedByIDTx(tx, apv.ID, reason, nowUTC); err != nil {
				return err
			}
			invalidatedErr = newApprovalInvalidatedError(reason)
			return nil
		}
		if err := tx.Exec(`
			UPDATE public.hosted_app_visibility_approvals
			SET status = 'rejected', reviewer_user_id = ?, review_comment = ?, updated_at = ?, decided_at = ?
			WHERE id = ? AND status = 'pending'
		`, reviewerUserID, comment, nowUTC, nowUTC, apv.ID).Error; err != nil {
			return err
		}
		apv.Status = sohaapi.HostedAppVisibilityApprovalStatusRejected
		apv.ReviewerUserID = reviewerUserID
		apv.ReviewComment = comment
		apv.UpdatedAt = nowUTC
		apv.DecidedAt = &nowUTC
		app = loadedApp
		rejected = apv
		return nil
	})
	if err != nil {
		return domainapphosting.App{}, domainapphosting.VisibilityApproval{}, err
	}
	if invalidatedErr != nil {
		return domainapphosting.App{}, domainapphosting.VisibilityApproval{}, invalidatedErr
	}
	return app, rejected, nil
}

func (r *PostgresRepository) TransferAppOwnership(ctx context.Context, appID, newOwnerUserID string, now time.Time) (domainapphosting.App, string, error) {
	var updated domainapphosting.App
	var previousOwner string
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		app, err := getAppWhere(ctx, tx, "id = ?", strings.TrimSpace(appID), true)
		if err != nil {
			return err
		}
		if app.OwnerUserID == newOwnerUserID {
			return apperrors.NewBusiness(apperrors.ErrConflict, "same_owner", "newOwnerUserId is already the application owner", "目标用户已是该应用的当前归属人")
		}
		previousOwner = app.OwnerUserID
		nowUTC := now.UTC()
		app.OwnerUserID = newOwnerUserID
		app.MaintainerUserIDs = slices.DeleteFunc(slices.Clone(app.MaintainerUserIDs), func(uid string) bool {
			return uid == newOwnerUserID
		})
		app.IdempotencyKey = ""
		app.RequestFingerprint = ""
		app.RouteGeneration++
		app.UpdatedAt = nowUTC

		maintJSON, err := json.Marshal(nonNilStrings(app.MaintainerUserIDs))
		if err != nil {
			return err
		}
		if err := tx.Exec(`
			UPDATE public.hosted_apps
			SET owner_user_id = ?, maintainer_user_ids = ?::jsonb, route_generation = ?,
			    idempotency_key = '', request_fingerprint = '', updated_at = ?
			WHERE id = ?
		`, app.OwnerUserID, string(maintJSON), app.RouteGeneration, nowUTC, app.ID).Error; err != nil {
			return err
		}
		if err := invalidateAppPendingApprovalsTx(tx, app.ID, "ownership_transferred", nowUTC); err != nil {
			return err
		}
		updated = app
		return nil
	})
	if err != nil {
		return domainapphosting.App{}, "", err
	}
	return updated, previousOwner, nil
}

func (r *PostgresRepository) DeleteApp(ctx context.Context, appID string, verify func(current domainapphosting.App) error) (domainapphosting.App, []domainapphosting.Revision, error) {
	var deleted domainapphosting.App
	var revisions []domainapphosting.Revision
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		app, err := getAppWhere(ctx, tx, "id = ?", strings.TrimSpace(appID), true)
		if err != nil {
			return err
		}
		if verify != nil {
			if err := verify(app); err != nil {
				return err
			}
		}
		revs, err := loadAllAppRevisionsTx(ctx, tx, app.ID)
		if err != nil {
			return err
		}
		if err := tx.Exec(`DELETE FROM public.hosted_apps WHERE id = ?`, app.ID).Error; err != nil {
			return err
		}
		deleted = app
		revisions = revs
		return nil
	})
	if err != nil {
		return domainapphosting.App{}, nil, err
	}
	return deleted, revisions, nil
}

func loadAllAppRevisionsTx(ctx context.Context, tx *gorm.DB, appID string) ([]domainapphosting.Revision, error) {
	rows, err := tx.WithContext(ctx).Raw(`
		SELECT `+hostedRevisionColumns+`
		FROM public.hosted_app_revisions
		WHERE app_id = ?
		ORDER BY revision_number DESC
	`, appID).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var items []domainapphosting.Revision
	for rows.Next() {
		rev, scanErr := scanHostedRevision(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, rev)
	}
	return items, rows.Err()
}

func insertVisibilityApprovalRowTx(tx *gorm.DB, apv domainapphosting.VisibilityApproval) error {
	curJSON, err := json.Marshal(domainapphosting.CloneVisibilityScope(apv.CurrentVisibility))
	if err != nil {
		return err
	}
	reqJSON, err := json.Marshal(domainapphosting.CloneVisibilityScope(apv.RequestedVisibility))
	if err != nil {
		return err
	}
	err = tx.Exec(`
		INSERT INTO public.hosted_app_visibility_approvals (
			id, app_id, app_name, app_dns_label, requester_user_id,
			current_visibility, requested_visibility, base_route_generation, policy_version,
			status, reason, reviewer_user_id, review_comment, invalidation_reason,
			created_at, updated_at, decided_at
		) VALUES (?, ?, ?, ?, ?, ?::jsonb, ?::jsonb, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		apv.ID, apv.AppID, apv.AppName, apv.AppDNSLabel, apv.RequesterUserID,
		string(curJSON), string(reqJSON), apv.BaseRouteGeneration, apv.PolicyVersion,
		string(apv.Status), apv.Reason, apv.ReviewerUserID, apv.ReviewComment, apv.InvalidationReason,
		apv.CreatedAt.UTC(), apv.UpdatedAt.UTC(), nullableTime(apv.DecidedAt),
	).Error
	return mapPostgresRevisionWriteError(err)
}

func loadPendingApprovalForAppTx(ctx context.Context, tx *gorm.DB, appID string, forUpdate bool) (*domainapphosting.VisibilityApproval, error) {
	suffix := " LIMIT 1"
	if forUpdate {
		suffix = " FOR UPDATE"
	}
	apv, err := scanVisibilityApproval(tx.WithContext(ctx).Raw(`
		SELECT `+visibilityApprovalColumns+`
		FROM public.hosted_app_visibility_approvals
		WHERE app_id = ? AND status = 'pending'`+suffix, appID).Row())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &apv, nil
}

func getVisibilityApprovalTx(ctx context.Context, tx *gorm.DB, approvalID string, forUpdate bool) (domainapphosting.VisibilityApproval, error) {
	suffix := " LIMIT 1"
	if forUpdate {
		suffix = " FOR UPDATE"
	}
	apv, err := scanVisibilityApproval(tx.WithContext(ctx).Raw(`
		SELECT `+visibilityApprovalColumns+`
		FROM public.hosted_app_visibility_approvals
		WHERE id = ?`+suffix, approvalID).Row())
	if errors.Is(err, sql.ErrNoRows) {
		return domainapphosting.VisibilityApproval{}, apperrors.ErrNotFound
	}
	return apv, err
}

func invalidateAppPendingApprovalsTx(tx *gorm.DB, appID, reason string, nowUTC time.Time) error {
	return tx.Exec(`
		UPDATE public.hosted_app_visibility_approvals
		SET status = 'invalidated', invalidation_reason = ?, updated_at = ?, decided_at = ?
		WHERE app_id = ? AND status = 'pending'
	`, reason, nowUTC, nowUTC, appID).Error
}

func markApprovalInvalidatedByIDTx(tx *gorm.DB, approvalID, reason string, nowUTC time.Time) error {
	return tx.Exec(`
		UPDATE public.hosted_app_visibility_approvals
		SET status = 'invalidated', invalidation_reason = ?, updated_at = ?, decided_at = ?
		WHERE id = ? AND status = 'pending'
	`, reason, nowUTC, nowUTC, approvalID).Error
}

func scanVisibilityApproval(scanner rowScanner) (domainapphosting.VisibilityApproval, error) {
	var apv domainapphosting.VisibilityApproval
	var curRaw, reqRaw []byte
	var statusStr string
	var decidedAt sql.NullTime
	err := scanner.Scan(
		&apv.ID, &apv.AppID, &apv.AppName, &apv.AppDNSLabel, &apv.RequesterUserID,
		&curRaw, &reqRaw, &apv.BaseRouteGeneration, &apv.PolicyVersion,
		&statusStr, &apv.Reason, &apv.ReviewerUserID, &apv.ReviewComment, &apv.InvalidationReason,
		&apv.CreatedAt, &apv.UpdatedAt, &decidedAt,
	)
	if err != nil {
		return domainapphosting.VisibilityApproval{}, err
	}
	if err := json.Unmarshal(curRaw, &apv.CurrentVisibility); err != nil {
		return domainapphosting.VisibilityApproval{}, err
	}
	if err := json.Unmarshal(reqRaw, &apv.RequestedVisibility); err != nil {
		return domainapphosting.VisibilityApproval{}, err
	}
	apv.Status = sohaapi.HostedAppVisibilityApprovalStatus(statusStr)
	apv.CreatedAt = apv.CreatedAt.UTC()
	apv.UpdatedAt = apv.UpdatedAt.UTC()
	if decidedAt.Valid {
		t := decidedAt.Time.UTC()
		apv.DecidedAt = &t
	}
	return apv, nil
}

func (d *PostgresAudienceDirectory) ResolveUserSummaries(ctx context.Context, userIDs []string) (map[string]sohaapi.HostedAppUserSummary, error) {
	unique := deduplicateNonEmptyStrings(userIDs)
	if len(unique) == 0 {
		return map[string]sohaapi.HostedAppUserSummary{}, nil
	}
	rows, err := d.db.WithContext(ctx).Raw(`
		SELECT u.id::text,
		       COALESCE(NULLIF(u.display_name, ''), NULLIF(u.username, ''), u.id::text),
		       COALESCE(u.preferences::jsonb->>'avatarUrl', ''),
		       COALESCE((
		           SELECT json_agg(t.name ORDER BY t.name ASC)
		           FROM public.user_team_bindings utb
		           JOIN public.teams t ON t.id = utb.team_id
		           WHERE utb.user_id = u.id
		       ), '[]'::json)
		FROM public.users u
		WHERE u.id::text IN ?
	`, unique).Rows()
	if err != nil {
		return nil, fmt.Errorf("resolve hosted app user summaries: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make(map[string]sohaapi.HostedAppUserSummary, len(unique))
	for rows.Next() {
		var uid, displayName, avatarURL string
		var teamsRaw []byte
		if err := rows.Scan(&uid, &displayName, &avatarURL, &teamsRaw); err != nil {
			return nil, err
		}
		var rawNames []string
		if len(teamsRaw) > 0 {
			if err := json.Unmarshal(teamsRaw, &rawNames); err != nil {
				return nil, err
			}
		}
		out[uid] = sohaapi.HostedAppUserSummary{
			UserID:      uid,
			DisplayName: truncateString(strings.TrimSpace(displayName), 256),
			AvatarURL:   truncateString(strings.TrimSpace(avatarURL), 2048),
			TeamNames:   sanitizeSummaryTeamNames(rawNames),
		}
	}
	return out, rows.Err()
}

func sanitizeSummaryTeamNames(raw []string) []string {
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		trimmed := truncateString(strings.TrimSpace(item), 128)
		if trimmed != "" && !slices.Contains(out, trimmed) {
			out = append(out, trimmed)
			if len(out) == 50 {
				break
			}
		}
	}
	return out
}

func (d *PostgresAudienceDirectory) ResolveUserPrincipal(ctx context.Context, userID string) (domainidentity.Principal, bool, error) {
	var id, username, status string
	var rolesRaw, teamsRaw []byte
	err := d.db.WithContext(ctx).Raw(`
		SELECT u.id::text, u.username, u.status,
		       COALESCE((
		           SELECT json_agg(urb.role_id ORDER BY urb.role_id ASC)
		           FROM public.user_role_bindings urb
		           WHERE urb.user_id = u.id
		       ), '[]'::json),
		       COALESCE((
		           SELECT json_agg(utb.team_id ORDER BY utb.team_id ASC)
		           FROM public.user_team_bindings utb
		           WHERE utb.user_id = u.id
		       ), '[]'::json)
		FROM public.users u
		WHERE u.id::text = ?
		LIMIT 1
	`, strings.TrimSpace(userID)).Row().Scan(&id, &username, &status, &rolesRaw, &teamsRaw)
	if errors.Is(err, sql.ErrNoRows) {
		return domainidentity.Principal{}, false, apperrors.ErrNotFound
	}
	if err != nil {
		return domainidentity.Principal{}, false, fmt.Errorf("resolve user principal: %w", err)
	}
	var roles, teams []string
	if len(rolesRaw) > 0 {
		if err := json.Unmarshal(rolesRaw, &roles); err != nil {
			return domainidentity.Principal{}, false, err
		}
	}
	if len(teamsRaw) > 0 {
		if err := json.Unmarshal(teamsRaw, &teams); err != nil {
			return domainidentity.Principal{}, false, err
		}
	}
	return domainidentity.Principal{
		UserID:   id,
		UserName: username,
		Roles:    roles,
		Teams:    teams,
	}, strings.EqualFold(status, "active"), nil
}

func (d *PostgresAudienceDirectory) ListTeamMemberIDs(ctx context.Context, teamID string) ([]string, error) {
	trimmed := strings.TrimSpace(teamID)
	if trimmed == "" {
		return []string{}, nil
	}
	rows, err := d.db.WithContext(ctx).Raw(`
		SELECT utb.user_id::text
		FROM public.user_team_bindings utb
		WHERE utb.team_id = ?
		ORDER BY utb.user_id::text ASC
	`, trimmed).Rows()
	if err != nil {
		return nil, fmt.Errorf("list team member user ids: %w", err)
	}
	defer func() { _ = rows.Close() }()

	members := make([]string, 0)
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			return nil, err
		}
		members = append(members, uid)
	}
	return members, rows.Err()
}

func deduplicateNonEmptyStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" && !slices.Contains(out, trimmed) {
			out = append(out, trimmed)
		}
	}
	return out
}
