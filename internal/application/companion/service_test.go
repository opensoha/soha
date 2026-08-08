package companion

import (
	"context"
	"errors"
	"testing"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domaincompanion "github.com/opensoha/soha/internal/domain/companion"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainplugin "github.com/opensoha/soha/internal/domain/plugin"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type testRolePermissions map[string][]string

func (s testRolePermissions) ListRolePermissions(context.Context) (map[string][]string, error) {
	return s, nil
}

type captureRepository struct {
	interaction domaincompanion.ApplyInteraction
	profile     domaincompanion.Profile
}

func (r *captureRepository) EnsureProfile(context.Context, string, time.Time) (domaincompanion.Profile, error) {
	return r.profile, nil
}

func (r *captureRepository) ApplyInteraction(_ context.Context, input domaincompanion.ApplyInteraction) (domaincompanion.InteractionReceipt, bool, error) {
	r.interaction = input
	return domaincompanion.InteractionReceipt{IdempotencyKey: input.IdempotencyKey, Applied: true, Profile: r.profile}, true, nil
}

func (r *captureRepository) ResetProfile(context.Context, domaincompanion.ResetProfile) (domaincompanion.Profile, bool, error) {
	return r.profile, true, nil
}

func (r *captureRepository) PutArtifact(context.Context, domaincompanion.Artifact) (domaincompanion.Artifact, error) {
	return domaincompanion.Artifact{}, nil
}

func (r *captureRepository) ListArtifacts(context.Context, string) ([]domaincompanion.Artifact, error) {
	return nil, nil
}

func (r *captureRepository) ActivateArtifact(context.Context, string, string, time.Time) (domaincompanion.Artifact, error) {
	return domaincompanion.Artifact{}, nil
}

func (r *captureRepository) GetActiveArtifact(context.Context, string) (domaincompanion.Artifact, error) {
	return domaincompanion.Artifact{}, nil
}

func (r *captureRepository) RetireArtifacts(context.Context, string, time.Time) error { return nil }

type testPluginReader struct{}

func (testPluginReader) GetInstalled(context.Context, string) (domainplugin.InstalledPlugin, error) {
	return domainplugin.InstalledPlugin{}, apperrors.ErrNotFound
}

func TestRecordInteractionUsesServerDefinition(t *testing.T) {
	repo := &captureRepository{profile: domaincompanion.Profile{ID: "profile:user-1", OwnerID: "user-1", Level: 1, Revision: 1}}
	service, err := New(repo, testPluginReader{}, appaccess.NewPermissionResolver(testRolePermissions{
		"assistant": {appaccess.PermObserveAIChatUse},
	}), nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, created, err := service.RecordInteraction(context.Background(), domainidentity.Principal{
		UserID: "user-1", Roles: []string{"assistant"},
	}, "interaction-0001", domaincompanion.InteractionRequest{
		PluginID: domaincompanion.BuiltinPluginID, InteractionID: "pet", ClientRevision: 1,
	})
	if err != nil {
		t.Fatalf("RecordInteraction() error = %v", err)
	}
	if !created {
		t.Fatal("RecordInteraction() created = false, want true")
	}
	if repo.interaction.RewardXP != 2 || repo.interaction.RewardAffinity != 2 {
		t.Fatalf("server reward = (%d, %d), want (2, 2)", repo.interaction.RewardXP, repo.interaction.RewardAffinity)
	}
	if repo.interaction.InteractionID != "pet" || repo.interaction.Cooldown != 5*time.Second {
		t.Fatalf("interaction definition = %#v, want pet with 5s cooldown", repo.interaction)
	}
}

func TestRecordInteractionRequiresPermission(t *testing.T) {
	service, err := New(&captureRepository{}, testPluginReader{}, appaccess.NewPermissionResolver(testRolePermissions{
		"viewer": {appaccess.PermPluginView},
	}), nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, _, err = service.RecordInteraction(context.Background(), domainidentity.Principal{
		UserID: "user-1", Roles: []string{"viewer"},
	}, "interaction-0001", domaincompanion.InteractionRequest{
		PluginID: domaincompanion.BuiltinPluginID, InteractionID: "pet",
	})
	if !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("RecordInteraction() error = %v, want access denied", err)
	}
}

func TestRecordInteractionRejectsShortIdempotencyKey(t *testing.T) {
	service, err := New(&captureRepository{}, testPluginReader{}, appaccess.NewPermissionResolver(testRolePermissions{
		"assistant": {appaccess.PermObserveAIChatUse},
	}), nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, _, err = service.RecordInteraction(context.Background(), domainidentity.Principal{
		UserID: "user-1", Roles: []string{"assistant"},
	}, "short", domaincompanion.InteractionRequest{
		PluginID: domaincompanion.BuiltinPluginID, InteractionID: "tap",
	})
	if !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("RecordInteraction() error = %v, want invalid argument", err)
	}
}
