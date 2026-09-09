package networkaccess

import (
	"context"
	"reflect"
	"testing"

	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainnetworkaccess "github.com/opensoha/soha/internal/domain/networkaccess"
)

type captureAudit struct{ entry domainaudit.Entry }

func (capture *captureAudit) Record(_ context.Context, entry domainaudit.Entry) error {
	capture.entry = entry
	return nil
}

func TestRecordPreviewKeepsAuthorizationEvidence(t *testing.T) {
	capture := &captureAudit{}
	service := &Service{audit: capture}
	input := PreviewInput{SubjectUserID: "user-1", DeviceID: "device-1", ResourceID: "resource-1", SiteID: "site-1", Mode: domainnetworkaccess.ModeExternalVPNZTNA}
	result := domainnetworkaccess.PolicyPreview{
		Decision: domainnetworkaccess.DecisionAllow, Path: domainnetworkaccess.PathWireGuardZTNA,
		NetworkProfile: domainnetworkaccess.ProfileRestricted, PolicyVersion: 9, Protected: true,
		NetworkLeaseRequired: true, ResourceLeaseRequired: true, Reasons: []string{"policy_allow:policy-1"},
	}

	service.recordPreview(context.Background(), domainidentity.Principal{UserID: "operator-1"}, input, result)
	want := map[string]any{
		"subjectUserId": input.SubjectUserID, "deviceId": input.DeviceID, "resourceId": input.ResourceID,
		"siteId": input.SiteID, "mode": input.Mode, "path": result.Path, "networkProfile": result.NetworkProfile,
		"policyVersion": result.PolicyVersion, "protected": result.Protected,
		"networkLeaseRequired": result.NetworkLeaseRequired, "resourceLeaseRequired": result.ResourceLeaseRequired,
		"reasons": result.Reasons,
	}
	if !reflect.DeepEqual(capture.entry.Metadata, want) {
		t.Fatalf("preview audit metadata = %#v, want %#v", capture.entry.Metadata, want)
	}
}
