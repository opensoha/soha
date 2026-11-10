package copilot

import (
	"context"
	"slices"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	identity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/requestctx"
)

type DSHRuntimeAssets interface {
	ListRuntimeAssets(context.Context) ([]domain.RuntimeAssetDescriptor, error)
	ProbeRuntimeMCP(context.Context, string, string, string) (domain.RuntimeMCPProbe, error)
}

type RuntimeAssetStore interface {
	DisabledRuntimeAssets(context.Context, []domain.RuntimeAssetRef) ([]domain.RuntimeAssetRef, error)
	SetRuntimeAssetEnabled(context.Context, string, domain.RuntimeAssetRef, bool) error
}

func WithRuntimeAssets(store RuntimeAssetStore) Option {
	return func(s *Service) { s.runtimeAssets = store }
}

var (
	errRuntimeAssetNotFound = apperrors.NewBusiness(apperrors.ErrNotFound, "runtime_asset_not_found", "The registered runtime asset version was not found.", "未找到该已登记的运行资产版本。")
	errRuntimeAssetDisabled = apperrors.NewBusiness(apperrors.ErrAccessDenied, "runtime_asset_disabled", "A referenced runtime asset version is disabled.", "引用的运行资产版本已停用。")
)

func (s *Service) ListRuntimeAssets(ctx context.Context, principal identity.Principal) ([]domain.RuntimeAsset, error) {
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermSettingsAIView); err != nil {
		return nil, err
	}
	_, descriptors, err := s.runtimeAssetCatalog(ctx)
	if err != nil {
		return nil, err
	}
	refs := make([]domain.RuntimeAssetRef, 0, len(descriptors))
	for _, asset := range descriptors {
		refs = append(refs, asset.Ref())
	}
	items := make([]domain.RuntimeAsset, 0, len(descriptors))
	if len(refs) == 0 {
		return items, nil
	}
	disabled, err := s.runtimeAssets.DisabledRuntimeAssets(ctx, refs)
	if err != nil {
		return nil, err
	}
	for _, asset := range descriptors {
		items = append(items, domain.RuntimeAsset{RuntimeAssetDescriptor: asset, Enabled: !slices.Contains(disabled, asset.Ref())})
	}
	return items, nil
}

func (s *Service) SetRuntimeAssetEnabled(ctx context.Context, principal identity.Principal, ref domain.RuntimeAssetRef, enabled bool) (domain.RuntimeAsset, error) {
	if err := s.authorizeRuntimeAssetUpdate(ctx, principal, ref); err != nil {
		return domain.RuntimeAsset{}, err
	}
	_, descriptors, err := s.runtimeAssetCatalog(ctx)
	if err != nil {
		return domain.RuntimeAsset{}, err
	}
	asset, err := registeredRuntimeAsset(descriptors, ref)
	if err != nil {
		return domain.RuntimeAsset{}, err
	}
	if err := s.runtimeAssets.SetRuntimeAssetEnabled(ctx, principal.UserID, ref, enabled); err != nil {
		return domain.RuntimeAsset{}, err
	}
	s.auditRuntimeAsset(ctx, principal, "update", ref, "success", map[string]any{"enabled": enabled})
	return domain.RuntimeAsset{RuntimeAssetDescriptor: asset, Enabled: enabled}, nil
}

func (s *Service) ProbeRuntimeMCP(ctx context.Context, principal identity.Principal, id, version, scope string) (domain.RuntimeMCPProbe, error) {
	ref := domain.RuntimeAssetRef{Kind: "mcp", ID: id, Version: version}
	if err := s.authorizeRuntimeAssetUpdate(ctx, principal, ref); err != nil {
		return domain.RuntimeMCPProbe{}, err
	}
	if scope != domain.RuntimeScopeChat && scope != domain.RuntimeScopeHeya {
		return domain.RuntimeMCPProbe{}, apperrors.ErrInvalidArgument
	}
	runtime, descriptors, err := s.runtimeAssetCatalog(ctx)
	if err != nil {
		return domain.RuntimeMCPProbe{}, err
	}
	if _, err := registeredRuntimeAsset(descriptors, ref); err != nil {
		return domain.RuntimeMCPProbe{}, err
	}
	probe, err := runtime.ProbeRuntimeMCP(ctx, id, version, scope)
	if err != nil {
		// The request may have opened a connection before its response was lost.
		probe = domain.RuntimeMCPProbe{Status: domain.RuntimeMCPProbeCleanupUnknown, Capabilities: []string{}}
	}
	result := "failure"
	if probe.Status == domain.RuntimeMCPProbeReady {
		result = "success"
	}
	s.auditRuntimeAsset(ctx, principal, "probe", ref, result, map[string]any{"scope": scope, "status": probe.Status})
	return probe, nil
}

func (s *Service) runtimeAssetCatalog(ctx context.Context) (DSHRuntimeAssets, []domain.RuntimeAssetDescriptor, error) {
	runtime, ok := s.dshRuntime.(DSHRuntimeAssets)
	if !ok || isNilDependency(runtime) || isNilDependency(s.runtimeAssets) {
		return nil, nil, apperrors.ErrServiceUnavailable
	}
	items, err := runtime.ListRuntimeAssets(ctx)
	if err != nil {
		return nil, nil, apperrors.ErrServiceUnavailable
	}
	return runtime, items, nil
}

func registeredRuntimeAsset(items []domain.RuntimeAssetDescriptor, ref domain.RuntimeAssetRef) (domain.RuntimeAssetDescriptor, error) {
	for _, item := range items {
		if item.Ref() == ref {
			return item, nil
		}
	}
	return domain.RuntimeAssetDescriptor{}, errRuntimeAssetNotFound
}

func (s *Service) authorizeRuntimeAssetUpdate(ctx context.Context, principal identity.Principal, ref domain.RuntimeAssetRef) error {
	if principal.UserID == "" {
		return apperrors.ErrAccessDenied
	}
	if err := s.authorizePrincipal(ctx, principal, appaccess.ManagedActionPermission(appaccess.PermSettingsAIManage, "update")); err != nil {
		return err
	}
	if validateRuntimeAssets([]domain.RuntimeAssetRef{ref}) != nil {
		return apperrors.ErrInvalidArgument
	}
	return nil
}

func (s *Service) authorizeRuntimeAssets(ctx context.Context, assets []domain.RuntimeAssetRef) error {
	if len(assets) == 0 || isNilDependency(s.runtimeAssets) {
		return nil
	}
	disabled, err := s.runtimeAssets.DisabledRuntimeAssets(ctx, assets)
	if err != nil {
		return err
	}
	if len(disabled) > 0 {
		return errRuntimeAssetDisabled
	}
	return nil
}

func (s *Service) auditRuntimeAsset(ctx context.Context, principal identity.Principal, action string, ref domain.RuntimeAssetRef, result string, metadata map[string]any) {
	if s.audits == nil {
		return
	}
	metadata["kind"], metadata["version"] = ref.Kind, ref.Version
	request := requestctx.FromContext(ctx)
	_ = s.audits.Record(ctx, domainaudit.Entry{ActorID: principal.UserID, ActorName: principal.UserName,
		ResourceKind: "WorkbenchRuntimeAsset", ResourceName: ref.ID, Action: "runtime_asset." + action, Result: result,
		Summary: "Workbench runtime asset " + action, RequestID: request.RequestID, RequestPath: request.Path,
		RequestMethod: request.Method, SourceIP: request.SourceIP, Metadata: metadata})
}
