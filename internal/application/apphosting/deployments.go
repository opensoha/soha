package apphosting

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainapphosting "github.com/opensoha/soha/internal/domain/apphosting"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func wrapTypedBusinessError(sentinel error, code, en, zh string) error {
	return apperrors.NewBusiness(sentinel, code, en, zh)
}

func (s *Service) Preflight(ctx context.Context, principal domainidentity.Principal, appID string, req sohaapi.HostedAppPreflightRequest) (sohaapi.HostedAppPreflightResult, error) {
	if err := s.authorize(ctx, principal, appaccess.PermAppHostingAppsPublish); err != nil {
		return sohaapi.HostedAppPreflightResult{}, err
	}
	trimmedID, err := validateIdentifier(appID, "appId")
	if err != nil {
		return sohaapi.HostedAppPreflightResult{}, err
	}
	app, err := s.repo.GetAppByID(ctx, trimmedID)
	if err != nil {
		return sohaapi.HostedAppPreflightResult{}, err
	}
	if !app.IsOwnerOrMaintainer(principal.UserID) {
		return sohaapi.HostedAppPreflightResult{}, apperrors.ErrAccessDenied
	}
	if !req.Kind.Valid() || req.UncompressedBytes < 0 || req.FileCount < 0 || (req.CompressedBytes != nil && *req.CompressedBytes < 0) {
		return sohaapi.HostedAppPreflightResult{}, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_preflight_request", "preflight request fields are invalid", "预检请求参数不合法")
	}

	settings, err := s.repo.GetSettings(ctx)
	if err != nil {
		return sohaapi.HostedAppPreflightResult{}, err
	}
	settings = settings.WithDefaults()
	blockers, warnings := evaluatePreflight(app, settings, req)

	return sohaapi.HostedAppPreflightResult{
		Allowed:              len(blockers) == 0,
		AppID:                app.ID,
		URL:                  app.URL(settings),
		Blockers:             blockers,
		Warnings:             warnings,
		MaxCompressedBytes:   settings.MaxBundleCompressedBytes,
		MaxUncompressedBytes: settings.MaxBundleUncompressedBytes,
		MaxFileCount:         settings.MaxBundleFileCount,
	}, nil
}

func evaluatePreflight(app domainapphosting.App, settings domainapphosting.Settings, req sohaapi.HostedAppPreflightRequest) ([]string, []string) {
	blockers := make([]string, 0)
	warnings := make([]string, 0)

	if !settings.Enabled {
		blockers = append(blockers, "workspace App Hosting is currently disabled by administrator")
	}
	if settings.StorageIntegrationID == "" {
		blockers = append(blockers, "object storage integration is not configured")
	}
	switch app.Status {
	case sohaapi.HostedAppStatusSuspendedByAdmin:
		blockers = append(blockers, "application is suspended by administrator")
	case sohaapi.HostedAppStatusOffline:
		blockers = append(blockers, "application is currently offline; bring it online before deploying")
	}
	if req.Kind != app.Kind {
		blockers = append(blockers, fmt.Sprintf("bundle kind %q does not match application kind %q", req.Kind, app.Kind))
	}
	if req.Kind == sohaapi.HostedAppKindDynamic {
		blockers = append(blockers, "dynamic HTTP application hosting is not available in this release")
	}
	if req.Kind == sohaapi.HostedAppKindStatic && !req.HasIndexHTML {
		blockers = append(blockers, "static bundle must contain root index.html")
	}
	blockers = appendPreflightLimitBlockers(blockers, settings, req)
	blockers, warnings = appendPreflightPathFindings(blockers, warnings, req.FlaggedFiles, req.SamplePaths)
	return blockers, warnings
}

func appendPreflightLimitBlockers(blockers []string, settings domainapphosting.Settings, req sohaapi.HostedAppPreflightRequest) []string {
	if req.FileCount == 0 {
		blockers = append(blockers, "bundle must include at least one regular file")
	}
	if req.FileCount > settings.MaxBundleFileCount {
		blockers = append(blockers, fmt.Sprintf("fileCount %d exceeds limit %d", req.FileCount, settings.MaxBundleFileCount))
	}
	if req.UncompressedBytes > settings.MaxBundleUncompressedBytes {
		blockers = append(blockers, fmt.Sprintf("uncompressedBytes %d exceeds limit %d", req.UncompressedBytes, settings.MaxBundleUncompressedBytes))
	}
	if req.CompressedBytes != nil && *req.CompressedBytes > settings.MaxBundleCompressedBytes {
		blockers = append(blockers, fmt.Sprintf("compressedBytes %d exceeds limit %d", *req.CompressedBytes, settings.MaxBundleCompressedBytes))
	}
	return blockers
}

func appendPreflightPathFindings(blockers, warnings []string, flaggedFiles, samplePaths []string) ([]string, []string) {
	for _, item := range flaggedFiles {
		trimmed := strings.TrimSpace(item)
		if trimmed != "" {
			blockers = append(blockers, fmt.Sprintf("flagged sensitive or disallowed file: %s", trimmed))
		}
	}
	for _, sample := range samplePaths {
		trimmed := strings.TrimSpace(sample)
		if trimmed == "" {
			continue
		}
		rel, err := normalizeBundleRelativePath(trimmed, false)
		if err != nil {
			blockers = append(blockers, fmt.Sprintf("invalid path %q", trimmed))
			continue
		}
		if reason, forbidden := checkForbiddenCredentialPath(rel); forbidden {
			blockers = append(blockers, fmt.Sprintf("forbidden credential file %q (%s)", rel, reason))
		}
		ext := strings.ToLower(path.Ext(rel))
		if ext == ".sqlite" || ext == ".sqlite3" || ext == ".db" {
			warnings = append(warnings, fmt.Sprintf("local database file %q detected; static hosting is read-only", rel))
		}
	}
	return blockers, warnings
}

func (s *Service) UploadRevision(ctx context.Context, principal domainidentity.Principal, appID string, input domainapphosting.UploadRevisionInput, bundle io.Reader) (sohaapi.HostedAppDeploymentReceipt, error) {
	app, settings, normalizedInput, err := s.prepareRevisionUpload(ctx, principal, appID, input)
	if err != nil {
		return sohaapi.HostedAppDeploymentReceipt{}, err
	}

	spooled, err := spoolCompressedBundle(bundle, settings.MaxBundleCompressedBytes)
	if err != nil {
		return sohaapi.HostedAppDeploymentReceipt{}, err
	}
	defer spooled.Close()

	if receipt, handled, err := s.checkUploadIdempotency(ctx, app, settings, normalizedInput.IdempotencyKey, spooled.sha256Hex); handled || err != nil {
		return receipt, err
	}

	archiveReader, err := spooled.OpenReader()
	if err != nil {
		return sohaapi.HostedAppDeploymentReceipt{}, err
	}
	validated, err := extractAndValidateStaticBundle(ctx, archiveReader, settings)
	if err != nil {
		s.recordAudit(ctx, principal, "revision.upload", "denied", app.ID, "Rejected hosted application bundle during validation", map[string]any{
			"appId":  app.ID,
			"reason": err.Error(),
		})
		return sohaapi.HostedAppDeploymentReceipt{}, err
	}

	return s.persistAndActivateBundle(ctx, principal, app, settings, normalizedInput, spooled.sha256Hex, validated)
}

func (s *Service) prepareRevisionUpload(ctx context.Context, principal domainidentity.Principal, appID string, input domainapphosting.UploadRevisionInput) (domainapphosting.App, domainapphosting.Settings, domainapphosting.UploadRevisionInput, error) {
	if err := s.authorize(ctx, principal, appaccess.PermAppHostingAppsPublish); err != nil {
		return domainapphosting.App{}, domainapphosting.Settings{}, domainapphosting.UploadRevisionInput{}, err
	}
	trimmedID, err := validateIdentifier(appID, "appId")
	if err != nil {
		return domainapphosting.App{}, domainapphosting.Settings{}, domainapphosting.UploadRevisionInput{}, err
	}
	normalized := domainapphosting.UploadRevisionInput{
		IdempotencyKey: strings.TrimSpace(input.IdempotencyKey),
		SpaFallback:    input.SpaFallback,
		Description:    strings.TrimSpace(input.Description),
	}
	if len(normalized.IdempotencyKey) > 128 || utf8.RuneCountInString(normalized.Description) > 512 {
		return domainapphosting.App{}, domainapphosting.Settings{}, domainapphosting.UploadRevisionInput{}, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_upload_params", "idempotencyKey or description exceeds maximum length", "上传参数超出长度限制")
	}
	settings, err := s.repo.GetSettings(ctx)
	if err != nil {
		return domainapphosting.App{}, domainapphosting.Settings{}, domainapphosting.UploadRevisionInput{}, err
	}
	settings = settings.WithDefaults()
	if !settings.Enabled || settings.StorageIntegrationID == "" || s.objectStore == nil {
		return domainapphosting.App{}, domainapphosting.Settings{}, domainapphosting.UploadRevisionInput{}, apperrors.NewBusiness(apperrors.ErrServiceUnavailable, "app_hosting_unavailable", "workspace App Hosting or object storage is not enabled", "工作区应用托管或对象存储尚未启用")
	}
	app, err := s.repo.GetAppByID(ctx, trimmedID)
	if err != nil {
		return domainapphosting.App{}, domainapphosting.Settings{}, domainapphosting.UploadRevisionInput{}, err
	}
	if !app.IsOwnerOrMaintainer(principal.UserID) {
		return domainapphosting.App{}, domainapphosting.Settings{}, domainapphosting.UploadRevisionInput{}, apperrors.ErrAccessDenied
	}
	if app.Kind != sohaapi.HostedAppKindStatic {
		return domainapphosting.App{}, domainapphosting.Settings{}, domainapphosting.UploadRevisionInput{}, apperrors.ErrUnsupportedOperation
	}
	return app, settings, normalized, nil
}

func (s *Service) checkUploadIdempotency(ctx context.Context, app domainapphosting.App, settings domainapphosting.Settings, idempotencyKey, bundleSha256 string) (sohaapi.HostedAppDeploymentReceipt, bool, error) {
	if idempotencyKey == "" {
		return sohaapi.HostedAppDeploymentReceipt{}, false, nil
	}
	existing, err := s.repo.FindRevisionByIdempotencyKey(ctx, app.ID, idempotencyKey)
	if errors.Is(err, apperrors.ErrNotFound) {
		return sohaapi.HostedAppDeploymentReceipt{}, false, nil
	}
	if err != nil {
		return sohaapi.HostedAppDeploymentReceipt{}, false, err
	}
	if existing.BundleSha256 != bundleSha256 {
		return sohaapi.HostedAppDeploymentReceipt{}, false, apperrors.NewBusiness(apperrors.ErrConflict, "idempotency_key_conflict", "idempotencyKey was already used with a different bundle digest", "同一幂等键已被不同内容的构建包占用")
	}
	if existing.Status != sohaapi.HostedAppRevisionStatusReady {
		return sohaapi.HostedAppDeploymentReceipt{}, false, apperrors.NewBusiness(apperrors.ErrConflict, "idempotency_revision_not_ready", "previous deployment with this idempotencyKey is not in ready state", "该幂等键关联的上传记录未处于可用状态")
	}
	return sohaapi.HostedAppDeploymentReceipt{
		App:                  app.ToAPI(settings),
		Revision:             existing.ToAPI(),
		ReusedIdempotencyKey: true,
	}, true, nil
}

func (s *Service) persistAndActivateBundle(
	ctx context.Context,
	principal domainidentity.Principal,
	app domainapphosting.App,
	settings domainapphosting.Settings,
	input domainapphosting.UploadRevisionInput,
	bundleSha256 string,
	validated *validatedBundle,
) (sohaapi.HostedAppDeploymentReceipt, error) {
	spaFallback := app.SpaFallback
	if input.SpaFallback != nil {
		spaFallback = *input.SpaFallback
	}
	now := s.now()
	draft := domainapphosting.Revision{
		ID:                   "rev_" + strings.ReplaceAll(uuid.NewString(), "-", ""),
		AppID:                app.ID,
		Description:          input.Description,
		Kind:                 app.Kind,
		Status:               sohaapi.HostedAppRevisionStatusUploading,
		BundleSha256:         bundleSha256,
		FileCount:            boundedFileCount(len(validated.Files)),
		TotalBytes:           validated.TotalBytes,
		SpaFallback:          spaFallback,
		StorageIntegrationID: settings.StorageIntegrationID,
		StoragePrefix:        settings.StoragePrefix,
		IdempotencyKey:       input.IdempotencyKey,
		CreatedBy:            firstNonEmpty(principal.UserID, principal.UserName),
		CreatedAt:            now,
	}
	createdRev, err := s.repo.CreateUploadingRevision(ctx, app.ID, draft)
	if err != nil {
		return sohaapi.HostedAppDeploymentReceipt{}, err
	}

	manifestFiles, storeKeys, err := s.uploadRevisionObjects(ctx, createdRev, validated.Files)
	if err != nil {
		_ = s.repo.FailRevision(context.WithoutCancel(ctx), app.ID, createdRev.ID, "upload", err.Error())
		s.bestEffortDeleteKeys(ctx, createdRev.StorageIntegrationID, storeKeys)
		return sohaapi.HostedAppDeploymentReceipt{}, err
	}

	activated, err := s.repo.ActivateRevision(ctx, ActivateRevisionInput{
		AppID:                app.ID,
		RevisionID:           createdRev.ID,
		SpaFallback:          spaFallback,
		Files:                manifestFiles,
		RetainedRevisionsMax: settings.RetainedRevisionsPerApp,
		Now:                  s.now(),
	})
	if err != nil {
		s.bestEffortDeleteKeys(ctx, createdRev.StorageIntegrationID, storeKeys)
		return sohaapi.HostedAppDeploymentReceipt{}, err
	}

	for _, evicted := range activated.EvictedRevisions {
		s.bestEffortDeleteKeys(ctx, evicted.StorageIntegrationID, buildRevisionStoreKeys(evicted))
	}
	s.recordAudit(ctx, principal, "revision.upload", "success", app.ID, "Uploaded and activated hosted application revision", map[string]any{
		"appId":           app.ID,
		"revisionId":      activated.Revision.ID,
		"revisionNumber":  activated.Revision.RevisionNumber,
		"bundleSha256":    bundleSha256,
		"fileCount":       activated.Revision.FileCount,
		"totalBytes":      activated.Revision.TotalBytes,
		"routeGeneration": activated.App.RouteGeneration,
	})
	return sohaapi.HostedAppDeploymentReceipt{
		App:                  activated.App.ToAPI(settings),
		Revision:             activated.Revision.ToAPI(),
		ReusedIdempotencyKey: false,
	}, nil
}

func (s *Service) uploadRevisionObjects(ctx context.Context, rev domainapphosting.Revision, files []extractedBundleFile) ([]sohaapi.HostedAppManifestFile, []string, error) {
	manifest := make([]sohaapi.HostedAppManifestFile, 0, len(files))
	uploadedKeys := make([]string, 0, len(files))
	for _, file := range files {
		objectKey := rev.AppID + "/" + rev.ID + "/" + file.Path
		storeKey := joinStoragePrefixKey(rev.StoragePrefix, objectKey)
		uploadedKeys = append(uploadedKeys, storeKey)
		size, sha256Hex, etag, err := s.objectStore.PutObject(ctx, rev.StorageIntegrationID, storeKey, file.ContentType, bytes.NewReader(file.Data))
		if err != nil {
			return nil, uploadedKeys, err
		}
		manifest = append(manifest, sohaapi.HostedAppManifestFile{
			Path:        file.Path,
			SizeBytes:   size,
			Sha256:      sha256Hex,
			ContentType: file.ContentType,
			Etag:        etag,
			ObjectKey:   objectKey,
		})
	}
	return manifest, uploadedKeys, nil
}

func (s *Service) bestEffortDeleteKeys(ctx context.Context, integrationID string, keys []string) {
	if s.objectStore == nil || strings.TrimSpace(integrationID) == "" || len(keys) == 0 {
		return
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	_ = s.objectStore.DeleteKeys(cleanupCtx, integrationID, keys)
}

func buildRevisionStoreKeys(rev domainapphosting.Revision) []string {
	keys := make([]string, 0, len(rev.Files))
	for _, file := range rev.Files {
		keys = append(keys, joinStoragePrefixKey(rev.StoragePrefix, file.ObjectKey))
	}
	return keys
}

func joinStoragePrefixKey(storagePrefix, objectKey string) string {
	cleanPrefix := strings.Trim(strings.TrimSpace(storagePrefix), "/")
	cleanObj := strings.TrimLeft(strings.TrimSpace(objectKey), "/")
	if cleanPrefix == "" {
		return cleanObj
	}
	return cleanPrefix + "/" + cleanObj
}

func (s *Service) ListRevisions(ctx context.Context, principal domainidentity.Principal, appID string, limit, offset int) (sohaapi.HostedAppRevisionList, error) {
	app, _, err := s.loadAuthorizedAppRead(ctx, principal, appID)
	if err != nil {
		return sohaapi.HostedAppRevisionList{}, err
	}
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 200 || offset < 0 {
		return sohaapi.HostedAppRevisionList{}, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_pagination", "limit must be 1..200 and offset >= 0", "分页参数无效")
	}
	revisions, total, err := s.repo.ListRevisions(ctx, app.ID, limit, offset)
	if err != nil {
		return sohaapi.HostedAppRevisionList{}, err
	}
	items := make([]sohaapi.HostedAppRevision, 0, len(revisions))
	for _, rev := range revisions {
		items = append(items, rev.ToAPI())
	}
	return sohaapi.HostedAppRevisionList{Items: items, Total: total}, nil
}

func (s *Service) GetRevision(ctx context.Context, principal domainidentity.Principal, appID, revisionID string) (sohaapi.HostedAppRevision, error) {
	app, _, err := s.loadAuthorizedAppRead(ctx, principal, appID)
	if err != nil {
		return sohaapi.HostedAppRevision{}, err
	}
	trimmedRevID, err := validateIdentifier(revisionID, "revisionId")
	if err != nil {
		return sohaapi.HostedAppRevision{}, err
	}
	rev, err := s.repo.GetRevision(ctx, app.ID, trimmedRevID)
	if err != nil {
		return sohaapi.HostedAppRevision{}, err
	}
	return rev.ToAPI(), nil
}

func (s *Service) Rollback(ctx context.Context, principal domainidentity.Principal, appID string, req sohaapi.HostedAppRollbackRequest) (sohaapi.HostedAppDeploymentReceipt, error) {
	if err := s.authorize(ctx, principal, appaccess.PermAppHostingAppsPublish); err != nil {
		return sohaapi.HostedAppDeploymentReceipt{}, err
	}
	trimmedAppID, err := validateIdentifier(appID, "appId")
	if err != nil {
		return sohaapi.HostedAppDeploymentReceipt{}, err
	}
	trimmedRevID, err := validateIdentifier(req.TargetRevisionID, "targetRevisionId")
	if err != nil {
		return sohaapi.HostedAppDeploymentReceipt{}, err
	}
	if utf8.RuneCountInString(strings.TrimSpace(req.Reason)) > 512 || len(strings.TrimSpace(req.IdempotencyKey)) > 128 {
		return sohaapi.HostedAppDeploymentReceipt{}, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_rollback_request", "reason or idempotencyKey exceeds maximum length", "回滚参数超出长度限制")
	}
	app, err := s.repo.GetAppByID(ctx, trimmedAppID)
	if err != nil {
		return sohaapi.HostedAppDeploymentReceipt{}, err
	}
	if !app.IsOwnerOrMaintainer(principal.UserID) {
		return sohaapi.HostedAppDeploymentReceipt{}, apperrors.ErrAccessDenied
	}
	settings, err := s.repo.GetSettings(ctx)
	if err != nil {
		return sohaapi.HostedAppDeploymentReceipt{}, err
	}

	idempotencyKey := strings.TrimSpace(req.IdempotencyKey)
	updatedApp, targetRev, reused, err := s.repo.RollbackRevision(ctx, app.ID, trimmedRevID, idempotencyKey, s.now())
	if err != nil {
		return sohaapi.HostedAppDeploymentReceipt{}, err
	}
	if !reused {
		s.recordAudit(ctx, principal, "app.rollback", "success", updatedApp.ID, "Rolled back hosted application active revision", map[string]any{
			"appId":            updatedApp.ID,
			"targetRevisionId": targetRev.ID,
			"reason":           strings.TrimSpace(req.Reason),
			"routeGeneration":  updatedApp.RouteGeneration,
		})
	}
	return sohaapi.HostedAppDeploymentReceipt{
		App:                  updatedApp.ToAPI(settings),
		Revision:             targetRev.ToAPI(),
		ReusedIdempotencyKey: reused,
	}, nil
}

func boundedFileCount(n int) int32 {
	const maxInt32 = int(^uint32(0) >> 1)
	if n <= 0 {
		return 0
	}
	if n > maxInt32 {
		return int32(maxInt32)
	}
	return int32(n) // #nosec G115 -- explicitly bounded to [0, math.MaxInt32]
}
