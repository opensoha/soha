package networkaccess_test

import (
	"errors"
	"testing"
	"time"

	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *accessRepositoryScenario) checkAccessNetworkCatalog(t *testing.T) {
	t.Helper()
	site, err := s.repository.CreateSite(s.ctx, domain.Site{
		ID: "access-network-" + s.suffix, Name: "Access network " + s.suffix, Status: domain.StatusActive,
		CreatedAt: s.now, UpdatedAt: s.now,
	})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := s.repository.CreateNASBinding(s.ctx, domain.NASBinding{
		ID: "access-wlc-" + s.suffix, NASID: "access-nas-" + s.suffix, RuntimeID: s.runtimeID,
		SiteID: site.ID, Name: "AP", AccessMedium: domain.AccessMediumWiFi, Status: domain.StatusActive,
		CreatedAt: s.now, UpdatedAt: s.now,
	})
	if err != nil {
		t.Fatal(err)
	}
	create := func(ssid string) domain.AccessNetwork {
		return domain.AccessNetwork{ID: ssid + "-" + s.suffix, Configuration: domain.AccessNetworkInput{
			SiteID: site.ID, Name: ssid, AccessMedium: domain.AccessMediumWiFi, Kind: domain.AccessNetworkStandard,
			Enabled: true, NASBindingIDs: []string{binding.ID}, Priority: 1, SSID: ssid,
		}}
	}
	first := create("staff")
	oldID := "legacy-password-" + s.suffix
	if err := s.store.Exec(s.ctx, `INSERT INTO public.network_runtime_sessions
		(id,runtime_id,subject_id,device_id,site_id,mode,access_profile,status,policy_version,configuration_version,posture_version,valid_until,nas_id,authentication_method,created_at,updated_at)
		VALUES ($1,$2,'old-user','old-device',$3,'internal_direct','restricted','restricted',1,0,0,$4,$5,'password-compatible',$6,$6)`,
		oldID, s.runtimeID, site.ID, s.now.Add(time.Minute), binding.NASID, s.now); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.store.Exec(s.ctx, `DELETE FROM public.network_runtime_sessions WHERE id=$1`, oldID); err != nil {
			t.Errorf("remove legacy session fixture: %v", err)
		}
	})
	if _, err := s.repository.SaveAccessNetwork(s.ctx, first, true); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("enabling while an old password session is active: %v", err)
	}
	if err := s.store.Exec(s.ctx, `UPDATE public.network_runtime_sessions SET valid_until=$1 WHERE id=$2`, s.now.Add(-time.Second), oldID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.repository.SaveAccessNetwork(s.ctx, first, true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.repository.SaveAccessNetwork(s.ctx, create("finance"), true); err != nil {
		t.Fatalf("one controller must support multiple SSIDs: %v", err)
	}
	overlap := create("same")
	overlap.Configuration.SSID = first.Configuration.SSID
	if _, err := s.repository.SaveAccessNetwork(s.ctx, overlap, true); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("overlapping SSID result = %v", err)
	}
	otherSite := create("cross-site")
	otherSite.Configuration.SiteID = s.site.ID
	if _, err := s.repository.SaveAccessNetwork(s.ctx, otherSite, true); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("cross-site NAS result = %v", err)
	}
	if err := s.repository.DeleteAccessNetwork(s.ctx, first.ID, s.now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.repository.GetAccessNetwork(s.ctx, first.ID); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("retired record is exposed in CRUD: %v", err)
	}
	items, err := s.repository.ListAccessNetworks(s.ctx, site.ID)
	if err != nil || len(items) != 2 || !items[1].Deleted {
		t.Fatalf("catalog retention result: %#v, %v", items, err)
	}
}
