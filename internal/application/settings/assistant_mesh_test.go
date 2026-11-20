package settings

import (
	"context"
	"errors"
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
	defaults, err := service.GetAssistantMeshSettings(ctx, editor)
	if err != nil || defaults.Enabled || defaults.ExperienceCardLifetimeDays != 90 || defaults.DefaultVisibility != "private" || !defaults.AllowPermanentExperienceCards {
		t.Fatalf("defaults = %+v, %v", defaults, err)
	}
	input := defaults
	input.Enabled = true
	input.ExperienceCardLifetimeDays = 120
	input.OrganizationApproverUserIDs = []string{activeMeshReviewer}
	if _, err := service.UpdateAssistantMeshSettings(ctx, viewer, input); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("viewer update = %v", err)
	}
	if _, err := service.UpdateAssistantMeshSettings(ctx, editor, input); err != nil {
		t.Fatal(err)
	}
	got, err := service.GetAssistantMeshSettings(ctx, editor)
	if err != nil || !got.Enabled || got.ExperienceCardLifetimeDays != 120 || len(got.OrganizationApproverUserIDs) != 1 || got.OrganizationApproverUserIDs[0] != activeMeshReviewer {
		t.Fatalf("saved settings = %+v, %v", got, err)
	}
	if len(audit.entries) != 1 {
		t.Fatalf("unexpected audit count: %d", len(audit.entries))
	}
	entry := audit.entries[0]
	if entry.ActorID != "alice" || entry.Metadata["before"].(map[string]any)["enabled"] != false || entry.Metadata["after"].(map[string]any)["enabled"] != true {
		t.Fatalf("unexpected audit: %+v", audit.entries)
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
