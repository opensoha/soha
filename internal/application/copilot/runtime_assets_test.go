package copilot

import (
	"context"
	"errors"
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	identity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type runtimeAssetTestCatalog struct {
	DSHRuntime
	items          []domain.RuntimeAssetDescriptor
	probeErr       error
	probeArguments [3]string
}

func (c *runtimeAssetTestCatalog) ListRuntimeAssets(context.Context) ([]domain.RuntimeAssetDescriptor, error) {
	return c.items, nil
}

func (c *runtimeAssetTestCatalog) ProbeRuntimeMCP(_ context.Context, id, version, scope string) (domain.RuntimeMCPProbe, error) {
	c.probeArguments = [3]string{id, version, scope}
	return domain.RuntimeMCPProbe{Status: domain.RuntimeMCPProbeReady, Capabilities: []string{"tools"}}, c.probeErr
}

type runtimeAssetTestStore map[domain.RuntimeAssetRef]bool

func (s runtimeAssetTestStore) DisabledRuntimeAssets(_ context.Context, refs []domain.RuntimeAssetRef) ([]domain.RuntimeAssetRef, error) {
	var result []domain.RuntimeAssetRef
	for _, ref := range refs {
		ref.Required = false
		if enabled, exists := s[ref]; exists && !enabled {
			result = append(result, ref)
		}
	}
	return result, nil
}

func (s runtimeAssetTestStore) SetRuntimeAssetEnabled(_ context.Context, _ string, ref domain.RuntimeAssetRef, enabled bool) error {
	s[ref] = enabled
	return nil
}

type runtimeAssetAudit struct {
	AuditReader
	entries []domainaudit.Entry
}

func (a *runtimeAssetAudit) Record(_ context.Context, entry domainaudit.Entry) error {
	a.entries = append(a.entries, entry)
	return nil
}

func TestRuntimeAssetGovernance(t *testing.T) {
	defer appaccess.SetRolePermissionMatrix(nil)
	svc, _ := newInspectionAuthzTestService(map[string][]string{
		"viewer": {appaccess.PermSettingsAIView},
		"editor": {appaccess.ManagedActionPermission(appaccess.PermSettingsAIManage, "update")},
	})
	v1 := domain.RuntimeAssetDescriptor{Kind: "mcp", ID: "example", Version: "1", Transport: "http"}
	v2 := v1
	v2.Version = "2"
	catalog := &runtimeAssetTestCatalog{items: []domain.RuntimeAssetDescriptor{v1, v2}}
	store := make(runtimeAssetTestStore)
	audit := &runtimeAssetAudit{}
	svc.dshRuntime, svc.runtimeAssets, svc.audits = catalog, store, audit
	viewer := identity.Principal{UserID: "viewer", Roles: []string{"viewer"}}
	editor := identity.Principal{UserID: "editor", Roles: []string{"editor"}}
	ctx := t.Context()
	if _, err := svc.ListRuntimeAssets(ctx, editor); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("editor without view permission listed assets: %v", err)
	}
	if _, err := svc.SetRuntimeAssetEnabled(ctx, viewer, v1.Ref(), false); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("viewer changed state: %v", err)
	}
	unknown := v1.Ref()
	unknown.Version = "missing"
	if _, err := svc.SetRuntimeAssetEnabled(ctx, editor, unknown, false); !errors.Is(err, apperrors.ErrNotFound) || len(store) != 0 {
		t.Fatalf("unregistered ref update: %v, store=%v", err, store)
	}
	items, err := svc.ListRuntimeAssets(ctx, viewer)
	if err != nil || len(items) != 2 || !items[0].Enabled || !items[1].Enabled {
		t.Fatalf("legacy defaults: %+v %v", items, err)
	}
	item, err := svc.SetRuntimeAssetEnabled(ctx, editor, v1.Ref(), false)
	if err != nil || item.Enabled || item.Version != "1" {
		t.Fatalf("disable exact version: %+v %v", item, err)
	}
	frozen := v1.Ref()
	frozen.Required = true
	if err := svc.authorizeRuntimeAssets(ctx, []domain.RuntimeAssetRef{frozen}); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("disabled version still authorized: %v", err)
	}
	if err := svc.authorizeRuntimeAssets(ctx, []domain.RuntimeAssetRef{v2.Ref()}); err != nil {
		t.Fatalf("other version blocked: %v", err)
	}
	items, err = svc.ListRuntimeAssets(ctx, viewer)
	if err != nil || len(items) != 2 || items[0].Enabled || !items[1].Enabled {
		t.Fatalf("effective states: %+v %v", items, err)
	}
	if _, err := svc.ProbeRuntimeMCP(ctx, viewer, v1.ID, v1.Version, "chat"); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("viewer probed connection: %v", err)
	}
	catalog.probeErr = errors.New("sensitive downstream error")
	probe, err := svc.ProbeRuntimeMCP(ctx, editor, v1.ID, v1.Version, "chat")
	if err != nil || probe.Status != domain.RuntimeMCPProbeCleanupUnknown || probe.Capabilities == nil || len(probe.Capabilities) != 0 ||
		catalog.probeArguments != [3]string{v1.ID, v1.Version, "chat"} {
		t.Fatalf("uncertain exact probe: %+v %v %v", probe, err, catalog.probeArguments)
	}
	if len(audit.entries) != 2 || len(audit.entries[1].Metadata) != 4 || audit.entries[1].Metadata["status"] != probe.Status {
		t.Fatalf("redacted audit entries: %+v", audit.entries)
	}
}
