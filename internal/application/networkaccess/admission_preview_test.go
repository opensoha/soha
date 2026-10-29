package networkaccess

import (
	"context"
	"errors"
	"testing"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func admissionPreviewFixture(now time.Time) domain.AdmissionPreviewSnapshot {
	snapshot := admissionSnapshot(now)
	snapshot.Policies.Policies[0].AccessProfile = domain.ProfileFull
	for key, profile := range snapshot.Profiles {
		profile.SiteID = snapshot.Site.ID
		snapshot.Profiles[key] = profile
	}
	return domain.AdmissionPreviewSnapshot{Subject: snapshot.Subject, Device: snapshot.Device, Site: snapshot.Site, Binding: snapshot.Binding, Policies: snapshot.Policies, Profiles: snapshot.Profiles}
}

func TestAdmissionPreviewIncludesCurrentRolesHealthAndRestrictionVersions(t *testing.T) {
	now := time.Now().UTC()
	baseline := EvaluateAdmissionPreview(admissionPreviewFixture(now), now)
	if baseline.Decision != domain.DecisionAllow || baseline.NetworkProfile != domain.ProfileFull || baseline.AttributeVersion == "" || baseline.EvaluatedAt == nil || !baseline.EvaluatedAt.Equal(now) {
		t.Fatalf("baseline=%#v", baseline)
	}
	for _, test := range []struct {
		name    string
		change  func(*domain.AdmissionPreviewSnapshot)
		profile string
	}{
		{"removed role", func(s *domain.AdmissionPreviewSnapshot) {
			s.Policies.Policies[0].Subjects.Roles = []string{"revoked-role"}
		}, domain.ProfileDeny},
		{"unhealthy endpoint", func(s *domain.AdmissionPreviewSnapshot) {
			config := domain.EndpointResponsePolicyConfig{SiteIDs: []string{s.Site.ID}, RequireHealthyForAdmission: true}
			s.ResponsePolicies = []domain.EndpointResponsePolicy{{ID: "health", PublishedRevision: 1, PublishedConfiguration: &config}}
		}, domain.ProfileDeny},
		{"durable floor", func(s *domain.AdmissionPreviewSnapshot) {
			s.Restriction = domain.EndpointRestriction{AccessProfile: domain.ProfileRestricted, Epoch: 1}
		}, domain.ProfileRestricted},
		{"binding disabled", func(s *domain.AdmissionPreviewSnapshot) { s.Binding.Status = "disabled" }, domain.ProfileDeny},
		{"profile missing", func(s *domain.AdmissionPreviewSnapshot) { delete(s.Profiles, domain.ProfileFull) }, domain.ProfileDeny},
		{"policy changed", func(s *domain.AdmissionPreviewSnapshot) { s.Policies.PolicyVersion++ }, domain.ProfileFull},
		{"health observation changed", func(s *domain.AdmissionPreviewSnapshot) { s.Presence.PayloadHash = "new-observation" }, domain.ProfileFull},
	} {
		t.Run(test.name, func(t *testing.T) {
			snapshot := admissionPreviewFixture(now)
			test.change(&snapshot)
			result := EvaluateAdmissionPreview(snapshot, now)
			if result.NetworkProfile != test.profile || result.AttributeVersion == baseline.AttributeVersion || (result.Decision == domain.DecisionDeny) != (test.profile == domain.ProfileDeny) {
				t.Fatalf("preview=%#v", result)
			}
		})
	}
}

type admissionPreviewStub struct {
	Store
	snapshot domain.AdmissionPreviewSnapshot
	reads    int
}

func (store *admissionPreviewStub) AdmissionPreview(context.Context, string, string) (domain.AdmissionPreviewSnapshot, error) {
	store.reads++
	return store.snapshot, nil
}

func TestAdmissionPreviewRequiresPermissionAndTargetScope(t *testing.T) {
	store := &admissionPreviewStub{snapshot: admissionPreviewFixture(time.Now().UTC())}
	service, principal := newScopeService(&scopeStore{}, appaccess.PermNetworkAccessPolicyView)
	auditor := &captureAudit{}
	service.store, service.audit = store, auditor
	input := AdmissionPreviewInput{DeviceID: store.snapshot.Device.ID, NASBindingID: store.snapshot.Binding.ID}
	if _, err := service.PreviewAdmission(context.Background(), principal, input); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("out-of-scope preview=%v", err)
	}
	service.scope = allowTestNetworkScope()
	result, err := service.PreviewAdmission(context.Background(), principal, input)
	if err != nil || result.Decision != domain.DecisionAllow {
		t.Fatalf("allowed preview=%#v error=%v", result, err)
	}
	audit := auditor.entry
	if audit.Action != "network_access.admission.preview" || audit.ResourceName != input.NASBindingID || audit.Metadata["attributeVersion"] != result.AttributeVersion {
		t.Fatalf("admission preview lost its target or input version: %#v", audit)
	}
	principal.Roles = nil
	reads := store.reads
	if _, err := service.PreviewAdmission(context.Background(), principal, input); !errors.Is(err, apperrors.ErrAccessDenied) || store.reads != reads {
		t.Fatalf("permissionless preview=%v reads=%d", err, store.reads)
	}
}
