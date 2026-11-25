package settings

import (
	"context"
	"errors"
	"math"
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainsettings "github.com/opensoha/soha/internal/domain/settings"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type meshSettingsTestWriter struct{ *captureSettingsStore }

func (w meshSettingsTestWriter) UpdateAssistantMesh(ctx context.Context, actor string, value map[string]any, audit func(context.Context, map[string]any, bool) error) error {
	before, found, err := w.Get(ctx, domainsettings.AssistantMeshSettingKey)
	if err != nil {
		return err
	}
	if err := audit(ctx, before, found); err != nil {
		return err
	}
	return w.Upsert(ctx, domainsettings.AssistantMeshSettingKey, "ai", value, actor)
}

const activeMeshReviewer = "f6f94ced-ce1f-4af5-b7d1-b33a7d51910b"

type meshSettingsTestUsers map[string]string

func (u meshSettingsTestUsers) GetByID(_ context.Context, id string) (domainidentity.User, error) {
	return domainidentity.User{ID: id, Status: u[id]}, nil
}

func TestAssistantMeshSettingsDefaultsAndUpdate(t *testing.T) {
	ctx := context.Background()
	store := &captureSettingsStore{}
	audit := &captureSettingsAudit{}
	permissions := appaccess.NewPermissionResolver(fixedSettingsPermissionReader{
		"editor": {appaccess.PermSettingsAIView, appaccess.ManagedActionPermission(appaccess.PermSettingsAIManage, "update")},
		"viewer": {appaccess.PermSettingsAIView},
	})
	service := New(store, permissions, audit, nil)
	service.SetAssistantMeshSettingsDependencies(meshSettingsTestWriter{store}, meshSettingsTestUsers{activeMeshReviewer: "active"})
	editor := domainidentity.Principal{UserID: "alice", Roles: []string{"editor"}}
	viewer := domainidentity.Principal{UserID: "bob", Roles: []string{"viewer"}}
	if _, err := service.GetAssistantMeshSettings(ctx, viewer); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("viewer read = %v", err)
	}
	input := readAssistantMeshDefaults(t, ctx, service, editor)
	input.DefaultProfileOpen = new(true)
	input.ReceptionMinScore = new(0.75)
	input.WorkPulseDefaultLifetimeHours = 36
	input.WorkPulseMaxLifetimeDays = 21
	input.Enabled = true
	input.ExperienceCardLifetimeDays = 120
	input.DelegationLifetimeHours = 12
	input.InquiryPairLimitPerHour = 16
	input.InquiryRetentionDays = 25
	input.OrganizationApproverUserIDs = []string{activeMeshReviewer}
	if _, err := service.UpdateAssistantMeshSettings(ctx, viewer, input); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("viewer update = %v", err)
	}
	if _, err := service.UpdateAssistantMeshSettings(ctx, editor, input); err != nil {
		t.Fatal(err)
	}
	got, err := service.GetAssistantMeshSettings(ctx, editor)
	if err != nil || !got.Enabled || got.ExperienceCardLifetimeDays != 120 || got.DelegationLifetimeHours != 12 || len(got.OrganizationApproverUserIDs) != 1 || got.OrganizationApproverUserIDs[0] != activeMeshReviewer {
		t.Fatalf("saved settings = %+v, %v", got, err)
	}
	assertAssistantMeshUpdateAudit(t, audit)
	assertAssistantMeshLegacyUpdate(t, ctx, service, editor, input)
}

func readAssistantMeshDefaults(t *testing.T, ctx context.Context, service *Service, editor domainidentity.Principal) domainsettings.AssistantMeshSettings {
	t.Helper()
	defaults, err := service.GetAssistantMeshSettings(ctx, editor)
	if err != nil || defaults.Enabled || defaults.ExperienceCardLifetimeDays != 90 || defaults.DelegationLifetimeHours != 4 || defaults.DefaultVisibility != "private" || !defaults.AllowPermanentExperienceCards {
		t.Fatalf("defaults = %+v, %v", defaults, err)
	}
	if defaults.DefaultProfileOpen == nil || *defaults.DefaultProfileOpen || defaults.ReceptionMinScore == nil ||
		*defaults.ReceptionMinScore != 0 || defaults.WorkPulseDefaultLifetimeHours != 24 || defaults.WorkPulseMaxLifetimeDays != 14 {
		t.Fatalf("policy defaults = %+v", defaults)
	}
	return defaults
}

func assertAssistantMeshUpdateAudit(t *testing.T, audit *captureSettingsAudit) {
	t.Helper()
	if len(audit.entries) != 1 {
		t.Fatalf("unexpected audit count: %d", len(audit.entries))
	}
	entry := audit.entries[0]
	before, beforeOK := entry.Metadata["before"].(map[string]any)
	after, afterOK := entry.Metadata["after"].(map[string]any)
	if !beforeOK || !afterOK || entry.ActorID != "alice" || before["enabled"] != false || after["enabled"] != true {
		t.Fatalf("unexpected audit: %+v", audit.entries)
	}
	if before["defaultProfileOpen"] != false || after["defaultProfileOpen"] != true || after["receptionMinScore"] != 0.75 ||
		after["workPulseDefaultLifetimeHours"] != 36 || after["workPulseMaxLifetimeDays"] != 21 {
		t.Fatalf("policy audit = %+v", entry)
	}
}

func assertAssistantMeshLegacyUpdate(t *testing.T, ctx context.Context, service *Service, editor domainidentity.Principal, input domainsettings.AssistantMeshSettings) {
	t.Helper()
	input.DefaultProfileOpen, input.ReceptionMinScore = nil, nil
	input.WorkPulseDefaultLifetimeHours, input.WorkPulseMaxLifetimeDays = 0, 0
	input.DelegationLifetimeHours = 0
	input.InquiryPairLimitPerHour = 0
	input.InquiryRetentionDays = 0
	saved, err := service.UpdateAssistantMeshSettings(ctx, editor, input)
	if err != nil || saved.DelegationLifetimeHours != 12 || saved.InquiryPairLimitPerHour != 16 || saved.InquiryRetentionDays != 25 {
		t.Fatalf("legacy update lost timeout: %+v, %v", saved, err)
	}
	if saved.DefaultProfileOpen == nil || !*saved.DefaultProfileOpen || saved.ReceptionMinScore == nil ||
		*saved.ReceptionMinScore != 0.75 || saved.WorkPulseDefaultLifetimeHours != 36 || saved.WorkPulseMaxLifetimeDays != 21 {
		t.Fatalf("legacy update lost new settings: %+v", saved)
	}
	input.WorkPulseMaxLifetimeDays = 1
	if _, err := service.UpdateAssistantMeshSettings(ctx, editor, input); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("reduced maximum below stored default: %v", err)
	}
	input.WorkPulseDefaultLifetimeHours = 24
	input.DefaultProfileOpen, input.ReceptionMinScore = new(false), new(0.0)
	saved, err = service.UpdateAssistantMeshSettings(ctx, editor, input)
	if err != nil || saved.DefaultProfileOpen == nil || *saved.DefaultProfileOpen || saved.ReceptionMinScore == nil || *saved.ReceptionMinScore != 0 {
		t.Fatalf("explicit false and zero were not saved: %+v, %v", saved, err)
	}
}

func TestAssistantMeshSettingsRejectsInvalidUpdates(t *testing.T) {
	ctx := context.Background()
	store := &captureSettingsStore{}
	audit := &captureSettingsAudit{}
	service := New(store, appaccess.NewPermissionResolver(fixedSettingsPermissionReader{
		"editor": {appaccess.ManagedActionPermission(appaccess.PermSettingsAIManage, "update")},
	}), audit, nil)
	service.SetAssistantMeshSettingsDependencies(meshSettingsTestWriter{store}, meshSettingsTestUsers{activeMeshReviewer: "active"})
	editor := domainidentity.Principal{UserID: "alice", Roles: []string{"editor"}}
	for name, change := range map[string]func(*domainsettings.AssistantMeshSettings){
		"out-of-range lifetime": func(in *domainsettings.AssistantMeshSettings) { in.ExperienceCardLifetimeDays = 366 },
		"paired limit":          func(in *domainsettings.AssistantMeshSettings) { in.InquiryPairLimitPerHour = 61 },
		"pending limit":         func(in *domainsettings.AssistantMeshSettings) { in.InquiryPendingLimit = 4 },
		"work pulse default":    func(in *domainsettings.AssistantMeshSettings) { in.WorkPulseDefaultLifetimeHours = 169 },
		"work pulse maximum":    func(in *domainsettings.AssistantMeshSettings) { in.WorkPulseMaxLifetimeDays = 91 },
		"reception score":       func(in *domainsettings.AssistantMeshSettings) { in.ReceptionMinScore = new(1.01) },
		"nonfinite score":       func(in *domainsettings.AssistantMeshSettings) { in.ReceptionMinScore = new(math.NaN()) },
		"body retention":        func(in *domainsettings.AssistantMeshSettings) { in.InquiryRetentionDays = 366 },
		"organization default":  func(in *domainsettings.AssistantMeshSettings) { in.DefaultVisibility = "organization" },
		"invalid reviewer ID":   func(in *domainsettings.AssistantMeshSettings) { in.OrganizationApproverUserIDs = []string{"reviewer"} },
		"inactive reviewer": func(in *domainsettings.AssistantMeshSettings) {
			in.OrganizationApproverUserIDs = []string{"af743c56-8694-4c36-a366-3dbb49e913dc"}
		},
		"duplicate reviewers": func(in *domainsettings.AssistantMeshSettings) {
			in.OrganizationApproverUserIDs = []string{activeMeshReviewer, activeMeshReviewer}
		},
	} {
		t.Run(name, func(t *testing.T) {
			input := domainsettings.DefaultAssistantMeshSettings()
			change(&input)
			if _, err := service.UpdateAssistantMeshSettings(ctx, editor, input); !errors.Is(err, apperrors.ErrInvalidArgument) {
				t.Fatalf("update error = %v", err)
			}
		})
	}
	if len(store.values) != 0 || len(audit.entries) != 0 {
		t.Fatal("invalid update had side effects")
	}
	audit.err = errors.New("audit unavailable")
	if _, err := service.UpdateAssistantMeshSettings(ctx, editor, domainsettings.DefaultAssistantMeshSettings()); !errors.Is(err, audit.err) || len(store.values) != 0 {
		t.Fatalf("audit failure must prevent settings update: %v", err)
	}
}
