package apphosting

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
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
	if req.Kind == sohaapi.HostedAppKindDynamic {
		settings = dynamicBundleLimits(settings)
	}
	blockers, warnings := evaluatePreflight(app, settings, req)
	if req.Kind == sohaapi.HostedAppKindDynamic && s.dynamic == nil {
		blockers = append(blockers, "dynamic HTTP application hosting is not configured")
	}

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

	publishVisibility, err := prepareRevisionVisibility(app, principal.UserID, normalizedInput)
	if err != nil {
		return sohaapi.HostedAppDeploymentReceipt{}, err
	}

	spooled, err := spoolCompressedBundle(bundle, settings.MaxBundleCompressedBytes)
	if err != nil {
		return sohaapi.HostedAppDeploymentReceipt{}, err
	}
	defer spooled.Close()

	if receipt, handled, err := s.checkUploadIdempotency(ctx, app, settings, normalizedInput.IdempotencyKey, spooled.sha256Hex, publishVisibility); handled || err != nil {
		return receipt, err
	}
	if publishVisibility != nil && publishVisibility.Scope.Mode == sohaapi.HostedAppVisibilityModeOrganization {
		if err := domainapphosting.CheckOrganizationVisibilityPolicy(settings); err != nil {
			return sohaapi.HostedAppDeploymentReceipt{}, err
		}
	}
	if app.Kind == sohaapi.HostedAppKindDynamic {
		return s.queueDynamicBundle(ctx, principal, app, settings, normalizedInput, spooled, publishVisibility)
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

	return s.persistAndActivateBundle(ctx, principal, app, settings, normalizedInput, spooled.sha256Hex, validated, publishVisibility)
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
		IdempotencyKey:    strings.TrimSpace(input.IdempotencyKey),
		SpaFallback:       input.SpaFallback,
		Description:       strings.TrimSpace(input.Description),
		PublishVisibility: input.PublishVisibility,
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
	if app.Kind == sohaapi.HostedAppKindDynamic {
		if s.dynamic == nil {
			return domainapphosting.App{}, domainapphosting.Settings{}, domainapphosting.UploadRevisionInput{}, apperrors.ErrUnsupportedOperation
		}
		if input.SpaFallback != nil && *input.SpaFallback {
			return domainapphosting.App{}, domainapphosting.Settings{}, domainapphosting.UploadRevisionInput{}, apperrors.ErrInvalidArgument
		}
		settings = dynamicBundleLimits(settings)
	}
	return app, settings, normalized, nil
}

func (s *Service) checkUploadIdempotency(ctx context.Context, app domainapphosting.App, settings domainapphosting.Settings, idempotencyKey, bundleSha256 string, publishVisibility *domainapphosting.RevisionVisibility) (sohaapi.HostedAppDeploymentReceipt, bool, error) {
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
	if existing.BundleSha256 != bundleSha256 ||
		(existing.PublicationVisibilityRecorded && !sameRevisionVisibility(existing.PublishVisibility, publishVisibility)) {
		return sohaapi.HostedAppDeploymentReceipt{}, false, apperrors.NewBusiness(apperrors.ErrConflict, "idempotency_key_conflict", "idempotencyKey was already used with a different bundle or sharing intent", "同一幂等键已被不同内容或分享范围的发布占用")
	}
	if app.Kind == sohaapi.HostedAppKindDynamic && existing.Status == sohaapi.HostedAppRevisionStatusUploading {
		return sohaapi.HostedAppDeploymentReceipt{}, false, nil
	}
	if existing.Status != sohaapi.HostedAppRevisionStatusReady && app.Kind != sohaapi.HostedAppKindDynamic {
		return sohaapi.HostedAppDeploymentReceipt{}, false, apperrors.NewBusiness(apperrors.ErrConflict, "idempotency_revision_not_ready", "previous deployment with this idempotencyKey is not in ready state", "该幂等键关联的上传记录未处于可用状态")
	}
	dto, err := s.enrichHostedApp(ctx, settings, app)
	if err != nil {
		return sohaapi.HostedAppDeploymentReceipt{}, false, err
	}
	return sohaapi.HostedAppDeploymentReceipt{
		App:                  dto,
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
	publishVisibility *domainapphosting.RevisionVisibility,
) (sohaapi.HostedAppDeploymentReceipt, error) {
	spaFallback := app.SpaFallback
	if input.SpaFallback != nil {
		spaFallback = *input.SpaFallback
	}
	now := s.now()
	revisionID := "rev_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	draft := domainapphosting.Revision{
		ID:                   revisionID,
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
		Files:                plannedManifestFiles(app.ID, revisionID, validated.Files),
		IdempotencyKey:       input.IdempotencyKey,
		CreatedBy:            firstNonEmpty(principal.UserID, principal.UserName),
		CreatedAt:            now,
		PublishVisibility:    publishVisibility,
	}
	createdRev, err := s.repo.CreateUploadingRevision(ctx, app.ID, draft)
	if err != nil {
		return sohaapi.HostedAppDeploymentReceipt{}, err
	}

	manifestFiles, err := s.uploadRevisionObjects(ctx, createdRev, validated.Files)
	if err != nil {
		return sohaapi.HostedAppDeploymentReceipt{}, s.failStaticUpload(ctx, createdRev, "upload", "object upload failed", err)
	}

	activated, err := s.repo.ActivateRevision(ctx, ActivateRevisionInput{
		AppID:                app.ID,
		RevisionID:           createdRev.ID,
		ActorUserID:          strings.TrimSpace(principal.UserID),
		SpaFallback:          spaFallback,
		Files:                manifestFiles,
		RetainedRevisionsMax: settings.RetainedRevisionsPerApp,
		Now:                  s.now(),
		BeforeActivate: func(ctx context.Context) error {
			return s.checkCurrentPublisher(ctx, createdRev.CreatedBy)
		},
	})
	if err != nil {
		reason := "revision activation failed"
		if errors.Is(err, apperrors.ErrAccessDenied) {
			reason = "publisher permission revoked"
		}
		return sohaapi.HostedAppDeploymentReceipt{}, s.failStaticUpload(ctx, createdRev, "activate", reason, err)
	}

	s.recordVisibilityUpdate(ctx, principal, activated.Visibility)
	s.recordAudit(ctx, principal, "revision.upload", "success", app.ID, "Uploaded and activated hosted application revision", map[string]any{
		"appId":           app.ID,
		"revisionId":      activated.Revision.ID,
		"revisionNumber":  activated.Revision.RevisionNumber,
		"bundleSha256":    bundleSha256,
		"fileCount":       activated.Revision.FileCount,
		"totalBytes":      activated.Revision.TotalBytes,
		"routeGeneration": activated.App.RouteGeneration,
	})
	appDTO, err := s.enrichHostedApp(ctx, settings, activated.App)
	if err != nil {
		return sohaapi.HostedAppDeploymentReceipt{}, err
	}
	return sohaapi.HostedAppDeploymentReceipt{
		App:                  appDTO,
		Revision:             activated.Revision.ToAPI(),
		ReusedIdempotencyKey: false,
	}, nil
}

func (s *Service) uploadRevisionObjects(ctx context.Context, rev domainapphosting.Revision, files []extractedBundleFile) ([]sohaapi.HostedAppManifestFile, error) {
	upload, cancel := context.WithTimeout(ctx, rev.CreatedAt.Add(staticArtifactUploadTimeout).Sub(s.now()))
	defer cancel()
	manifest := make([]sohaapi.HostedAppManifestFile, 0, len(files))
	for _, file := range files {
		if err := upload.Err(); err != nil {
			return nil, err
		}
		objectKey := rev.AppID + "/" + rev.ID + "/" + file.Path
		storeKey := joinStoragePrefixKey(rev.StoragePrefix, objectKey)
		size, sha256Hex, etag, err := s.objectStore.PutObject(upload, rev.StorageIntegrationID, storeKey, file.ContentType, bytes.NewReader(file.Data))
		if err != nil {
			return nil, err
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
	return manifest, upload.Err()
}

func plannedManifestFiles(appID, revisionID string, files []extractedBundleFile) []sohaapi.HostedAppManifestFile {
	out := make([]sohaapi.HostedAppManifestFile, 0, len(files))
	for _, file := range files {
		out = append(out, sohaapi.HostedAppManifestFile{
			Path:        file.Path,
			SizeBytes:   int64(len(file.Data)),
			Sha256:      sha256HexBytes(file.Data),
			ContentType: file.ContentType,
			ObjectKey:   appID + "/" + revisionID + "/" + file.Path,
		})
	}
	return out
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
	updatedApp, targetRev, reused, err := s.rollbackRevision(ctx, app, principal.UserID, trimmedRevID, idempotencyKey)
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
	appDTO, err := s.enrichHostedApp(ctx, settings, updatedApp)
	if err != nil {
		return sohaapi.HostedAppDeploymentReceipt{}, err
	}
	return sohaapi.HostedAppDeploymentReceipt{
		App:                  appDTO,
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
