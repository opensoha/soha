package apphosting

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path"
	"strings"

	"github.com/google/uuid"
	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	domain "github.com/opensoha/soha/internal/domain/apphosting"
	"github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const maxDynamicCompressedBytes = 30 << 20

type dynamicProject struct {
	Template      string `json:"template"`
	Database      bool   `json:"database"`
	SchemaVersion int    `json:"schemaVersion"`
}

func dynamicBundleLimits(settings domain.Settings) domain.Settings {
	settings.MaxBundleCompressedBytes = min(settings.MaxBundleCompressedBytes, maxDynamicCompressedBytes)
	settings.MaxBundleUncompressedBytes = min(settings.MaxBundleUncompressedBytes, 150<<20)
	settings.MaxBundleFileCount = min(settings.MaxBundleFileCount, 2000)
	return settings
}

func classifyDynamicSourceFile(ctx context.Context, name string, data []byte, consumed *int64) (string, error) {
	for _, part := range strings.Split(strings.ToLower(name), "/") {
		switch part {
		case "dockerfile", ".dockerignore", ".npmrc", "pip.conf", ".git", "node_modules", ".venv", "__pycache__":
			return "", newBundleRejectedError("dynamic_source_path", "source includes a disallowed build override or local dependency directory", "源码包含禁止的构建配置或本地依赖目录")
		}
	}
	switch strings.ToLower(path.Ext(name)) {
	case ".py", ".ts", ".tsx", ".jsx":
		return "text/plain; charset=utf-8", inspectTextBundleFile(ctx, name, data, consumed)
	default:
		return classifyAndInspectBundleFile(ctx, name, data, consumed)
	}
}

func validateDynamicProject(bundle *validatedBundle) (dynamicProject, error) {
	config := dynamicProject{}
	files := make(map[string][]byte, len(bundle.Files))
	for _, file := range bundle.Files {
		files[file.Path] = file.Data
	}
	data := files["soha-host.json"]
	const maxConfigBytes = 4096
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if len(data) > maxConfigBytes || decoder.Decode(&config) != nil || !errors.Is(decoder.Decode(new(any)), io.EOF) {
		return dynamicProject{}, newBundleRejectedError("dynamic_manifest_invalid", "source root requires a valid soha-host.json hosting manifest", "源码根目录需要有效的 soha-host.json 托管配置")
	}
	if config.Database && config.SchemaVersion == 0 {
		config.SchemaVersion = 1
	}
	if (!config.Database && config.SchemaVersion != 0) ||
		(config.Database && (config.SchemaVersion < 1 || config.SchemaVersion > 2)) {
		return dynamicProject{}, newBundleRejectedError("dynamic_schema_invalid", "unsupported managed data schema version", "受管数据库结构版本不受支持")
	}
	var required []string
	switch config.Template {
	case "node":
		required = []string{"server.js", "package.json", "package-lock.json"}
	case "python":
		required = []string{"app.py", "requirements.txt"}
	default:
		return dynamicProject{}, newBundleRejectedError("dynamic_template_invalid", "hosting template must be node or python", "托管模板必须为 node 或 python")
	}
	for _, name := range required {
		if _, exists := files[name]; !exists {
			return dynamicProject{}, newBundleRejectedError("dynamic_source_incomplete", "source is missing required file "+name, "源码缺少必需文件 "+name)
		}
	}
	return config, nil
}

type boundedSourceArchive struct {
	bytes.Buffer
	limit int64
}

func (b *boundedSourceArchive) Write(p []byte) (int, error) {
	if int64(len(p)) > b.limit-int64(b.Len()) {
		return 0, newPayloadTooLargeError("validated source archive exceeds compressed upload limit", "校验后的源码归档超过压缩大小限制")
	}
	return b.Buffer.Write(p)
}

func canonicalDynamicArchive(ctx context.Context, bundle *validatedBundle, limit int64) ([]byte, error) {
	buffer := &boundedSourceArchive{limit: limit}
	gz := gzip.NewWriter(buffer)
	writer := tar.NewWriter(gz)
	for _, file := range bundle.Files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		hdr := &tar.Header{Name: file.Path, Mode: 0o644, Size: int64(len(file.Data)), Typeflag: tar.TypeReg}
		if err := writer.WriteHeader(hdr); err != nil {
			return nil, err
		}
		if _, err := writer.Write(file.Data); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func (s *Service) queueDynamicBundle(ctx context.Context, actor identity.Principal, app domain.App, settings domain.Settings, input domain.UploadRevisionInput, spooled *spooledBundle, publishVisibility *domain.RevisionVisibility) (sohaapi.HostedAppDeploymentReceipt, error) {
	reader, err := spooled.OpenReader()
	if err != nil {
		return sohaapi.HostedAppDeploymentReceipt{}, err
	}
	bundle, err := extractAndValidateBundle(ctx, reader, settings, false)
	if err != nil {
		return sohaapi.HostedAppDeploymentReceipt{}, err
	}
	project, err := validateDynamicProject(bundle)
	if err != nil {
		return sohaapi.HostedAppDeploymentReceipt{}, err
	}
	if err := s.checkSensitiveDynamicRevision(ctx, app, domain.Revision{Database: project.Database}, actor.UserID); err != nil {
		return sohaapi.HostedAppDeploymentReceipt{}, err
	}
	archive, err := canonicalDynamicArchive(ctx, bundle, settings.MaxBundleCompressedBytes)
	if err != nil {
		return sohaapi.HostedAppDeploymentReceipt{}, err
	}
	limits := settings.EffectiveGovernance().RuntimeLimits
	revision := domain.Revision{
		RuntimeLimits: &limits,
		ID:            "rev_" + uuid.New().String(),
		AppID:         app.ID, Kind: app.Kind, Description: input.Description, BundleSha256: spooled.sha256Hex,
		FileCount: boundedFileCount(len(bundle.Files)), TotalBytes: bundle.TotalBytes,
		StorageIntegrationID: settings.StorageIntegrationID, StoragePrefix: settings.StoragePrefix,
		IdempotencyKey: input.IdempotencyKey, CreatedAt: s.now(), CreatedBy: actor.UserID,
		PublishVisibility: publishVisibility,
	}
	revision.ID = strings.ReplaceAll(revision.ID, "-", "")
	revision, err = s.repo.CreateUploadingRevision(ctx, app.ID, revision)
	if err != nil {
		if input.IdempotencyKey == "" {
			return sohaapi.HostedAppDeploymentReceipt{}, err
		}
		if receipt, handled, idemErr := s.checkUploadIdempotency(ctx, app, settings, input.IdempotencyKey, spooled.sha256Hex, publishVisibility); handled || idemErr != nil {
			return receipt, idemErr
		}
		existing, lookupErr := s.repo.FindRevisionByIdempotencyKey(ctx, app.ID, input.IdempotencyKey)
		if lookupErr != nil || existing.BundleSha256 != spooled.sha256Hex || existing.Status != sohaapi.HostedAppRevisionStatusUploading {
			return sohaapi.HostedAppDeploymentReceipt{}, err
		}
		revision = existing
	}
	return s.commitDynamicSource(ctx, actor, app, settings, revision, project, archive)
}

func (s *Service) commitDynamicSource(ctx context.Context, actor identity.Principal, app domain.App, settings domain.Settings, revision domain.Revision, project dynamicProject, archive []byte) (sohaapi.HostedAppDeploymentReceipt, error) {
	repo, ok := s.repo.(dynamicRepository)
	if !ok {
		return sohaapi.HostedAppDeploymentReceipt{}, apperrors.ErrUnsupportedOperation
	}
	key := dynamicSourceKey(revision)
	upload, cancelUpload := context.WithDeadline(ctx, revision.CreatedAt.Add(revisionDeadline))
	defer cancelUpload()
	if err := upload.Err(); err != nil {
		return sohaapi.HostedAppDeploymentReceipt{}, err
	}
	_, _, _, err := s.objectStore.PutObject(upload, revision.StorageIntegrationID, key, "application/gzip", bytes.NewReader(archive))
	if err != nil {
		_, cleanupErr := s.discardInactiveDynamicSource(ctx, repo, revision, key)
		return sohaapi.HostedAppDeploymentReceipt{}, errors.Join(err, cleanupErr)
	}
	queueCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), revisionStepTimeout)
	defer cancel()
	err = repo.QueueDynamicRevision(queueCtx, app.ID, revision.ID, key, project.Template, project.Database, project.SchemaVersion, s.now())
	if err != nil {
		if deleted, cleanupErr := s.discardInactiveDynamicSource(ctx, repo, revision, key); deleted || cleanupErr != nil {
			return sohaapi.HostedAppDeploymentReceipt{}, errors.Join(err, cleanupErr)
		}
		if receipt, handled, idemErr := s.checkUploadIdempotency(queueCtx, app, settings, revision.IdempotencyKey, revision.BundleSha256, revision.PublishVisibility); handled || idemErr != nil {
			return receipt, idemErr
		}
		// A lost acknowledgment can leave a valid queue entry. Keep its source for reconciliation.
		return sohaapi.HostedAppDeploymentReceipt{}, err
	}
	revision.Status = sohaapi.HostedAppRevisionStatusValidating
	revision.BuildPhase = "queued"
	s.recordAudit(queueCtx, actor, "revision.upload", "success", app.ID, "Queued dynamic hosted application revision", map[string]any{"revisionId": revision.ID})
	dto, err := s.enrichHostedApp(queueCtx, settings, app)
	return sohaapi.HostedAppDeploymentReceipt{App: dto, Revision: revision.ToAPI()}, err
}

func (s *Service) discardInactiveDynamicSource(ctx context.Context, repo deletedDynamicRepository, revision domain.Revision, key string) (bool, error) {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), revisionStepTimeout)
	defer cancel()
	stored, err := s.repo.GetRevision(cleanup, revision.AppID, revision.ID)
	if err == nil {
		// If cleanup is pending, its worker removes the source after revoking access.
		if stored.Kind == sohaapi.HostedAppKindDynamic && stored.NextCheckAt.IsZero() && (stored.SourceKey == "" || stored.SourceKey == key) {
			switch stored.Status {
			case sohaapi.HostedAppRevisionStatusFailed, sohaapi.HostedAppRevisionStatusEvicted:
				return false, s.objectStore.DeleteKeys(cleanup, revision.StorageIntegrationID, []string{key})
			}
		}
		return false, nil
	}
	if !errors.Is(err, apperrors.ErrNotFound) {
		return false, nil
	}
	revision.SourceKey = key
	// The original cleanup may have completed before the late object write finished.
	queueErr := repo.RequeueDeletedDynamicCleanup(cleanup, revision)
	return true, errors.Join(queueErr, s.objectStore.DeleteKeys(cleanup, revision.StorageIntegrationID, []string{key}))
}

func dynamicSourceKey(revision domain.Revision) string {
	return joinStoragePrefixKey(revision.StoragePrefix, revision.AppID+"/"+revision.ID+"/source.tar.gz")
}
