package networkaccess

import (
	"context"
	"errors"
	"testing"
	"time"

	api "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	publication "github.com/opensoha/soha/internal/application/proxypublication"
	identity "github.com/opensoha/soha/internal/domain/identity"
	access "github.com/opensoha/soha/internal/domain/networkaccess"
	domain "github.com/opensoha/soha/internal/domain/proxypublication"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/keyring"
)

type retentionPorts struct {
	publication.BundleStore
	publication.SourceStore
	publication.RuleStore
	publication.PoolReader
	publication.DesiredWriter
	publication.EngineCompiler
	publication.RuntimeAuthenticator
	publication.EndpointAuthorizer
	publication.CurrentAuthorizer
	publication.DestinationAuthorizer
	record      domain.BundleRecord
	views       int
	bundleReads int
}

func (p *retentionPorts) FindBundle(context.Context, string) (domain.BundleRecord, error) {
	p.bundleReads++
	return p.record, nil
}
func (p *retentionPorts) GetBundle(context.Context, string, string) (domain.BundleRecord, error) {
	p.bundleReads++
	return p.record, nil
}
func (p *retentionPorts) AllowedSourceNodes(_ context.Context, _ string, _ string, _ int64, ids []string) ([]string, error) {
	return ids, nil
}
func (p *retentionPorts) ReadRules(context.Context, string, string) (api.NetworkProxyRules, error) {
	return api.NetworkProxyRules{OrgRevision: 1}, nil
}
func (p *retentionPorts) Authorize(context.Context, identity.Principal, string, string) error {
	p.views++
	return apperrors.ErrAccessDenied
}
func (p *retentionPorts) RecordPublication(context.Context, identity.Principal, string, string, string, int64) error {
	return nil
}
func retentionService(t *testing.T) (*Service, *bindingStore, *retentionPorts, identity.Principal, access.MihomoProfileInput) {
	t.Helper()
	key, _ := keyring.NewKey("test", "test-key", time.Now(), nil)
	keys, _ := keyring.New(key, nil)
	input := access.MihomoProfileInput{DeviceID: "device-1", Name: "Renamed", Mode: access.MihomoModeManagedFollow, Status: access.StatusActive, DeliveryMode: "direct_subscription", BundleID: "bundle-1", OrgRevision: 1, SourceRevision: 1, MixedPort: 7890, ControllerPort: 9090, DNSMode: access.MihomoDNSDisabled, SelectorGroup: "Employees", FailClosed: true}
	store := &bindingStore{device: access.Device{ID: "device-1", OwnerUserID: "recipient"}}
	store.item = mihomoProfileFromInput("profile-1", input, "", "", time.Now())
	store.item.Name = "Old"
	store.item.Revision = 7
	ports := &retentionPorts{record: domain.BundleRecord{InstanceID: "instance", SourceNodeIDs: []string{"node-1"}, Nodes: []string{"node-1"}, Scope: api.NetworkProxyScope{DeviceIDs: []string{"device-1"}, UserIDs: []string{"recipient"}, NodeIDs: []string{"node-1"}}, Public: api.NetworkProxyBundle{BundleID: "bundle-1", State: "activated", Revision: 1, ActiveRevision: 1, DeliveryMode: "direct_subscription", OrgRevision: 1, SourceRevision: 1}}}
	bundles, err := publication.NewBundles(ports, ports, ports, ports, ports, ports, ports, ports, ports, ports, ports, ports, keys)
	if err != nil {
		t.Fatal(err)
	}
	service, err := New(store, appaccess.NewPermissionResolver(enrollmentRoleReader{"editor": {appaccess.PermNetworkAccessMihomoProfilesUpdate}}), allowTestNetworkScope(), &captureAudit{}, &enrollmentOperationCapture{}, keys)
	if err != nil {
		t.Fatal(err)
	}
	service.SetMihomoBundleValidator(bundles)
	return service, store, ports, identity.Principal{UserID: "editor", Roles: []string{"editor"}}, input
}
func TestMihomoUpdateOnlyRetainsTrustedPublishedBinding(t *testing.T) {
	service, store, ports, p, input := retentionService(t)
	out, err := service.UpdateMihomoProfile(context.Background(), p, "profile-1", input)
	if err != nil || out.Name != "Renamed" || out.Revision != 8 || ports.views != 0 || store.item.BundleID != "bundle-1" {
		t.Fatal("retained edit failed", out, err)
	}
}
func TestMihomoUpdateChecksPermissionBeforeValidator(t *testing.T) {
	service, store, ports, _, input := retentionService(t)
	if _, err := service.UpdateMihomoProfile(context.Background(), identity.Principal{UserID: "none"}, "profile-1", input); !errors.Is(err, apperrors.ErrAccessDenied) || store.reads != 0 || ports.views != 0 || store.item.Revision != 7 {
		t.Fatal("denied editor reached validator/write", err)
	}
}
func TestMihomoRetainedEditRejectsRevocationAndBindingSwap(t *testing.T) {
	for _, change := range []func(*retentionPorts, *access.MihomoProfileInput){func(p *retentionPorts, _ *access.MihomoProfileInput) { p.record.Public.State = "revoked" }, func(_ *retentionPorts, in *access.MihomoProfileInput) { in.BundleID = "other" }} {
		service, store, ports, p, input := retentionService(t)
		change(ports, &input)
		if _, err := service.UpdateMihomoProfile(context.Background(), p, "profile-1", input); !errors.Is(err, apperrors.ErrAccessDenied) || store.item.Revision != 7 {
			t.Fatal("revoked/swapped binding written", err)
		}
	}
}
func TestMihomoClientCannotSupplyPreviousProfile(t *testing.T) {
	service, store, _, p, input := retentionService(t)
	store.item.ID = "different-record"
	if _, err := service.UpdateMihomoProfile(context.Background(), p, "profile-1", input); !errors.Is(err, apperrors.ErrConflict) || store.item.Revision != 7 {
		t.Fatal("cross-record previous accepted", err)
	}
}
func TestMihomoRetainedEditCASDoesNotRestoreConcurrentBinding(t *testing.T) {
	service, store, _, p, input := retentionService(t)
	validator := &bundleValidatorFake{beforeReturn: func() { store.item.Revision++; store.item.BundleID = "concurrent-new-bundle" }}
	service.SetMihomoBundleValidator(validator)
	if _, err := service.UpdateMihomoProfile(context.Background(), p, "profile-1", input); !errors.Is(err, apperrors.ErrConflict) || store.item.BundleID != "concurrent-new-bundle" || validator.previous == nil || validator.previous.ID != "profile-1" || validator.previous.Revision != 7 {
		t.Fatal("concurrent binding restored from stale retained edit", err)
	}
}

func TestMihomoUpdateCanClearRevokedBindingWithoutProxyRead(t *testing.T) {
	service, store, ports, p, input := retentionService(t)
	ports.record.Public.State = "revoked"
	input.BundleID = ""
	input.OrgRevision = 0
	input.SourceRevision = 0
	input.Mode = access.MihomoModeAppSubscription
	input.SourceType = ""
	out, err := service.UpdateMihomoProfile(context.Background(), p, "profile-1", input)
	if err != nil || out.BundleID != "" || out.Revision != 8 || ports.views != 0 || ports.bundleReads != 0 || store.reads != 0 || store.item.SubscriptionURLCiphertext != "" || store.item.ManualNodeCiphertext != "" {
		t.Fatal("unbind required proxy read or copied bundle material", err)
	}
}
