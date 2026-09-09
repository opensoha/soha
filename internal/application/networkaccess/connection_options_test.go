package networkaccess

import (
	"context"
	"errors"
	"testing"

	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainnetworkaccess "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type connectionOptionsStore struct {
	Store
	device   domainnetworkaccess.Device
	subject  domainnetworkaccess.Subject
	snapshot domainnetworkaccess.PolicySnapshot
	bindings []domainnetworkaccess.NASBinding
	sites    []domainnetworkaccess.Site
}

func (s *connectionOptionsStore) GetDevice(context.Context, string) (domainnetworkaccess.Device, error) {
	return s.device, nil
}

func (s *connectionOptionsStore) GetSubject(context.Context, string) (domainnetworkaccess.Subject, error) {
	return s.subject, nil
}

func (s *connectionOptionsStore) GetPolicySnapshot(context.Context) (domainnetworkaccess.PolicySnapshot, error) {
	return s.snapshot, nil
}

func (s *connectionOptionsStore) ListNASBindings(context.Context, domainnetworkaccess.NASBindingFilter) ([]domainnetworkaccess.NASBinding, error) {
	return s.bindings, nil
}

func (s *connectionOptionsStore) ListSites(context.Context, domainnetworkaccess.SiteFilter) ([]domainnetworkaccess.Site, error) {
	return s.sites, nil
}

func TestListConnectionOptionsReturnsOnlyAuthorized8021XChoices(t *testing.T) {
	store := &connectionOptionsStore{
		device:   domainnetworkaccess.Device{ID: "device-1", OwnerUserID: "user-1", SiteID: "site-1", Status: domainnetworkaccess.DeviceStatusActive, PostureStatus: domainnetworkaccess.PostureCompliant},
		subject:  domainnetworkaccess.Subject{UserID: "user-1", Status: domainnetworkaccess.StatusActive},
		snapshot: domainnetworkaccess.PolicySnapshot{PolicyVersion: 7, Policies: []domainnetworkaccess.Policy{{ID: "allow-office", Enabled: true, Effect: domainnetworkaccess.PolicyEffectAllow, Subjects: domainnetworkaccess.PolicySubjects{Users: []string{"user-1"}}, SiteIDs: []string{"site-1"}, Modes: []string{domainnetworkaccess.ModeInternalDirect}, AccessProfile: domainnetworkaccess.ProfileFull}}},
		bindings: []domainnetworkaccess.NASBinding{
			{ID: "wifi-1", SiteID: "site-1", Name: "Office Wi-Fi", AccessMedium: domainnetworkaccess.AccessMediumWiFi, SSID: "Soha-Staff", Status: domainnetworkaccess.StatusActive},
			{ID: "wifi-duplicate", SiteID: "site-1", Name: "Second AP", AccessMedium: domainnetworkaccess.AccessMediumWiFi, SSID: "Soha-Staff", Status: domainnetworkaccess.StatusActive},
			{ID: "wired-1", SiteID: "site-1", Name: "Office switch", AccessMedium: domainnetworkaccess.AccessMediumWired, Status: domainnetworkaccess.StatusActive},
			{ID: "disabled", SiteID: "site-1", Name: "Disabled", AccessMedium: domainnetworkaccess.AccessMediumWiFi, SSID: "Hidden", Status: domainnetworkaccess.StatusDisabled},
		},
		sites: []domainnetworkaccess.Site{{ID: "site-1", Name: "Shanghai HQ", Status: domainnetworkaccess.StatusActive}},
	}
	service := &Service{store: store}
	items, err := service.ListConnectionOptions(context.Background(), domainidentity.Principal{UserID: "user-1"}, "device-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].AccessMedium != domainnetworkaccess.AccessMediumWiFi || items[0].SSID != "Soha-Staff" || items[1].AccessMedium != domainnetworkaccess.AccessMediumWired || items[0].Authentication != domainnetworkaccess.ConnectionAuthenticationRadius8021X || items[0].AccessProfile != domainnetworkaccess.ProfileFull || items[0].PolicyVersion != 7 {
		t.Fatalf("connection options = %#v", items)
	}
}

func TestListConnectionOptionsRejectsAnotherUsersDevice(t *testing.T) {
	service := &Service{store: &connectionOptionsStore{device: domainnetworkaccess.Device{ID: "device-1", OwnerUserID: "user-2"}}}
	_, err := service.ListConnectionOptions(context.Background(), domainidentity.Principal{UserID: "user-1"}, "device-1")
	if !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("error = %v, want access denied", err)
	}
}
