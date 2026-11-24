package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"time"

	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	appintegration "github.com/opensoha/soha/internal/application/systemintegration"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainplugin "github.com/opensoha/soha/internal/domain/plugin"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type executableReleaseHistory interface {
	PreviousRelease(context.Context, string) (domainplugin.InstalledPlugin, error)
	RetainRelease(context.Context, domainplugin.InstalledPlugin) error
}

func (s *Service) verifyLocalRelease(ctx context.Context, item domainplugin.InstalledPlugin) error {
	if s.executable == nil || !repositoryInstalled(item) || item.SignatureStatus != "verified" ||
		!publisherFingerprintPattern.MatchString(item.SourceInfo.PublisherKeyFingerprint) ||
		!publisherFingerprintPattern.MatchString(item.SourceInfo.ReleaseAttestationDigest) {
		return apperrors.ErrUnsupportedOperation
	}
	info := item.SourceInfo
	claims := repositoryPreviewClaims{Repository: firstNonEmpty(info.RepositoryURL, info.CanonicalSource),
		Ref: info.PinnedRef, Commit: info.CommitSha, Checksum: info.RawManifestChecksum, MarketplaceOfficial: info.Official}
	return s.executable.assets.VerifyLocal(ctx, claims.previewSource(), item.Manifest, info.PublisherKeyFingerprint, info.ReleaseAttestationDigest)
}

func (s *Service) withExecutableMutation() func() {
	if s.executable == nil {
		return func() {}
	}
	s.executable.mu.Lock()
	return s.executable.mu.Unlock
}

func (s *Service) commitExecutableChange(ctx context.Context, expected, item domainplugin.InstalledPlugin, retain bool) (domainplugin.InstalledPlugin, error) {
	if s.executable == nil {
		return domainplugin.InstalledPlugin{}, apperrors.ErrUnsupportedOperation
	}
	var beforeUpdate func(context.Context, *domainplugin.InstalledPlugin) error
	if retain {
		history, ok := s.repo.(executableReleaseHistory)
		if !ok {
			return domainplugin.InstalledPlugin{}, apperrors.ErrUnsupportedOperation
		}
		beforeUpdate = func(ctx context.Context, _ *domainplugin.InstalledPlugin) error {
			return history.RetainRelease(ctx, expected)
		}
	}
	if retain || item.Status == statusEnabled {
		if err := s.verifyLocalRelease(ctx, item); err != nil {
			return domainplugin.InstalledPlugin{}, err
		}
	}
	var updated domainplugin.InstalledPlugin
	commit := func() error {
		var err error
		updated, err = s.repo.UpdateInstalled(ctx, expected, item, beforeUpdate)
		return err
	}
	driverID := storageDriverID(item.Manifest)
	err := s.executable.references.WithStorageDriverReferences(ctx, driverID, func(bindings [][]sohaapi.SystemIntegrationConfigurationField) error {
		if retain && len(bindings) != 0 {
			if item.Status != statusEnabled {
				return storageDriverInUse()
			}
			schema, err := json.Marshal(item.Manifest.ConfigSchema)
			if err != nil {
				return err
			}
			for _, fields := range bindings {
				if err := appintegration.ValidateStorageDriverBinding(schema, driverID, fields); err != nil {
					return apperrors.NewBusiness(apperrors.ErrConflict, "plugin_storage_references_incompatible",
						"The selected version is incompatible with existing storage connections.", "所选版本与现有存储连接的配置不兼容。")
				}
			}
		}
		if item.Status == statusEnabled {
			return s.executable.runtime.Switch(ctx, item, commit)
		}
		if expected.Status == statusEnabled {
			if len(bindings) != 0 {
				return storageDriverInUse()
			}
			return s.executable.runtime.Disable(ctx, item.ID, commit)
		}
		return commit()
	})
	return updated, err
}

func storageDriverInUse() error {
	return apperrors.NewBusiness(apperrors.ErrConflict, "plugin_storage_driver_in_use",
		"Migrate or remove the storage connections that use this driver first.", "请先迁移或移除引用此驱动的存储连接。")
}

func (s *Service) removeExecutable(ctx context.Context, item domainplugin.InstalledPlugin, commit func() error) error {
	if s.executable == nil {
		return apperrors.ErrUnsupportedOperation
	}
	return s.executable.references.WithStorageDriverReferences(ctx, storageDriverID(item.Manifest), func(bindings [][]sohaapi.SystemIntegrationConfigurationField) error {
		if len(bindings) != 0 {
			return storageDriverInUse()
		}
		return s.executable.runtime.Disable(ctx, item.ID, commit)
	})
}

func (s *Service) cleanRemovedExecutable(ctx context.Context, pluginID string) error {
	trailing, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	if err := s.executable.runtime.WaitRetired(trailing, pluginID); err != nil {
		return err
	}
	return s.executable.assets.RemovePlugin(trailing, pluginID)
}

func (s *Service) RollbackRepository(ctx context.Context, actor domainidentity.Principal, pluginID string, req domainplugin.PluginRepositoryRollbackRequest) (domainplugin.InstalledPlugin, error) {
	if err := s.authorizeRepository(ctx, actor, appaccess.PermPluginUpgrade); err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	if req.ExpectedVersion == "" || req.ExpectedUpdatedAt.IsZero() || req.Version == "" || req.ExpectedVersion == req.Version {
		return domainplugin.InstalledPlugin{}, apperrors.ErrInvalidArgument
	}
	defer s.withExecutableMutation()()
	item, err := s.repo.GetInstalled(ctx, pluginID)
	if err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	history, ok := s.repo.(executableReleaseHistory)
	if !ok || !executableManifest(item.Manifest) {
		return domainplugin.InstalledPlugin{}, apperrors.ErrUnsupportedOperation
	}
	previous, err := history.PreviousRelease(ctx, pluginID)
	if err != nil && !errors.Is(err, apperrors.ErrNotFound) {
		return domainplugin.InstalledPlugin{}, err
	}
	if !samePluginSource(item.SourceInfo, previous.SourceInfo) {
		return domainplugin.InstalledPlugin{}, repositoryPreviewInvalid()
	}
	if item.Version == req.Version && previous.Version == req.ExpectedVersion && previous.UpdatedAt.Equal(req.ExpectedUpdatedAt) {
		return s.withExecutableDetails(ctx, item)
	}
	if err := validateRollbackTarget(item, previous, req); err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	// Roll back the release snapshot while preserving installation identity and user configuration.
	previous.InstalledBy, previous.InstalledAt = item.InstalledBy, item.InstalledAt
	previous.EnabledAt, previous.DisabledAt = item.EnabledAt, item.DisabledAt
	previous.Status = item.Status
	previous.ConfiguredSecretRefs = item.ConfiguredSecretRefs
	previous.Metadata = maps.Clone(item.Metadata)
	if previous.Metadata == nil {
		previous.Metadata = map[string]any{}
	}
	previous.Metadata["manifestChecksum"] = previous.SourceInfo.RawManifestChecksum
	previous.Metadata["configured"] = manifestConfigReady(previous.Manifest, previous.ConfiguredSecretRefs, previous.Metadata)
	if item.Status == statusEnabled && previous.Metadata["configured"] == false {
		return domainplugin.InstalledPlugin{}, apperrors.ErrConflict
	}
	previous.UpdatedAt = time.Now().UTC()
	updated, err := s.commitExecutableChange(ctx, item, previous, true)
	if err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	s.reconcileCurrent(ctx, pluginID)
	s.recordAudit(ctx, actor, "rollback", updated, "restored retained executable release")
	return s.withExecutableDetails(ctx, updated)
}

func validateRollbackTarget(item, previous domainplugin.InstalledPlugin, req domainplugin.PluginRepositoryRollbackRequest) error {
	if item.Version != req.ExpectedVersion || !item.UpdatedAt.Equal(req.ExpectedUpdatedAt) || previous.Version != req.Version ||
		(!item.SourceInfo.Official && item.SourceInfo.PublisherKeyFingerprint != previous.SourceInfo.PublisherKeyFingerprint) {
		return repositoryPreviewInvalid()
	}
	if permissionsIncreased(item.RequestedPermissions, previous.RequestedPermissions) && !req.ConfirmPermissionIncrease {
		return apperrors.NewBusiness(apperrors.ErrConflict, "plugin_permission_increase",
			"Review and confirm the additional plugin permissions.", "请审阅并确认新增插件权限。")
	}
	return nil
}

func (s *Service) withExecutableDetails(ctx context.Context, item domainplugin.InstalledPlugin) (domainplugin.InstalledPlugin, error) {
	if !executableManifest(item.Manifest) {
		return item, nil
	}
	ready := s.executable != nil && item.Status == statusEnabled && s.executable.runtime.Ready(item.ID, item.Version)
	item.RuntimeReady = &ready
	if ready {
		item.ActiveVersion = item.Version
	}
	if driverID := storageDriverID(item.Manifest); driverID != "" && s.executable != nil {
		count, err := s.executable.references.StorageDriverReferenceCount(ctx, driverID)
		if err != nil {
			return item, err
		}
		item.ReferenceCount = &count
	}
	if history, ok := s.repo.(executableReleaseHistory); ok {
		previous, err := history.PreviousRelease(ctx, item.ID)
		if !errors.Is(err, apperrors.ErrNotFound) {
			if err != nil {
				return item, err
			}
			item.RollbackVersion = previous.Version
			item.RollbackRequiresPermissionConfirmation = permissionsIncreased(item.RequestedPermissions, previous.RequestedPermissions)
		}
	}
	return item, nil
}

// RestoreExecutablePlugins re-selects the installed active versions on startup using local packages.
func (s *Service) RestoreExecutablePlugins(ctx context.Context) error {
	if s.executable == nil {
		return nil
	}
	items, err := s.repo.ListInstalled(ctx)
	if err != nil {
		return err
	}
	var failures error
	for _, item := range items {
		if !executableManifest(item.Manifest) || item.Status != statusEnabled {
			continue
		}
		err := s.verifyLocalRelease(ctx, item)
		if err == nil {
			startCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			err = s.executable.runtime.Switch(startCtx, item, func() error { return nil })
			cancel()
		}
		if err != nil {
			failures = errors.Join(failures, fmt.Errorf("restore plugin %q: %w", item.ID, err))
		}
		s.reconcileItem(item)
	}
	return failures
}
