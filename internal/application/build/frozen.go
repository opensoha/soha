package build

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domainbuild "github.com/opensoha/soha/internal/domain/build"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type RepositoryRefResolver interface {
	ResolveRepositoryCommit(context.Context, string, string, string) (string, error)
}

func (s *Service) SetRepositoryRefResolver(resolver RepositoryRefResolver) { s.refs = resolver }

type repositoryRefCacheKey struct{}

// WithRepositoryRefCache pins each repository/ref once for a batch's targets.
// ponytail: freezing is serial; add synchronization if targets are frozen concurrently.
func WithRepositoryRefCache(ctx context.Context) context.Context {
	return context.WithValue(ctx, repositoryRefCacheKey{}, map[domainbuild.RepositoryRef]string{})
}

func (s *Service) resolveFrozenCommit(ctx context.Context, ref domainbuild.RepositoryRef) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	cache, _ := ctx.Value(repositoryRefCacheKey{}).(map[domainbuild.RepositoryRef]string)
	if commit := cache[ref]; commit != "" {
		return commit, nil
	}
	commit, err := s.refs.ResolveRepositoryCommit(ctx, ref.RepositoryID, ref.RefType, ref.RefName)
	if err != nil {
		return "", err
	}
	if !fullCommit(commit) {
		return "", fmt.Errorf("%w: repository resolver did not return a full commit", apperrors.ErrInvalidArgument)
	}
	commit = strings.ToLower(commit)
	if cache != nil {
		cache[ref] = commit
	}
	return commit, nil
}

// PrepareDeliveryBuild performs authorization and read-only resolution before a
// batch exists. Ref selection is validated before replacing it with server SHAs.
func (s *Service) PrepareDeliveryBuild(ctx context.Context, principal domainidentity.Principal, input domainbuild.TriggerInput) (domainbuild.Prepared, error) {
	if input.RefName == "" {
		var err error
		input.RefName, err = s.deliveryDefaultRef(ctx, principal, input)
		if err != nil {
			return domainbuild.Prepared{}, err
		}
	}
	_, prepared, err := s.prepareTrigger(ctx, principal, input)
	if err != nil {
		return prepared, err
	}
	return s.freezePrepared(ctx, prepared)
}

func (s *Service) freezePrepared(ctx context.Context, prepared domainbuild.Prepared) (domainbuild.Prepared, error) {
	if prepared.ProviderKind == "external_pipeline_adapter" || prepared.SourceType == string(domainapp.BuildSourceTypeExternalPipeline) && s.pipelines == nil {
		return prepared, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "delivery_external_pipeline_unavailable", "External pipeline run tracking and confirmed cancellation are not configured for delivery batches. Choose a supported build source or deploy a verified artifact.", "外部流水线尚未接通运行跟踪和取消确认，不能用于交付批次。请选择受支持的构建方式或部署已验证产物。")
	}
	if prepared.Input.BuildSourceID == "" || s.refs == nil {
		return prepared, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "delivery_build_not_configured", "Configure the service build source and repository resolver before building.", "请先为服务关联构建来源，并配置源码解析连接。")
	}
	if err := resolveFrozenBuildArgs(&prepared); err != nil {
		return prepared, err
	}
	workspace := metadataMap(prepared.Metadata, "workspace")
	checkouts, _ := workspace["checkouts"].([]map[string]any)
	if len(checkouts) == 0 {
		return prepared, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "delivery_source_missing", "Associate a source repository with the service build configuration before building.", "请先在服务构建配置中关联源码仓库。")
	}
	prepared.Input.RepositoryRefs = nil
	for _, checkout := range checkouts {
		id := metadataString(checkout, "repositoryId")
		commit, err := s.resolveFrozenCommit(ctx, domainbuild.RepositoryRef{RepositoryID: id, RefType: metadataString(checkout, "refType"), RefName: metadataString(checkout, "refName")})
		if err != nil {
			return prepared, fmt.Errorf("resolve build repository %s: %w", id, err)
		}
		checkout["refType"], checkout["refName"] = "commit", commit
		prepared.Input.RepositoryRefs = append(prepared.Input.RepositoryRefs, domainbuild.RepositoryRef{RepositoryID: id, RefType: "commit", RefName: commit})
	}
	primary := prepared.Input.RepositoryRefs[0]
	prepared.Input.RepositoryID, prepared.Input.ResolvedCommit = primary.RepositoryID, primary.RefName
	prepared.Input.RefType, prepared.Input.RefName = "commit", primary.RefName
	prepared.Metadata["repositoryId"], prepared.Metadata["resolvedCommit"] = primary.RepositoryID, primary.RefName
	prepared.Metadata["refType"], prepared.Metadata["refName"] = "commit", primary.RefName
	prepared.Metadata["repositoryRefs"] = prepared.Input.RepositoryRefs
	if err := s.freezeExternalPipeline(ctx, &prepared); err != nil {
		return prepared, err
	}
	var err error
	prepared.Fingerprint, err = buildFingerprint(prepared)
	if err != nil {
		return prepared, err
	}
	prepared.Metadata["buildFingerprint"] = prepared.Fingerprint
	// Detach all nested configuration from the editable application and template.
	return copyPrepared(prepared)
}

func (s *Service) deliveryDefaultRef(ctx context.Context, principal domainidentity.Principal, input domainbuild.TriggerInput) (string, error) {
	if err := s.authorize(ctx, principal, domainaccess.ActionTrigger, input.ApplicationID); err != nil {
		return "", err
	}
	app, err := s.apps.Get(ctx, input.ApplicationID)
	if err != nil {
		return "", err
	}
	bindings := repositoryBindingsForBuild(app, resolveBuildSource(app, input.BuildSourceID), input)
	if len(bindings) == 0 {
		return "", apperrors.NewBusiness(apperrors.ErrInvalidArgument, "delivery_source_missing", "Associate a source repository with the service build configuration before building.", "请先在服务构建配置中关联源码仓库。")
	}
	repository, err := s.apps.GetRepository(ctx, bindings[0].RepositoryID)
	if err != nil {
		return "", err
	}
	return firstNonEmptyString(bindings[0].DefaultBranch, repository.DefaultBranch, app.DefaultBranch, "main"), nil
}

func resolveFrozenBuildArgs(prepared *domainbuild.Prepared) error {
	config := metadataMap(prepared.Metadata, "buildSourceConfig")
	defaults := metadataMap(config, "buildArgs")
	args := make(map[string]any, len(defaults))
	for key, value := range defaults {
		args[key] = value
	}
	for key, value := range prepared.Input.BuildArgs {
		if _, declared := defaults[key]; !declared {
			return fmt.Errorf("%w: build argument %q is not declared in the build source", apperrors.ErrInvalidArgument, key)
		}
		args[key] = value
	}
	for key, value := range args {
		if key == "" || len(key) > 128 || len(fmt.Sprint(value)) > 16384 {
			return fmt.Errorf("%w: invalid build argument", apperrors.ErrInvalidArgument)
		}
		switch value.(type) {
		case string, bool, float64, int, int64:
		default:
			return fmt.Errorf("%w: build arguments must be scalar values", apperrors.ErrInvalidArgument)
		}
	}
	prepared.Input.BuildArgs, prepared.Metadata["buildArgs"] = args, args
	source := &domainapp.BuildSource{Type: domainapp.BuildSourceType(metadataString(prepared.Metadata, "buildSourceType")), Config: config}
	commands, err := buildExecutionCommands(source, prepared.Metadata, prepared.ImageRef)
	if err != nil {
		return err
	}
	prepared.Metadata["commands"] = commands
	prepared.Input.Variables = metadataMap(prepared.Metadata, "variables")
	return nil
}

func fullCommit(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func buildFingerprint(prepared domainbuild.Prepared) (string, error) {
	inputs := map[string]any{"applicationId": prepared.Input.ApplicationID, "buildSourceId": prepared.Input.BuildSourceID,
		"image": prepared.ImageRef, "provider": prepared.ProviderKind, "sourceType": prepared.SourceType}
	for _, key := range []string{"workspace", "runtime", "commands", "buildArgs", "variables", "buildSourceConfig", "buildTemplateVersion", "buildTemplateContentDigest"} {
		inputs[key] = prepared.Metadata[key]
	}
	// Keep legacy fingerprints stable when the optional CNB/secret inputs are absent.
	for _, key := range []string{"buildpacks", "secretRefs", "externalPipeline"} {
		if value, exists := prepared.Metadata[key]; exists {
			inputs[key] = value
		}
	}
	encoded, err := json.Marshal(inputs)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func copyPrepared(prepared domainbuild.Prepared) (domainbuild.Prepared, error) {
	encoded, err := json.Marshal(prepared)
	if err != nil {
		return domainbuild.Prepared{}, err
	}
	var copy domainbuild.Prepared
	err = json.Unmarshal(encoded, &copy)
	return copy, err
}

// TriggerFrozen is called only by the leased Run worker with a stored snapshot.
// Commands, runtime and checkout inputs are never regenerated from current config.
func (s *Service) TriggerFrozen(ctx context.Context, principal domainidentity.Principal, prepared domainbuild.Prepared) (domainbuild.Record, error) {
	node, ok := domainworkflow.NodeExecutionFrom(ctx)
	if !ok || node.Stage != "build" || prepared.Fingerprint == "" {
		return domainbuild.Record{}, fmt.Errorf("%w: frozen builds require a delivery build node", apperrors.ErrConflict)
	}
	fingerprint, err := buildFingerprint(prepared)
	if err != nil || fingerprint != prepared.Fingerprint {
		return domainbuild.Record{}, fmt.Errorf("%w: frozen build inputs changed", apperrors.ErrConflict)
	}
	if err := s.authorize(ctx, principal, domainaccess.ActionTrigger, prepared.Input.ApplicationID); err != nil {
		return domainbuild.Record{}, err
	}
	app, err := s.apps.Get(ctx, prepared.Input.ApplicationID)
	if err != nil {
		return domainbuild.Record{}, err
	}
	if err := s.validateServiceBuild(ctx, &prepared.Input, resolveBuildSource(app, prepared.Input.BuildSourceID), prepared.ImageRef); err != nil {
		return domainbuild.Record{}, err
	}
	if err := s.authorizeFrozenExternalPipeline(ctx, principal, prepared); err != nil {
		return domainbuild.Record{}, err
	}
	prepared, err = copyPrepared(prepared)
	if err != nil {
		return domainbuild.Record{}, err
	}
	return s.triggerPrepared(ctx, principal, app, prepared)
}
