package copilot

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	domain "github.com/opensoha/soha/internal/domain/copilot"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestRuntimeProfileRevisionsWithPostgres(t *testing.T) {
	repo, _, ctx := newCopilotPostgresTestRepository(t)
	profile := domain.RuntimeProfile{ID: "profile", Name: "First", Scope: domain.RuntimeScopeChat, Enabled: true, Assets: []domain.RuntimeAssetRef{}}
	var created atomic.Int64
	runInspectionConcurrently(t, func() error {
		item, err := repo.PublishRuntimeProfile(ctx, "admin", 0, profile)
		if errors.Is(err, apperrors.ErrConflict) {
			return nil
		}
		if err == nil && item.Revision == 1 {
			created.Add(1)
			return nil
		}
		return fmt.Errorf("publish revision 1: %+v %v", item, err)
	})
	if got := created.Load(); got != 1 {
		t.Fatalf("published %d concurrent initial revisions", got)
	}
	changedScope := profile
	changedScope.Scope = domain.RuntimeScopeHeya
	if _, err := repo.PublishRuntimeProfile(ctx, "admin", 1, changedScope); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("scope change: %v", err)
	}
	profile.Name = "Second"
	if _, err := repo.PublishRuntimeProfile(ctx, "admin", 1, profile); err != nil {
		t.Fatal(err)
	}
	assertProfileRevisionAuthorization(t, repo, ctx, 1, true)
	profile.Enabled = false
	if _, err := repo.PublishRuntimeProfile(ctx, "admin", 2, profile); err != nil {
		t.Fatal(err)
	}
	current, err := repo.ListCurrentRuntimeProfiles(ctx, []string{domain.RuntimeScopeChat})
	if err != nil || len(current) != 0 {
		t.Fatalf("disabled head exposed prior revision: %+v %v", current, err)
	}
	profile.Name, profile.Enabled = "First", true
	if _, err := repo.PublishRuntimeProfile(ctx, "admin", 3, profile); err != nil {
		t.Fatal(err)
	}
	assertProfileRevisionAuthorization(t, repo, ctx, 1, false)
	assertProfileRevisionAuthorization(t, repo, ctx, 4, true)
	history, err := repo.ListRuntimeProfileRevisions(ctx, profile.ID)
	if err != nil || len(history) != 4 || history[0].Revision != 4 || history[1].Enabled || history[3].Name != "First" {
		t.Fatalf("immutable history: %+v %v", history, err)
	}
	t.Run("selection survives stale metadata updates", func(t *testing.T) {
		verifyRuntimeProfileSelection(t, repo, ctx, profile.ID)
	})
}

func assertProfileRevisionAuthorization(t *testing.T, repo *Repository, ctx context.Context, revision int64, want bool) {
	t.Helper()
	allowed, err := repo.RuntimeProfileRevisionAuthorized(ctx, "profile", revision, domain.RuntimeScopeChat)
	if err != nil || allowed != want {
		t.Fatalf("revision %d authorization = %v, %v, want %v", revision, allowed, err, want)
	}
}

func verifyRuntimeProfileSelection(t *testing.T, repo *Repository, ctx context.Context, id string) {
	t.Helper()
	now := time.Now().UTC()
	stale, err := repo.CreateSession(ctx, domain.Session{ID: "session", CreatedBy: "owner", Title: "Chat", Metadata: map[string]any{"status": "active", "legacy": true}, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SelectSessionRuntimeProfile(ctx, "foreign", stale.ID, id); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("foreign selection: %v", err)
	}
	selected, err := repo.SelectSessionRuntimeProfile(ctx, "owner", stale.ID, id)
	if err != nil || selected.Metadata["runtimeProfileId"] != id || selected.Metadata["legacy"] != true {
		t.Fatalf("narrow selection: %+v %v", selected.Metadata, err)
	}
	stale.Title = "New title"
	updated, err := repo.UpdateSession(ctx, "owner", stale.ID, stale)
	if err != nil || updated.Title != "New title" || updated.Metadata["runtimeProfileId"] != id {
		t.Fatalf("stale update: %+v %v", updated, err)
	}
	if _, err := repo.SelectSessionRuntimeProfile(ctx, "owner", stale.ID, ""); err != nil {
		t.Fatal(err)
	}
	updated, err = repo.UpdateSession(ctx, "owner", updated.ID, updated)
	if err != nil || updated.Metadata["runtimeProfileId"] != nil {
		t.Fatalf("stale selection restored after clearing: %+v %v", updated, err)
	}
	updated.Metadata["status"] = "archived"
	if _, err := repo.UpdateSession(ctx, "owner", updated.ID, updated); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SelectSessionRuntimeProfile(ctx, "owner", updated.ID, id); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("archived session selection: %v", err)
	}
}
