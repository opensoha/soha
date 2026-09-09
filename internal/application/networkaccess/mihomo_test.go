package networkaccess

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainnetworkaccess "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/platform/keyring"
	"github.com/opensoha/soha/internal/platform/secretcrypto"
)

type mihomoStoreStub struct {
	Store
	item domainnetworkaccess.MihomoProfile
}

func (s *mihomoStoreStub) GetMihomoProfile(_ context.Context, id string) (domainnetworkaccess.MihomoProfile, error) {
	return s.item, nil
}

func (s *mihomoStoreStub) CreateMihomoProfile(_ context.Context, item domainnetworkaccess.MihomoProfile) (domainnetworkaccess.MihomoProfile, error) {
	s.item = item
	return item, nil
}

func (s *mihomoStoreStub) UpdateMihomoProfile(_ context.Context, _ string, item domainnetworkaccess.MihomoProfile, updatedAt time.Time) (domainnetworkaccess.MihomoProfile, error) {
	item.Revision = s.item.Revision + 1
	item.UpdatedAt = updatedAt
	s.item = item
	return item, nil
}

func TestMihomoProfileEncryptsAndPreservesWriteOnlySources(t *testing.T) {
	key, err := keyring.NewKey("test-v1", "stable-test-credential-key-32-bytes", time.Now().UTC(), nil)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := keyring.New(key, nil)
	if err != nil {
		t.Fatal(err)
	}
	store := &mihomoStoreStub{}
	service, err := New(
		store,
		appaccess.NewPermissionResolver(enrollmentRoleReader{"network-admin": {
			appaccess.PermNetworkAccessMihomoProfilesCreate,
			appaccess.PermNetworkAccessMihomoProfilesUpdate,
		}}),
		&captureAudit{}, &enrollmentOperationCapture{}, keys,
	)
	if err != nil {
		t.Fatal(err)
	}
	principal := domainidentity.Principal{UserID: "operator-1", Roles: []string{"network-admin"}}
	url := "https://subscription.example/config?token=sensitive"
	input := domainnetworkaccess.MihomoProfileInput{
		DeviceID: "device-1", Name: "Managed proxy", Mode: domainnetworkaccess.MihomoModeManagedFollow,
		SourceType: domainnetworkaccess.MihomoSourceManagedSubscription,
		Status:     domainnetworkaccess.StatusActive, SubscriptionURL: &url, MixedPort: 7890, ControllerPort: 9090,
		DNSMode: domainnetworkaccess.MihomoDNSDisabled, SelectorGroup: "SOHA", SelectedProxy: "Hong Kong",
		BypassCIDRs: []string{"10.0.0.0/8"}, BypassHosts: []string{"control.example.com"}, FailClosed: true,
	}
	created, err := service.CreateMihomoProfile(context.Background(), principal, input)
	if err != nil {
		t.Fatal(err)
	}
	if store.item.SubscriptionURLCiphertext == url || !strings.HasPrefix(store.item.SubscriptionURLCiphertext, secretcrypto.PrefixV2) {
		t.Fatalf("subscription stored without envelope encryption: %q", store.item.SubscriptionURLCiphertext)
	}
	plaintext, err := secretcrypto.DecryptStringWithKeyring(keys, store.item.SubscriptionURLCiphertext)
	if err != nil || plaintext != url {
		t.Fatalf("decrypt subscription = %q, %v", plaintext, err)
	}
	encoded, err := json.Marshal(created)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "sensitive") || strings.Contains(string(encoded), "Ciphertext") || strings.Contains(string(encoded), secretcrypto.PrefixV2) {
		t.Fatalf("public profile leaked subscription material: %s", encoded)
	}

	input.SubscriptionURL = nil
	updated, err := service.UpdateMihomoProfile(context.Background(), principal, created.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Revision != 2 || store.item.SubscriptionURLCiphertext == "" {
		t.Fatalf("update did not preserve encrypted subscription: %#v", updated)
	}

	checkManualMihomoSource(t, service, store, keys, principal, input)
}

func checkManualMihomoSource(t *testing.T, service *Service, store *mihomoStoreStub, keys keyring.Ring, principal domainidentity.Principal, input domainnetworkaccess.MihomoProfileInput) {
	t.Helper()
	input.SourceType = domainnetworkaccess.MihomoSourceManualNode
	input.SubscriptionURL, input.SelectedProxy = nil, ""
	input.ManualNode = &domainnetworkaccess.MihomoManualNode{Protocol: "https", Server: "proxy.example.com", Port: 8443, Username: "alice", Password: "manual-secret"}
	created, err := service.CreateMihomoProfile(context.Background(), principal, input)
	if err != nil {
		t.Fatal(err)
	}
	plaintext, err := secretcrypto.DecryptStringWithKeyring(keys, store.item.ManualNodeCiphertext)
	if err != nil || !strings.Contains(plaintext, "manual-secret") || store.item.SubscriptionURLCiphertext != "" {
		t.Fatalf("manual node was not stored as the sole encrypted source: %q, %v", plaintext, err)
	}
	encoded, _ := json.Marshal(created)
	if strings.Contains(string(encoded), "manual-secret") || strings.Contains(string(encoded), "proxy.example.com") {
		t.Fatalf("public profile leaked manual node material: %s", encoded)
	}
	input.ManualNode = nil
	updated, err := service.UpdateMihomoProfile(context.Background(), principal, created.ID, input)
	if err != nil || updated.Revision != 2 || store.item.ManualNodeCiphertext == "" {
		t.Fatalf("update did not preserve encrypted manual node: %#v, %v", updated, err)
	}
}
