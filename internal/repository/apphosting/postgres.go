package apphosting

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	appapphosting "github.com/opensoha/soha/internal/application/apphosting"
	domainapphosting "github.com/opensoha/soha/internal/domain/apphosting"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

var (
	_ appapphosting.Repository        = (*PostgresRepository)(nil)
	_ appapphosting.AudienceDirectory = (*PostgresAudienceDirectory)(nil)
)

func NewRepository(db *gorm.DB) appapphosting.Repository {
	if db == nil {
		return NewMemoryRepository()
	}
	return &PostgresRepository{db: db}
}

func NewAudienceDirectory(db *gorm.DB) appapphosting.AudienceDirectory {
	if db == nil {
		return NewMemoryAudienceDirectory()
	}
	return &PostgresAudienceDirectory{db: db}
}

type PostgresRepository struct {
	db *gorm.DB
}

func NewPostgresRepository(db *gorm.DB) (*PostgresRepository, error) {
	if db == nil {
		return nil, errors.New("apphosting postgres database is required")
	}
	return &PostgresRepository{db: db}, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func (r *PostgresRepository) GetSettings(ctx context.Context) (domainapphosting.Settings, error) {
	return loadSettingsRow(ctx, r.db)
}

func loadSettingsRow(ctx context.Context, db *gorm.DB) (domainapphosting.Settings, error) {
	settings := domainapphosting.DefaultSettings()
	var orgPolicyStr string
	var rotatedAt, updatedAt sql.NullTime
	err := db.WithContext(ctx).Raw(`
		SELECT enabled, apps_domain, public_scheme, storage_integration_id, storage_prefix,
		       allow_organization_visibility, organization_visibility_policy, policy_version,
		       max_bundle_compressed_bytes, max_bundle_uncompressed_bytes,
		       max_bundle_file_count, max_apps_per_owner, retained_revisions_per_app,
		       gateway_token_sha256, gateway_token_rotated_at, updated_by, updated_at
		FROM public.app_hosting_settings
		WHERE scope_key = 'default'
		LIMIT 1
	`).Row().Scan(
		&settings.Enabled,
		&settings.AppsDomain,
		&settings.PublicScheme,
		&settings.StorageIntegrationID,
		&settings.StoragePrefix,
		&settings.AllowOrganizationVisibility,
		&orgPolicyStr,
		&settings.PolicyVersion,
		&settings.MaxBundleCompressedBytes,
		&settings.MaxBundleUncompressedBytes,
		&settings.MaxBundleFileCount,
		&settings.MaxAppsPerOwner,
		&settings.RetainedRevisionsPerApp,
		&settings.GatewayTokenSHA256,
		&rotatedAt,
		&settings.UpdatedBy,
		&updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return settings.WithDefaults(), nil
	}
	if err != nil {
		return domainapphosting.Settings{}, fmt.Errorf("load app hosting settings: %w", err)
	}
	settings.OrganizationVisibilityPolicy = sohaapi.AppHostingOrganizationVisibilityPolicy(orgPolicyStr)
	if rotatedAt.Valid {
		t := rotatedAt.Time.UTC()
		settings.GatewayTokenRotatedAt = &t
	}
	if updatedAt.Valid {
		t := updatedAt.Time.UTC()
		settings.UpdatedAt = &t
	}
	return settings.WithDefaults(), nil
}

func (r *PostgresRepository) SaveSettings(ctx context.Context, mutate func(current domainapphosting.Settings) (domainapphosting.Settings, error)) (domainapphosting.Settings, error) {
	var saved domainapphosting.Settings
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended('app_hosting_settings/default', 0))`).Error; err != nil {
			return err
		}
		current, err := loadSettingsRow(ctx, tx)
		if err != nil {
			return err
		}
		prev := current.WithDefaults()
		next, err := mutate(prev)
		if err != nil {
			return err
		}
		next = next.WithDefaults()
		if err := upsertPostgresSettingsRow(tx, next); err != nil {
			return err
		}
		if next.OrganizationVisibilityPolicy != prev.OrganizationVisibilityPolicy || next.PolicyVersion != prev.PolicyVersion {
			decidedAt := time.Now().UTC()
			if next.UpdatedAt != nil {
				decidedAt = next.UpdatedAt.UTC()
			}
			if err := tx.Exec(`
				UPDATE public.hosted_app_visibility_approvals
				SET status = 'invalidated', invalidation_reason = 'policy_changed', updated_at = ?, decided_at = ?
				WHERE status = 'pending'
			`, decidedAt, decidedAt).Error; err != nil {
				return fmt.Errorf("invalidate pending approvals on policy change: %w", err)
			}
		}
		saved = next
		return nil
	})
	if err != nil {
		return domainapphosting.Settings{}, err
	}
	return saved, nil
}

func upsertPostgresSettingsRow(tx *gorm.DB, next domainapphosting.Settings) error {
	err := tx.Exec(`
		INSERT INTO public.app_hosting_settings (
			scope_key, enabled, apps_domain, public_scheme, storage_integration_id, storage_prefix,
			allow_organization_visibility, organization_visibility_policy, policy_version,
			max_bundle_compressed_bytes, max_bundle_uncompressed_bytes,
			max_bundle_file_count, max_apps_per_owner, retained_revisions_per_app,
			gateway_token_sha256, gateway_token_rotated_at, updated_by, updated_at
		) VALUES ('default', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (scope_key) DO UPDATE SET
			enabled = EXCLUDED.enabled,
			apps_domain = EXCLUDED.apps_domain,
			public_scheme = EXCLUDED.public_scheme,
			storage_integration_id = EXCLUDED.storage_integration_id,
			storage_prefix = EXCLUDED.storage_prefix,
			allow_organization_visibility = EXCLUDED.allow_organization_visibility,
			organization_visibility_policy = EXCLUDED.organization_visibility_policy,
			policy_version = EXCLUDED.policy_version,
			max_bundle_compressed_bytes = EXCLUDED.max_bundle_compressed_bytes,
			max_bundle_uncompressed_bytes = EXCLUDED.max_bundle_uncompressed_bytes,
			max_bundle_file_count = EXCLUDED.max_bundle_file_count,
			max_apps_per_owner = EXCLUDED.max_apps_per_owner,
			retained_revisions_per_app = EXCLUDED.retained_revisions_per_app,
			gateway_token_sha256 = EXCLUDED.gateway_token_sha256,
			gateway_token_rotated_at = EXCLUDED.gateway_token_rotated_at,
			updated_by = EXCLUDED.updated_by,
			updated_at = EXCLUDED.updated_at
	`,
		next.Enabled, next.AppsDomain, next.PublicScheme, next.StorageIntegrationID, next.StoragePrefix,
		next.AllowOrganizationVisibility, string(next.OrganizationVisibilityPolicy), next.PolicyVersion,
		next.MaxBundleCompressedBytes, next.MaxBundleUncompressedBytes,
		next.MaxBundleFileCount, next.MaxAppsPerOwner, next.RetainedRevisionsPerApp,
		next.GatewayTokenSHA256, nullableTime(next.GatewayTokenRotatedAt), next.UpdatedBy, nullableTime(next.UpdatedAt),
	).Error
	if err != nil {
		return fmt.Errorf("upsert app hosting settings: %w", err)
	}
	return nil
}

func (r *PostgresRepository) CreateApp(ctx context.Context, candidate domainapphosting.App, maxAppsPerOwner int32) (domainapphosting.App, bool, error) {
	created, _, reused, err := r.CreateAppWithApproval(ctx, candidate, maxAppsPerOwner, nil)
	return created, reused, err
}

func (r *PostgresRepository) CreateAppWithApproval(
	ctx context.Context,
	candidate domainapphosting.App,
	maxAppsPerOwner int32,
	initialApproval *domainapphosting.VisibilityApproval,
) (domainapphosting.App, *domainapphosting.VisibilityApproval, bool, error) {
	var created domainapphosting.App
	var savedApproval *domainapphosting.VisibilityApproval
	var reused bool
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?, 0))`, "hosted_apps/owner/"+candidate.OwnerUserID).Error; err != nil {
			return err
		}
		if candidate.IdempotencyKey != "" {
			existing, found, err := findAppByOwnerIdempotency(ctx, tx, candidate.OwnerUserID, candidate.IdempotencyKey)
			if err != nil {
				return err
			}
			if found {
				if existing.RequestFingerprint != candidate.RequestFingerprint {
					return apperrors.NewBusiness(apperrors.ErrConflict, "idempotency_key_conflict", "idempotencyKey already used with different create parameters", "同一幂等键已被不同参数的应用创建请求占用")
				}
				created = existing
				reused = true
				if initialApproval != nil {
					savedApproval, err = loadPendingApprovalForAppTx(ctx, tx, existing.ID, false)
					return err
				}
				return nil
			}
		}
		if maxAppsPerOwner > 0 {
			var count int64
			if err := tx.Raw(`SELECT COUNT(1) FROM public.hosted_apps WHERE owner_user_id = ?`, candidate.OwnerUserID).Row().Scan(&count); err != nil {
				return err
			}
			if count >= int64(maxAppsPerOwner) {
				return domainapphosting.ErrQuotaExceeded
			}
		}
		if err := insertHostedAppRow(tx, candidate, &created); err != nil {
			return err
		}
		if initialApproval != nil {
			apv := *initialApproval
			apv.AppID = created.ID
			apv.AppName = created.Name
			apv.AppDNSLabel = created.DNSLabel
			if err := insertVisibilityApprovalRowTx(tx, apv); err != nil {
				return err
			}
			savedApproval = &apv
		}
		return nil
	})
	if err != nil {
		return domainapphosting.App{}, nil, false, err
	}
	return created, savedApproval, reused, nil
}

func findAppByOwnerIdempotency(ctx context.Context, tx *gorm.DB, ownerUserID, idemKey string) (domainapphosting.App, bool, error) {
	row := tx.WithContext(ctx).Raw(`
		SELECT `+hostedAppColumns+`
		FROM public.hosted_apps
		WHERE owner_user_id = ? AND idempotency_key = ?
		LIMIT 1
	`, ownerUserID, idemKey).Row()
	app, err := scanHostedApp(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domainapphosting.App{}, false, nil
	}
	if err != nil {
		return domainapphosting.App{}, false, err
	}
	return app, true, nil
}

func insertHostedAppRow(tx *gorm.DB, candidate domainapphosting.App, out *domainapphosting.App) error {
	maintJSON, visUsersJSON, visTeamsJSON, err := marshalAppJSONArrays(candidate)
	if err != nil {
		return err
	}
	lowerDNS := strings.ToLower(candidate.DNSLabel)
	err = tx.Exec(`
		INSERT INTO public.hosted_apps (
			id, name, description, kind, dns_label, spa_fallback, owner_user_id,
			maintainer_user_ids, visibility_mode, visibility_user_ids, visibility_team_ids,
			status, suspended_reason, active_revision_id, route_generation,
			idempotency_key, request_fingerprint, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?::jsonb, ?, ?::jsonb, ?::jsonb, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		candidate.ID, candidate.Name, candidate.Description, string(candidate.Kind), lowerDNS,
		candidate.SpaFallback, candidate.OwnerUserID, maintJSON, string(candidate.Visibility.Mode),
		visUsersJSON, visTeamsJSON, string(candidate.Status), candidate.SuspendedReason,
		candidate.ActiveRevisionID, candidate.RouteGeneration, candidate.IdempotencyKey,
		candidate.RequestFingerprint, candidate.CreatedAt.UTC(), candidate.UpdatedAt.UTC(),
	).Error
	if err != nil {
		return mapPostgresAppWriteError(err)
	}
	stored := candidate
	stored.DNSLabel = lowerDNS
	*out = stored
	return nil
}

const hostedAppColumns = `id, name, description, kind, dns_label, spa_fallback, owner_user_id,
	maintainer_user_ids, visibility_mode, visibility_user_ids, visibility_team_ids,
	status, suspended_reason, active_revision_id, route_generation,
	idempotency_key, request_fingerprint, created_at, updated_at`

func (r *PostgresRepository) GetAppByID(ctx context.Context, appID string) (domainapphosting.App, error) {
	return getAppWhere(ctx, r.db, "id = ?", strings.TrimSpace(appID), false)
}

func (r *PostgresRepository) GetAppByDNSLabel(ctx context.Context, dnsLabel string) (domainapphosting.App, error) {
	return getAppWhere(ctx, r.db, "dns_label = ?", strings.ToLower(strings.TrimSpace(dnsLabel)), false)
}

func getAppWhere(ctx context.Context, db *gorm.DB, predicate string, arg any, forUpdate bool) (domainapphosting.App, error) {
	suffix := " LIMIT 1"
	if forUpdate {
		suffix = " FOR UPDATE"
	}
	app, err := scanHostedApp(db.WithContext(ctx).Raw(`SELECT `+hostedAppColumns+` FROM public.hosted_apps WHERE `+predicate+suffix, arg).Row())
	if errors.Is(err, sql.ErrNoRows) {
		return domainapphosting.App{}, apperrors.ErrNotFound
	}
	return app, err
}

func (r *PostgresRepository) ListApps(ctx context.Context, filter domainapphosting.AppListFilter) ([]domainapphosting.App, int, error) {
	whereSQL, args := buildListAppsWhere(filter)
	return queryPaginatedRows(
		ctx,
		r.db,
		"public.hosted_apps",
		hostedAppColumns,
		whereSQL,
		"updated_at DESC, id ASC",
		args,
		filter.Limit,
		filter.Offset,
		"hosted apps",
		scanHostedApp,
	)
}

func queryPaginatedRows[T any](
	ctx context.Context,
	db *gorm.DB,
	tableExpr, columnsExpr, whereSQL, orderSQL string,
	args []any,
	limit, offset int,
	label string,
	scanFn func(rowScanner) (T, error),
) ([]T, int, error) {
	var total int
	if err := db.WithContext(ctx).Raw(`SELECT COUNT(1) FROM `+tableExpr+` `+whereSQL, args...).Row().Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count %s: %w", label, err)
	}
	queryArgs := append(append([]any{}, args...), limit, offset)
	rows, err := db.WithContext(ctx).Raw(`
		SELECT `+columnsExpr+`
		FROM `+tableExpr+`
		`+whereSQL+`
		ORDER BY `+orderSQL+`
		LIMIT ? OFFSET ?
	`, queryArgs...).Rows()
	if err != nil {
		return nil, 0, fmt.Errorf("list %s: %w", label, err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]T, 0, limit)
	for rows.Next() {
		item, scanErr := scanFn(rows)
		if scanErr != nil {
			return nil, 0, scanErr
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func buildListAppsWhere(filter domainapphosting.AppListFilter) (string, []any) {
	clauses := []string{"1 = 1"}
	var args []any
	if filter.Scope != sohaapi.HostedAppListScopeAll {
		memberJSON, _ := json.Marshal([]string{filter.MemberUserID})
		clauses = append(clauses, "(owner_user_id = ? OR maintainer_user_ids @> ?::jsonb)")
		args = append(args, filter.MemberUserID, string(memberJSON))
	}
	if filter.OwnerUserID != "" {
		clauses = append(clauses, "owner_user_id = ?")
		args = append(args, filter.OwnerUserID)
	}
	clauses, args = appendListAppsTeamFilter(clauses, args, filter)
	if filter.Status != "" {
		clauses = append(clauses, "status = ?")
		args = append(args, string(filter.Status))
	}
	if filter.Query != "" {
		pattern := "%" + escapeILIKEPattern(filter.Query) + "%"
		clauses = append(clauses, `(name ILIKE ? ESCAPE '\' OR dns_label ILIKE ? ESCAPE '\' OR description ILIKE ? ESCAPE '\')`)
		args = append(args, pattern, pattern, pattern)
	}
	return "WHERE " + strings.Join(clauses, " AND "), args
}

func appendListAppsTeamFilter(clauses []string, args []any, filter domainapphosting.AppListFilter) ([]string, []any) {
	if filter.TeamFilterActive {
		if len(filter.TeamMemberUserIDs) == 0 {
			return append(clauses, "1 = 0"), args
		}
		return append(clauses, "owner_user_id IN ?"), append(args, filter.TeamMemberUserIDs)
	}
	if filter.TeamID != "" {
		clauses = append(clauses, "EXISTS (SELECT 1 FROM public.user_team_bindings utb WHERE utb.user_id::text = public.hosted_apps.owner_user_id AND utb.team_id = ?)")
		args = append(args, filter.TeamID)
	}
	return clauses, args
}

func escapeILIKEPattern(raw string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(raw)
}

func (r *PostgresRepository) MutateApp(ctx context.Context, appID string, mutate func(current domainapphosting.App) (domainapphosting.App, error)) (domainapphosting.App, error) {
	var updated domainapphosting.App
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		current, err := getAppWhere(ctx, tx, "id = ?", strings.TrimSpace(appID), true)
		if err != nil {
			return err
		}
		next, err := mutate(current)
		if err != nil {
			return err
		}
		next.ID = current.ID
		next.DNSLabel = current.DNSLabel
		next.OwnerUserID = current.OwnerUserID
		if err := persistHostedAppUpdate(tx, next); err != nil {
			return err
		}
		if err := syncOrInvalidateApprovalOnMutateTx(tx, current, next); err != nil {
			return err
		}
		updated = next
		return nil
	})
	if err != nil {
		return domainapphosting.App{}, err
	}
	return updated, nil
}

func syncOrInvalidateApprovalOnMutateTx(tx *gorm.DB, current, next domainapphosting.App) error {
	decidedAt := next.UpdatedAt.UTC()
	if decidedAt.IsZero() {
		decidedAt = time.Now().UTC()
	}
	if next.Status == sohaapi.HostedAppStatusSuspendedByAdmin {
		return invalidateAppPendingApprovalsTx(tx, next.ID, "app_suspended", decidedAt)
	}
	if next.RouteGeneration != current.RouteGeneration {
		return invalidateAppPendingApprovalsTx(tx, next.ID, "route_generation_changed", decidedAt)
	}
	if next.Name != current.Name {
		return tx.Exec(`UPDATE public.hosted_app_visibility_approvals SET app_name = ? WHERE app_id = ? AND status = 'pending'`, next.Name, next.ID).Error
	}
	return nil
}

func persistHostedAppUpdate(tx *gorm.DB, app domainapphosting.App) error {
	maintJSON, visUsersJSON, visTeamsJSON, err := marshalAppJSONArrays(app)
	if err != nil {
		return err
	}
	return tx.Exec(`
		UPDATE public.hosted_apps
		SET name = ?, description = ?, spa_fallback = ?, maintainer_user_ids = ?::jsonb,
		    visibility_mode = ?, visibility_user_ids = ?::jsonb, visibility_team_ids = ?::jsonb,
		    status = ?, suspended_reason = ?, active_revision_id = ?,
		    route_generation = ?, updated_at = ?
		WHERE id = ?
	`,
		app.Name, app.Description, app.SpaFallback, maintJSON,
		string(app.Visibility.Mode), visUsersJSON, visTeamsJSON,
		string(app.Status), app.SuspendedReason, app.ActiveRevisionID,
		app.RouteGeneration, app.UpdatedAt.UTC(), app.ID,
	).Error
}

const hostedRevisionColumns = `id, app_id, revision_number, description, kind, status,
	bundle_sha256, file_count, total_bytes, spa_fallback, storage_integration_id,
	storage_prefix, manifest_json, failure_stage, failure_reason,
	idempotency_key, created_by, created_at, activated_at`

func (r *PostgresRepository) FindRevisionByIdempotencyKey(ctx context.Context, appID, idempotencyKey string) (domainapphosting.Revision, error) {
	rev, err := scanHostedRevision(r.db.WithContext(ctx).Raw(`
		SELECT `+hostedRevisionColumns+`
		FROM public.hosted_app_revisions
		WHERE app_id = ? AND idempotency_key = ?
		LIMIT 1
	`, appID, idempotencyKey).Row())
	if errors.Is(err, sql.ErrNoRows) {
		return domainapphosting.Revision{}, apperrors.ErrNotFound
	}
	return rev, err
}

func (r *PostgresRepository) CreateUploadingRevision(ctx context.Context, appID string, draft domainapphosting.Revision) (domainapphosting.Revision, error) {
	var created domainapphosting.Revision
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		app, err := getAppWhere(ctx, tx, "id = ?", appID, true)
		if err != nil {
			return err
		}
		if app.Status == sohaapi.HostedAppStatusOffline || app.Status == sohaapi.HostedAppStatusSuspendedByAdmin {
			return apperrors.NewBusiness(apperrors.ErrConflict, "app_not_deployable", "cannot upload revision while application is offline or administratively suspended", "应用处于下线或管理员封停状态时无法部署新版本")
		}
		var nextNum int64
		if err := tx.Raw(`SELECT COALESCE(MAX(revision_number), 0) + 1 FROM public.hosted_app_revisions WHERE app_id = ?`, appID).Row().Scan(&nextNum); err != nil {
			return err
		}
		stored := draft
		stored.RevisionNumber = nextNum
		stored.Status = sohaapi.HostedAppRevisionStatusUploading
		stored.Files = nonNilManifest(draft.Files)
		manifestBytes, err := json.Marshal(stored.Files)
		if err != nil {
			return err
		}
		if err := tx.Exec(`
			INSERT INTO public.hosted_app_revisions (
				id, app_id, revision_number, description, kind, status, bundle_sha256,
				file_count, total_bytes, spa_fallback, storage_integration_id, storage_prefix,
				manifest_json, failure_stage, failure_reason, idempotency_key, created_by, created_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?::jsonb, '', '', ?, ?, ?)
		`,
			stored.ID, stored.AppID, stored.RevisionNumber, stored.Description,
			string(stored.Kind), string(stored.Status), stored.BundleSha256,
			stored.FileCount, stored.TotalBytes, stored.SpaFallback,
			stored.StorageIntegrationID, stored.StoragePrefix, string(manifestBytes),
			stored.IdempotencyKey, stored.CreatedBy, stored.CreatedAt.UTC(),
		).Error; err != nil {
			return mapPostgresRevisionWriteError(err)
		}
		created = stored
		return nil
	})
	if err != nil {
		return domainapphosting.Revision{}, err
	}
	return created, nil
}

func (r *PostgresRepository) FailRevision(ctx context.Context, appID, revisionID, stage, reason string) error {
	res := r.db.WithContext(ctx).Exec(`
		UPDATE public.hosted_app_revisions
		SET status = 'failed', failure_stage = ?, failure_reason = ?
		WHERE app_id = ? AND id = ?
	`, stage, truncateString(reason, 1024), appID, revisionID)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return apperrors.ErrNotFound
	}
	return nil
}

func (r *PostgresRepository) ActivateRevision(ctx context.Context, input appapphosting.ActivateRevisionInput) (appapphosting.ActivateRevisionOutput, error) {
	var out appapphosting.ActivateRevisionOutput
	var rejectedConflict error

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		app, err := getAppWhere(ctx, tx, "id = ?", input.AppID, true)
		if err != nil {
			return err
		}
		rev, err := getRevisionTx(ctx, tx, input.AppID, input.RevisionID, true)
		if err != nil {
			return err
		}
		if rejectedErr := rejectUnactivatableRevisionTx(tx, app, input); rejectedErr != nil {
			rejectedConflict = rejectedErr
			return nil
		}

		wasDraft := app.Status == sohaapi.HostedAppStatusDraft
		prevGen := app.RouteGeneration
		manifestBytes, err := json.Marshal(nonNilManifest(input.Files))
		if err != nil {
			return err
		}
		nowUTC := input.Now.UTC()
		if err := tx.Exec(`
			UPDATE public.hosted_app_revisions
			SET status = 'ready', spa_fallback = ?, manifest_json = ?::jsonb, activated_at = ?
			WHERE app_id = ? AND id = ?
		`, input.SpaFallback, string(manifestBytes), nowUTC, input.AppID, input.RevisionID).Error; err != nil {
			return err
		}
		rev.Status = sohaapi.HostedAppRevisionStatusReady
		rev.SpaFallback = input.SpaFallback
		rev.Files = nonNilManifest(input.Files)
		rev.ActivatedAt = &nowUTC

		if wasDraft {
			app.Status = sohaapi.HostedAppStatusOnline
		}
		app.ActiveRevisionID = rev.ID
		app.SpaFallback = input.SpaFallback
		app.RouteGeneration++
		app.UpdatedAt = nowUTC
		if err := persistHostedAppUpdate(tx, app); err != nil {
			return err
		}
		if wasDraft {
			if err := tx.Exec(`
				UPDATE public.hosted_app_visibility_approvals
				SET base_route_generation = ?
				WHERE app_id = ? AND status = 'pending' AND requester_user_id = ? AND base_route_generation = ?
			`, app.RouteGeneration, app.ID, app.OwnerUserID, prevGen).Error; err != nil {
				return err
			}
		} else {
			if err := invalidateAppPendingApprovalsTx(tx, app.ID, "route_generation_changed", nowUTC); err != nil {
				return err
			}
		}

		evicted, err := evictExcessPostgresRevisions(ctx, tx, app.ID, app.ActiveRevisionID, input.RetainedRevisionsMax)
		if err != nil {
			return err
		}
		out = appapphosting.ActivateRevisionOutput{App: app, Revision: rev, EvictedRevisions: evicted}
		return nil
	})
	if err != nil {
		return appapphosting.ActivateRevisionOutput{}, err
	}
	if rejectedConflict != nil {
		return appapphosting.ActivateRevisionOutput{}, rejectedConflict
	}
	return out, nil
}

func evictExcessPostgresRevisions(ctx context.Context, tx *gorm.DB, appID, activeRevisionID string, retainedMax int32) ([]domainapphosting.Revision, error) {
	if retainedMax <= 0 {
		return nil, nil
	}
	ready, err := queryReadyHostedRevisionsForUpdate(ctx, tx, appID)
	if err != nil {
		return nil, err
	}

	var kept int32
	var evicted []domainapphosting.Revision
	for _, item := range ready {
		if item.ID == activeRevisionID || kept < retainedMax {
			kept++
			continue
		}
		if err := tx.Exec(`UPDATE public.hosted_app_revisions SET status = 'evicted' WHERE app_id = ? AND id = ?`, appID, item.ID).Error; err != nil {
			return nil, err
		}
		item.Status = sohaapi.HostedAppRevisionStatusEvicted
		evicted = append(evicted, item)
	}
	return evicted, nil
}

func (r *PostgresRepository) GetRevision(ctx context.Context, appID, revisionID string) (domainapphosting.Revision, error) {
	return getRevisionTx(ctx, r.db, appID, revisionID, false)
}

func getRevisionTx(ctx context.Context, db *gorm.DB, appID, revisionID string, forUpdate bool) (domainapphosting.Revision, error) {
	suffix := " LIMIT 1"
	if forUpdate {
		suffix = " FOR UPDATE"
	}
	rev, err := scanHostedRevision(db.WithContext(ctx).Raw(`
		SELECT `+hostedRevisionColumns+`
		FROM public.hosted_app_revisions
		WHERE app_id = ? AND id = ?`+suffix, appID, revisionID).Row())
	if errors.Is(err, sql.ErrNoRows) {
		return domainapphosting.Revision{}, apperrors.ErrNotFound
	}
	return rev, err
}

func (r *PostgresRepository) ListRevisions(ctx context.Context, appID string, limit, offset int) ([]domainapphosting.Revision, int, error) {
	var total int
	if err := r.db.WithContext(ctx).Raw(`SELECT COUNT(1) FROM public.hosted_app_revisions WHERE app_id = ?`, appID).Row().Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT `+hostedRevisionColumns+`
		FROM public.hosted_app_revisions
		WHERE app_id = ?
		ORDER BY revision_number DESC, id ASC
		LIMIT ? OFFSET ?
	`, appID, limit, offset).Rows()
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()

	items := make([]domainapphosting.Revision, 0, limit)
	for rows.Next() {
		item, scanErr := scanHostedRevision(rows)
		if scanErr != nil {
			return nil, 0, scanErr
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (r *PostgresRepository) RollbackRevision(ctx context.Context, appID, targetRevisionID, idempotencyKey string, now time.Time) (domainapphosting.App, domainapphosting.Revision, bool, error) {
	var updatedApp domainapphosting.App
	var targetRev domainapphosting.Revision
	var reused bool
	trimmedKey := strings.TrimSpace(idempotencyKey)
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		app, err := getAppWhere(ctx, tx, "id = ?", appID, true)
		if err != nil {
			return err
		}
		if trimmedKey != "" {
			replayedRev, hit, replayErr := checkRollbackIdempotencyTx(ctx, tx, appID, targetRevisionID, trimmedKey)
			if replayErr != nil {
				return replayErr
			}
			if hit {
				updatedApp = app
				targetRev = replayedRev
				reused = true
				return nil
			}
		}
		if app.Status == sohaapi.HostedAppStatusDraft {
			return apperrors.NewBusiness(apperrors.ErrConflict, "app_still_draft", "draft application cannot roll back", "草稿状态应用无可回滚版本")
		}
		rev, err := getRevisionTx(ctx, tx, appID, targetRevisionID, true)
		if err != nil {
			return err
		}
		if rev.Status != sohaapi.HostedAppRevisionStatusReady {
			return apperrors.NewBusiness(apperrors.ErrConflict, "revision_not_ready", "target revision is not in ready state", "目标版本不可用于回滚")
		}
		nowUTC := now.UTC()
		if err := applyRollbackUpdatesTx(tx, &app, &rev, trimmedKey, nowUTC); err != nil {
			return err
		}
		updatedApp = app
		targetRev = rev
		return nil
	})
	if err != nil {
		return domainapphosting.App{}, domainapphosting.Revision{}, false, err
	}
	return updatedApp, targetRev, reused, nil
}

func checkRollbackIdempotencyTx(ctx context.Context, tx *gorm.DB, appID, targetRevisionID, idempotencyKey string) (domainapphosting.Revision, bool, error) {
	var recordedTarget string
	err := tx.Raw(`
		SELECT target_revision_id
		FROM public.hosted_app_rollback_idempotency
		WHERE app_id = ? AND idempotency_key = ?
	`, appID, idempotencyKey).Row().Scan(&recordedTarget)
	if errors.Is(err, sql.ErrNoRows) {
		return domainapphosting.Revision{}, false, nil
	}
	if err != nil {
		return domainapphosting.Revision{}, false, err
	}
	if recordedTarget != targetRevisionID {
		return domainapphosting.Revision{}, false, apperrors.NewBusiness(apperrors.ErrConflict, "idempotency_key_conflict", "idempotencyKey was already used for a different targetRevisionId", "该幂等键已绑定到不同的目标回滚版本")
	}
	rev, err := getRevisionTx(ctx, tx, appID, recordedTarget, false)
	if err != nil {
		return domainapphosting.Revision{}, false, err
	}
	return rev, true, nil
}

func applyRollbackUpdatesTx(tx *gorm.DB, app *domainapphosting.App, rev *domainapphosting.Revision, idempotencyKey string, nowUTC time.Time) error {
	if err := tx.Exec(`UPDATE public.hosted_app_revisions SET activated_at = ? WHERE app_id = ? AND id = ?`, nowUTC, app.ID, rev.ID).Error; err != nil {
		return err
	}
	rev.ActivatedAt = &nowUTC
	app.ActiveRevisionID = rev.ID
	app.SpaFallback = rev.SpaFallback
	app.RouteGeneration++
	app.UpdatedAt = nowUTC
	if err := persistHostedAppUpdate(tx, *app); err != nil {
		return err
	}
	if err := invalidateAppPendingApprovalsTx(tx, app.ID, "route_generation_changed", nowUTC); err != nil {
		return err
	}
	if idempotencyKey == "" {
		return nil
	}
	return tx.Exec(`
		INSERT INTO public.hosted_app_rollback_idempotency (
			app_id, idempotency_key, target_revision_id, route_generation, created_at
		) VALUES (?, ?, ?, ?, ?)
	`, app.ID, idempotencyKey, rev.ID, app.RouteGeneration, nowUTC).Error
}

func (r *PostgresRepository) CreateAuthTicket(ctx context.Context, ticket domainapphosting.AuthTicket) error {
	teamsJSON, err := json.Marshal(nonNilStrings(ticket.Teams))
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Exec(`
		INSERT INTO public.hosted_app_auth_tickets (
			ticket_sha256, app_id, dns_label, host, route_generation, return_path,
			source_session_id, gateway_state_sha256,
			user_id, username, display_name, email, teams_json, expires_at, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?::jsonb, ?, ?)
	`,
		ticket.TicketSHA256, ticket.AppID, ticket.DNSLabel, ticket.Host,
		ticket.RouteGeneration, ticket.ReturnPath,
		ticket.SourceSessionID, ticket.GatewayStateSHA256,
		ticket.UserID, ticket.Username, ticket.DisplayName, ticket.Email,
		string(teamsJSON), ticket.ExpiresAt.UTC(), ticket.CreatedAt.UTC(),
	).Error
}

func (r *PostgresRepository) ConsumeAuthTicket(ctx context.Context, ticketSHA256, host string, now time.Time) (domainapphosting.AuthTicket, error) {
	var ticket domainapphosting.AuthTicket
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var teamsRaw []byte
		var consumedAt sql.NullTime
		scanErr := tx.Raw(`
			SELECT ticket_sha256, app_id, dns_label, host, route_generation, return_path,
			       source_session_id, gateway_state_sha256,
			       user_id, username, display_name, email, teams_json, expires_at, consumed_at, created_at
			FROM public.hosted_app_auth_tickets
			WHERE ticket_sha256 = ?
			FOR UPDATE
		`, ticketSHA256).Row().Scan(
			&ticket.TicketSHA256, &ticket.AppID, &ticket.DNSLabel, &ticket.Host,
			&ticket.RouteGeneration, &ticket.ReturnPath,
			&ticket.SourceSessionID, &ticket.GatewayStateSHA256,
			&ticket.UserID, &ticket.Username, &ticket.DisplayName, &ticket.Email,
			&teamsRaw, &ticket.ExpiresAt, &consumedAt, &ticket.CreatedAt,
		)
		if errors.Is(scanErr, sql.ErrNoRows) {
			return apperrors.ErrAccessDenied
		}
		if scanErr != nil {
			return scanErr
		}
		if err := json.Unmarshal(teamsRaw, &ticket.Teams); err != nil {
			return err
		}
		nowUTC := now.UTC()
		if consumedAt.Valid || !nowUTC.Before(ticket.ExpiresAt) || !strings.EqualFold(ticket.Host, host) {
			return apperrors.ErrAccessDenied
		}
		res := tx.Exec(`
			UPDATE public.hosted_app_auth_tickets
			SET consumed_at = ?
			WHERE ticket_sha256 = ? AND consumed_at IS NULL
		`, nowUTC, ticketSHA256)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return apperrors.ErrAccessDenied
		}
		ticket.ConsumedAt = &nowUTC
		return nil
	})
	if err != nil {
		return domainapphosting.AuthTicket{}, err
	}
	return ticket, nil
}

type PostgresAudienceDirectory struct {
	db *gorm.DB
}

func NewPostgresAudienceDirectory(db *gorm.DB) (*PostgresAudienceDirectory, error) {
	if db == nil {
		return nil, errors.New("apphosting audience directory database is required")
	}
	return &PostgresAudienceDirectory{db: db}, nil
}

func (d *PostgresAudienceDirectory) SearchCandidates(ctx context.Context, query string, limit int) (sohaapi.AppHostingAudienceCandidates, error) {
	if limit <= 0 {
		limit = 20
	}
	limit = min(limit, 100)
	trimmed := strings.TrimSpace(query)
	pattern := "%" + escapeILIKEPattern(trimmed) + "%"

	users, err := d.queryAudienceUsers(ctx, trimmed, pattern, limit)
	if err != nil {
		return sohaapi.AppHostingAudienceCandidates{}, err
	}
	teams, err := d.queryAudienceTeams(ctx, trimmed, pattern, limit)
	if err != nil {
		return sohaapi.AppHostingAudienceCandidates{}, err
	}
	return sohaapi.AppHostingAudienceCandidates{Users: users, Teams: teams}, nil
}

func (d *PostgresAudienceDirectory) queryAudienceUsers(ctx context.Context, trimmed, pattern string, limit int) ([]sohaapi.AppHostingAudienceUser, error) {
	rows, err := d.db.WithContext(ctx).Raw(`
		SELECT u.id::text, u.username, COALESCE(NULLIF(u.display_name, ''), u.username),
		       COALESCE((
		           SELECT t.name
		           FROM public.user_team_bindings utb
		           JOIN public.teams t ON t.id = utb.team_id
		           WHERE utb.user_id = u.id
		           ORDER BY t.name ASC
		           LIMIT 1
		       ), '')
		FROM public.users u
		WHERE u.status = 'active'
		  AND (? = '' OR u.username ILIKE ? ESCAPE '\' OR COALESCE(u.display_name, '') ILIKE ? ESCAPE '\')
		ORDER BY u.username ASC, u.id ASC
		LIMIT ?
	`, trimmed, pattern, pattern, limit).Rows()
	if err != nil {
		return nil, fmt.Errorf("search audience users: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]sohaapi.AppHostingAudienceUser, 0, limit)
	for rows.Next() {
		var u sohaapi.AppHostingAudienceUser
		if err := rows.Scan(&u.ID, &u.Username, &u.DisplayName, &u.Department); err != nil {
			return nil, err
		}
		items = append(items, u)
	}
	return items, rows.Err()
}

func (d *PostgresAudienceDirectory) queryAudienceTeams(ctx context.Context, trimmed, pattern string, limit int) ([]sohaapi.AppHostingAudienceTeam, error) {
	rows, err := d.db.WithContext(ctx).Raw(`
		SELECT t.id, t.name, t.slug
		FROM public.teams t
		WHERE (? = '' OR t.name ILIKE ? ESCAPE '\' OR t.slug ILIKE ? ESCAPE '\')
		ORDER BY t.name ASC, t.id ASC
		LIMIT ?
	`, trimmed, pattern, pattern, limit).Rows()
	if err != nil {
		return nil, fmt.Errorf("search audience teams: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]sohaapi.AppHostingAudienceTeam, 0, limit)
	for rows.Next() {
		var team sohaapi.AppHostingAudienceTeam
		if err := rows.Scan(&team.ID, &team.Name, &team.Slug); err != nil {
			return nil, err
		}
		items = append(items, team)
	}
	return items, rows.Err()
}

func (d *PostgresAudienceDirectory) ResolveVisitor(ctx context.Context, userID string) (domainapphosting.VisitorProfile, error) {
	var profile domainapphosting.VisitorProfile
	var status string
	var teamsRaw []byte
	err := d.db.WithContext(ctx).Raw(`
		SELECT u.id::text, u.username, COALESCE(NULLIF(u.display_name, ''), u.username),
		       u.email, u.status,
		       COALESCE((
		           SELECT json_agg(utb.team_id ORDER BY utb.team_id)
		           FROM public.user_team_bindings utb
		           WHERE utb.user_id = u.id
		       ), '[]'::json)
		FROM public.users u
		WHERE u.id::text = ?
		LIMIT 1
	`, strings.TrimSpace(userID)).Row().Scan(
		&profile.ID, &profile.Username, &profile.DisplayName, &profile.Email, &status, &teamsRaw,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domainapphosting.VisitorProfile{}, apperrors.ErrNotFound
	}
	if err != nil {
		return domainapphosting.VisitorProfile{}, fmt.Errorf("resolve live visitor: %w", err)
	}
	if !strings.EqualFold(status, "active") {
		return domainapphosting.VisitorProfile{}, domainapphosting.ErrVisitorDisabled
	}
	if err := json.Unmarshal(teamsRaw, &profile.TeamIDs); err != nil {
		return domainapphosting.VisitorProfile{}, fmt.Errorf("decode visitor teams: %w", err)
	}
	return profile, nil
}

func scanHostedApp(scanner rowScanner) (domainapphosting.App, error) {
	var app domainapphosting.App
	var kindStr, visModeStr, statusStr string
	var maintRaw, visUsersRaw, visTeamsRaw []byte
	err := scanner.Scan(
		&app.ID, &app.Name, &app.Description, &kindStr, &app.DNSLabel, &app.SpaFallback, &app.OwnerUserID,
		&maintRaw, &visModeStr, &visUsersRaw, &visTeamsRaw,
		&statusStr, &app.SuspendedReason, &app.ActiveRevisionID, &app.RouteGeneration,
		&app.IdempotencyKey, &app.RequestFingerprint, &app.CreatedAt, &app.UpdatedAt,
	)
	if err != nil {
		return domainapphosting.App{}, err
	}
	app.Kind = sohaapi.HostedAppKind(kindStr)
	app.Status = sohaapi.HostedAppStatus(statusStr)
	app.Visibility.Mode = sohaapi.HostedAppVisibilityMode(visModeStr)
	if err := json.Unmarshal(maintRaw, &app.MaintainerUserIDs); err != nil {
		return domainapphosting.App{}, err
	}
	if app.Visibility.Mode == sohaapi.HostedAppVisibilityModeSelected {
		if err := json.Unmarshal(visUsersRaw, &app.Visibility.UserIDs); err != nil {
			return domainapphosting.App{}, err
		}
		if err := json.Unmarshal(visTeamsRaw, &app.Visibility.TeamIDs); err != nil {
			return domainapphosting.App{}, err
		}
	}
	app.CreatedAt = app.CreatedAt.UTC()
	app.UpdatedAt = app.UpdatedAt.UTC()
	return app, nil
}

func scanHostedRevision(scanner rowScanner) (domainapphosting.Revision, error) {
	var rev domainapphosting.Revision
	var kindStr, statusStr string
	var manifestRaw []byte
	var activatedAt sql.NullTime
	err := scanner.Scan(
		&rev.ID, &rev.AppID, &rev.RevisionNumber, &rev.Description, &kindStr, &statusStr,
		&rev.BundleSha256, &rev.FileCount, &rev.TotalBytes, &rev.SpaFallback,
		&rev.StorageIntegrationID, &rev.StoragePrefix, &manifestRaw,
		&rev.FailureStage, &rev.FailureReason, &rev.IdempotencyKey,
		&rev.CreatedBy, &rev.CreatedAt, &activatedAt,
	)
	if err != nil {
		return domainapphosting.Revision{}, err
	}
	rev.Kind = sohaapi.HostedAppKind(kindStr)
	rev.Status = sohaapi.HostedAppRevisionStatus(statusStr)
	if len(manifestRaw) > 0 {
		if err := json.Unmarshal(manifestRaw, &rev.Files); err != nil {
			return domainapphosting.Revision{}, err
		}
	}
	rev.CreatedAt = rev.CreatedAt.UTC()
	if activatedAt.Valid {
		t := activatedAt.Time.UTC()
		rev.ActivatedAt = &t
	}
	return rev, nil
}

func marshalAppJSONArrays(app domainapphosting.App) (string, string, string, error) {
	maint, err := json.Marshal(nonNilStrings(app.MaintainerUserIDs))
	if err != nil {
		return "", "", "", err
	}
	users, err := json.Marshal(nonNilStrings(app.Visibility.UserIDs))
	if err != nil {
		return "", "", "", err
	}
	teams, err := json.Marshal(nonNilStrings(app.Visibility.TeamIDs))
	if err != nil {
		return "", "", "", err
	}
	return string(maint), string(users), string(teams), nil
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func nonNilManifest(files []sohaapi.HostedAppManifestFile) []sohaapi.HostedAppManifestFile {
	if files == nil {
		return []sohaapi.HostedAppManifestFile{}
	}
	return files
}

func nullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC()
}

func mapPostgresAppWriteError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		if strings.Contains(pgErr.ConstraintName, "dns_label") {
			return appapphosting.ErrDNSLabelConflict
		}
		return apperrors.ErrConflict
	}
	return err
}

func mapPostgresRevisionWriteError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return apperrors.ErrConflict
	}
	return err
}

func queryReadyHostedRevisionsForUpdate(ctx context.Context, tx *gorm.DB, appID string) ([]domainapphosting.Revision, error) {
	rows, err := tx.WithContext(ctx).Raw(`
		SELECT `+hostedRevisionColumns+`
		FROM public.hosted_app_revisions
		WHERE app_id = ? AND status = 'ready'
		ORDER BY revision_number DESC
		FOR UPDATE
	`, appID).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var ready []domainapphosting.Revision
	for rows.Next() {
		item, scanErr := scanHostedRevision(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		ready = append(ready, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return ready, nil
}
