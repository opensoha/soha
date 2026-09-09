package networkaccess

import (
	"context"
	"errors"
	"testing"
	"time"

	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainnetworkaccess "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type deviceRegistrationStore struct {
	Store
	item domainnetworkaccess.Device
}

func (s *deviceRegistrationStore) RegisterDevice(_ context.Context, item domainnetworkaccess.Device) (domainnetworkaccess.Device, error) {
	s.item = item
	return item, nil
}

func TestRegisterDeviceBindsAuthenticatedOwnerAndStartsPending(t *testing.T) {
	store := &deviceRegistrationStore{}
	service := &Service{store: store, audit: &captureAudit{}, operations: &enrollmentOperationCapture{}}

	device, err := service.RegisterDevice(context.Background(), domainidentity.Principal{UserID: "85b2d62f-e9fe-4252-b876-74888e5fa549"}, "endpoint-mac-1", domainnetworkaccess.DeviceRegistrationInput{
		Name: "MacBook Pro", Hostname: "macbook.local", Platform: "darwin", DeviceType: domainnetworkaccess.DeviceTypeLaptop,
		ReportedFacts: &domainnetworkaccess.DeviceReportedFacts{
			OSName: "macOS", OSVersion: "15.6.1", Architecture: "arm64", AgentVersion: "0.2.0", CollectedAt: time.Now().UTC(),
			NetworkInterfaces: []domainnetworkaccess.DeviceNetworkInterface{{Name: "en0", Kind: domainnetworkaccess.NetworkInterfaceKindPhysical, Status: domainnetworkaccess.NetworkInterfaceStatusUp, IPv4Addresses: []string{"192.168.1.10"}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if device.OwnerUserID == "" || device.Status != domainnetworkaccess.DeviceStatusPending || device.PostureStatus != domainnetworkaccess.PostureUnknown || device.DeviceType != domainnetworkaccess.DeviceTypeLaptop || device.OwnershipType != domainnetworkaccess.DeviceOwnershipUnassigned || device.ReportedFacts == nil || device.LastSeenAt == nil {
		t.Fatalf("registered device = %#v", device)
	}
}

func TestRegisterDeviceRejectsMissingPrincipal(t *testing.T) {
	service := &Service{store: &deviceRegistrationStore{}, audit: &captureAudit{}, operations: &enrollmentOperationCapture{}}
	_, err := service.RegisterDevice(context.Background(), domainidentity.Principal{}, "endpoint-mac-1", domainnetworkaccess.DeviceRegistrationInput{Name: "Mac", Platform: "darwin"})
	if !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("register error = %v, want unauthorized", err)
	}
}
