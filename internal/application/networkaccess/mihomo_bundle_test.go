package networkaccess

import (
	"context"
	"errors"
	appaccess "github.com/opensoha/soha/internal/application/access"
	identity "github.com/opensoha/soha/internal/domain/identity"
	access "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/keyring"
	"testing"
	"time"
)

type bindingStore struct {
	mihomoStoreStub
	device access.Device
	reads  int
}

func (s *bindingStore) GetDevice(context.Context, string) (access.Device, error) {
	s.reads++
	return s.device, nil
}

type bundleValidatorFake struct {
	err          error
	calls        int
	device       access.Device
	previous     *access.MihomoProfile
	beforeReturn func()
}

func (v *bundleValidatorFake) ValidateMihomoBundle(_ context.Context, _ identity.Principal, _ access.MihomoProfileInput, device access.Device, previous *access.MihomoProfile) error {
	v.calls++
	v.device = device
	v.previous = previous
	if v.beforeReturn != nil {
		v.beforeReturn()
	}
	return v.err
}
func TestMihomoBundleSaveRequiresValidatorAndTrustedDevice(t *testing.T) {
	key, err := keyring.NewKey("test-v1", "stable-test-credential-key-32-bytes", time.Now(), nil)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := keyring.New(key, nil)
	if err != nil {
		t.Fatal(err)
	}
	store := &bindingStore{device: access.Device{ID: "device-1", OwnerUserID: "recipient"}}
	service, err := New(store, appaccess.NewPermissionResolver(enrollmentRoleReader{"admin": {appaccess.PermNetworkAccessMihomoProfilesCreate, appaccess.PermNetworkAccessMihomoProfilesUpdate}}), allowTestNetworkScope(), &captureAudit{}, &enrollmentOperationCapture{}, keys)
	if err != nil {
		t.Fatal(err)
	}
	p := identity.Principal{UserID: "operator", Roles: []string{"admin"}}
	in := access.MihomoProfileInput{DeviceID: "device-1", Name: "Bundle", Mode: access.MihomoModeManagedFollow, SourceType: access.MihomoSourceManagedSubscription, Status: access.StatusActive, DeliveryMode: "mapped_nodes", BundleID: "bundle-1", OrgRevision: 1, SourceRevision: 1, MixedPort: 7890, ControllerPort: 9090, DNSMode: access.MihomoDNSDisabled, SelectorGroup: "Employees", FailClosed: true}
	if _, err = service.CreateMihomoProfile(context.Background(), p, in); !errors.Is(err, apperrors.ErrServiceUnavailable) || store.item.ID != "" {
		t.Fatalf("missing validator wrote profile: %v", err)
	}
	validator := &bundleValidatorFake{err: apperrors.ErrAccessDenied}
	service.SetMihomoBundleValidator(validator)
	if _, err = service.CreateMihomoProfile(context.Background(), p, in); !errors.Is(err, apperrors.ErrAccessDenied) || store.item.ID != "" {
		t.Fatalf("rejected reference wrote profile: %v", err)
	}
	if validator.device.OwnerUserID != "recipient" {
		t.Fatal("recipient was not resolved from store")
	}
	validator.err = nil
	created, err := service.CreateMihomoProfile(context.Background(), p, in)
	if err != nil {
		t.Fatal(err)
	}
	validator.err = apperrors.ErrConflict
	in.SourceRevision++
	if _, err = service.UpdateMihomoProfile(context.Background(), p, created.ID, in); !errors.Is(err, apperrors.ErrConflict) || store.item.SourceRevision != 1 {
		t.Fatalf("stale reference updated profile: %v", err)
	}
	calls := validator.calls
	store.device.ID = "other-device"
	if _, err = service.UpdateMihomoProfile(context.Background(), p, created.ID, in); !errors.Is(err, apperrors.ErrAccessDenied) || validator.calls != calls {
		t.Fatalf("device mismatch reached validator: %v", err)
	}
}
