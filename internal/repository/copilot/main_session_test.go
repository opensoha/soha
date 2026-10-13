package copilot

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	dbstore "github.com/opensoha/soha/internal/infrastructure/db"
	"sync/atomic"
	"testing"
	"time"
)

func TestMainSessionWithPostgres(t *testing.T) {
	repo, store, ctx := newCopilotPostgresTestRepository(t)
	owner := "main-chat-" + uuid.NewString()
	candidate := func(actor, kind string) domain.Session {
		now := time.Now().UTC()
		return domain.Session{ID: uuid.NewString(), Title: kind, CreatedBy: actor, Metadata: map[string]any{"conversationKind": kind, "agentProviderId": "internal"}, CreatedAt: now, UpdatedAt: now}
	}
	side, err := repo.CreateSession(ctx, candidate(owner, "side"))
	if err != nil {
		t.Fatal(err)
	}
	var created atomic.Int64
	ids := make(chan string, 8)
	runInspectionConcurrently(t, func() error {
		session, fresh, err := repo.GetOrCreateMainSession(ctx, candidate(owner, "main"))
		if fresh {
			created.Add(1)
		}
		if err == nil {
			ids <- session.ID
		}
		return err
	})
	close(ids)
	var id string
	for current := range ids {
		if id != "" && id != current {
			t.Fatal("concurrent main chats differ")
		}
		id = current
	}
	if id == side.ID || created.Load() != 1 {
		t.Fatalf("history promoted or duplicate: %s %d", id, created.Load())
	}
	other := testMainSessionReopenAndOwner(t, repo, store, ctx, owner, id, candidate)
	t.Run("personal assistant identity", func(t *testing.T) { testMainSessionPersonalAssistant(t, repo, store, ctx, owner, id, side, other) })
	t.Run("heya access decisions", func(t *testing.T) { testHeyaAccessDecisions(t, repo, ctx, owner) })
}

func testMainSessionReopenAndOwner(t *testing.T, repo *Repository, store *dbstore.Store, ctx context.Context, owner, id string, candidate func(string, string) domain.Session) domain.Session {
	t.Helper()
	session, err := repo.GetSession(ctx, owner, id)
	if err != nil {
		t.Fatal(err)
	}
	session.Title = "Renamed"
	session.Metadata["agentProviderId"] = "hermes"
	if _, err := repo.UpdateSession(ctx, owner, id, session); err != nil {
		t.Fatal(err)
	}
	again, fresh, err := repo.GetOrCreateMainSession(context.Background(), candidate(owner, "main"))
	if err != nil || fresh || again.ID != id || again.Title != "Renamed" || again.Metadata["agentProviderId"] != "hermes" {
		t.Fatalf("reopen reset preferences: %+v %v", again, err)
	}
	other, _, err := repo.GetOrCreateMainSession(ctx, candidate(owner+"-other", "main"))
	if err != nil || other.ID == id {
		t.Fatalf("user isolation: %+v %v", other, err)
	}
	if _, err := repo.GetSession(ctx, owner+"-other", id); err == nil {
		t.Fatal("cross-user read")
	}
	var count int64
	if err := store.DB().Raw(`SELECT COUNT(*) FROM ai_sessions WHERE created_by = ? AND metadata->>'conversationKind'='main'`, owner).Scan(&count).Error; err != nil || count != 1 {
		t.Fatalf("unique index: %d %v", count, err)
	}
	return other
}

func testMainSessionPersonalAssistant(t *testing.T, repo *Repository, store *dbstore.Store, ctx context.Context, owner, id string, side, other domain.Session) {
	t.Helper()
	candidate := domain.PersonalAssistant{ID: uuid.NewString(), CreatedBy: owner, Name: "Soha", MainSessionID: id, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	first, err := repo.GetOrCreatePersonalAssistant(ctx, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.RenamePersonalAssistant(ctx, owner, "小苏"); err != nil {
		t.Fatal(err)
	}
	avatar := testMainSessionAssistantAvatar(t, repo, ctx, owner)
	runInspectionConcurrently(t, func() error {
		local := candidate
		local.ID = uuid.NewString()
		again, err := repo.GetOrCreatePersonalAssistant(ctx, local)
		if err != nil {
			return err
		}
		if again.ID != first.ID || again.Name != "小苏" || again.MainSessionID != id || again.AvatarURL != avatar.AvatarURL || again.AvatarMotion != "off" {
			return fmt.Errorf("identity changed: %+v", again)
		}
		return nil
	})
	foreign := candidate
	foreign.ID = uuid.NewString()
	foreign.CreatedBy = owner + "-other"
	if _, err := repo.GetOrCreatePersonalAssistant(ctx, foreign); err == nil {
		t.Fatal("foreign main chat accepted")
	}
	foreign.MainSessionID = other.ID
	second, err := repo.GetOrCreatePersonalAssistant(ctx, foreign)
	if err != nil || second.ID == first.ID {
		t.Fatalf("assistant owner isolation: %+v %v", second, err)
	}
	if second.AvatarURL != "" {
		t.Fatal("avatar leaked to another owner")
	}
	t.Run("chat avatar transaction", func(t *testing.T) { testMainSessionChatAvatar(t, repo, ctx, owner, id, other.ID, avatar) })
	if _, err := repo.UpdatePersonalAssistantAvatar(ctx, "unknown-owner", avatar); err == nil {
		t.Fatal("avatar update without owned assistant")
	}
	var count int64
	if err := store.DB().Raw(`SELECT COUNT(*) FROM ai_personal_assistants WHERE created_by = ?`, owner).Scan(&count).Error; err != nil || count != 1 {
		t.Fatalf("assistant uniqueness: %d %v", count, err)
	}
	personal, personalSide := testMainSessionPersonalChats(t, repo, ctx, owner, id, candidate, first, avatar)
	testMainSessionArtifacts(t, repo, ctx, owner, personal.ID, personalSide, other)
	testHeyaMessageIsolation(t, repo, ctx, owner, id, personal.ID)
	testMainSessionArchivedTasks(t, repo, ctx, owner, personal.ID, personalSide, other)
}

func testMainSessionPersonalChats(t *testing.T, repo *Repository, ctx context.Context, owner, ordinaryID string, candidate, first domain.PersonalAssistant, avatar domain.PersonalAssistantAvatar) (domain.Session, domain.Session) {
	t.Helper()
	personal := domain.Session{ID: uuid.NewString(), CreatedBy: owner, Title: "Heya", Metadata: map[string]any{"conversationKind": "personal_main"}, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	personal, _, err := repo.GetOrCreateMainSession(ctx, personal)
	if err != nil {
		t.Fatal(err)
	}
	personalSide := personal
	personalSide.ID = uuid.NewString()
	personalSide.Metadata = map[string]any{"conversationKind": "personal_side"}
	personalSide, err = repo.CreateSession(ctx, personalSide)
	if err != nil {
		t.Fatal(err)
	}
	candidate.MainSessionID = personal.ID
	rebound, err := repo.GetOrCreatePersonalAssistant(ctx, candidate)
	if err != nil || rebound.ID != first.ID || rebound.Name != "小苏" || rebound.AvatarURL != avatar.AvatarURL || rebound.MainSessionID != personal.ID {
		t.Fatalf("migration erased identity: %+v %v", rebound, err)
	}
	ordinary, err := repo.GetSession(ctx, owner, ordinaryID)
	if err != nil || ordinary.Metadata["conversationKind"] != "main" {
		t.Fatalf("legacy main was reclassified: %+v %v", ordinary, err)
	}
	chats, err := repo.ListSessions(ctx, owner, 100)
	if err != nil || len(chats) != 2 {
		t.Fatalf("ordinary sessions: %+v %v", chats, err)
	}
	personals, err := repo.ListPersonalSessions(ctx, owner, 100)
	if err != nil || len(personals) != 2 {
		t.Fatalf("Heya sessions: %+v %v", personals, err)
	}
	return personal, personalSide
}

func testHeyaMessageIsolation(t *testing.T, repo *Repository, ctx context.Context, owner, ordinaryID, personalID string) {
	t.Helper()
	for _, sessionID := range []string{ordinaryID, personalID} {
		if _, err := repo.CreateMessage(ctx, domain.Message{ID: uuid.NewString(), SessionID: sessionID, Role: "user", Content: "search boundary", Metadata: map[string]any{}, CreatedAt: time.Now().UTC()}); err != nil {
			t.Fatal(err)
		}
	}
	for _, personal := range []bool{false, true} {
		rows, err := repo.SearchMessageCandidates(ctx, owner, domain.MessageCursor{Personal: personal}, 100)
		if err != nil || len(rows) == 0 {
			t.Fatalf("scene search personal=%v: %v", personal, err)
		}
		for _, row := range rows {
			session, err := repo.GetSession(ctx, owner, row.Message.SessionID)
			if err != nil || (session.Metadata["conversationKind"] == "personal_main" || session.Metadata["conversationKind"] == "personal_side") != personal {
				t.Fatalf("scene leaked personal=%v: %+v %v", personal, row, err)
			}
		}
	}
	recent, err := repo.RecentOwnedMessages(ctx, owner, 100)
	if err != nil || len(recent) == 0 {
		t.Fatalf("Heya interests: %v", err)
	}
	for _, item := range recent {
		if item.SessionID == ordinaryID {
			t.Fatal("ordinary chat entered Heya interests")
		}
	}
}

func testMainSessionAssistantAvatar(t *testing.T, repo *Repository, ctx context.Context, owner string) domain.PersonalAssistantAvatar {
	t.Helper()
	avatar := domain.PersonalAssistantAvatar{AvatarURL: "persisted-avatar", AvatarPosterURL: "persisted-poster", AvatarMotion: "off"}
	if updated, err := repo.UpdatePersonalAssistantAvatar(ctx, owner, avatar); err != nil || updated.Name != "小苏" || updated.AvatarURL != avatar.AvatarURL {
		t.Fatalf("avatar persistence: %+v %v", updated, err)
	}
	if renamed, err := repo.RenamePersonalAssistant(ctx, owner, "小苏"); err != nil || renamed.AvatarPosterURL != avatar.AvatarPosterURL || renamed.AvatarMotion != "off" {
		t.Fatalf("rename erased avatar: %+v %v", renamed, err)
	}
	return avatar
}

func testMainSessionChatAvatar(t *testing.T, repo *Repository, ctx context.Context, owner, id, otherID string, avatar domain.PersonalAssistantAvatar) {
	t.Helper()
	receipt := domain.Message{ID: uuid.NewString(), SessionID: id, Role: "assistant", Content: "avatar updated", Metadata: map[string]any{"assistantProfileUpdated": true}, CreatedAt: time.Now().UTC()}
	before, err := repo.getPersonalAssistant(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	next := domain.PersonalAssistantAvatar{AvatarURL: "chat-image", AvatarPosterURL: "chat-poster", AvatarMotion: "auto"}
	updated, err := repo.CommitPersonalAssistantAvatar(ctx, owner, next, before, receipt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CommitPersonalAssistantAvatar(ctx, owner, avatar, before, receipt); err == nil {
		t.Fatal("stale CAS accepted")
	}
	// Duplicate receipt must roll back the image as well.
	if _, err := repo.CommitPersonalAssistantAvatar(ctx, owner, avatar, updated, receipt); err == nil {
		t.Fatal("duplicate receipt accepted")
	}
	current, err := repo.getPersonalAssistant(ctx, owner)
	if err != nil || current.AvatarURL != next.AvatarURL {
		t.Fatal("receipt failure did not roll back avatar")
	}
	foreignReceipt := receipt
	foreignReceipt.ID = uuid.NewString()
	foreignReceipt.SessionID = otherID
	if _, err := repo.CommitPersonalAssistantAvatar(ctx, owner, avatar, updated, foreignReceipt); err == nil {
		t.Fatal("foreign session accepted")
	}
	receipt.ID = uuid.NewString()
	receipt.Content = "undo avatar"
	if _, err := repo.CommitPersonalAssistantAvatar(ctx, owner, avatar, updated, receipt); err != nil {
		t.Fatal(err)
	}
	receipt.ID = uuid.NewString()
	if _, err := repo.CommitPersonalAssistantAvatar(ctx, owner, next, before, receipt); err == nil {
		t.Fatal("old version accepted after the same avatar was restored")
	}
}

func testMainSessionArtifacts(t *testing.T, repo *Repository, ctx context.Context, owner, id string, side, other domain.Session) {
	t.Helper()
	for index, sessionID := range []string{id, side.ID, other.ID} {
		_, err := repo.CreateMessage(ctx, domain.Message{ID: uuid.NewString(), SessionID: sessionID, Role: "assistant", Content: "report", Metadata: map[string]any{"analysisArtifacts": []domain.AnalysisArtifact{{Kind: "report", RunID: fmt.Sprintf("report-%d", index), Summary: "Saved report"}}}, CreatedAt: time.Now().UTC()})
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := repo.CreateMessage(ctx, domain.Message{ID: uuid.NewString(), SessionID: id, Role: "assistant", Content: "legacy", Metadata: map[string]any{"analysisArtifacts": "legacy-invalid"}, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	artifacts, err := repo.ListPersonalArtifacts(ctx, owner)
	if err != nil || len(artifacts) != 2 {
		t.Fatalf("cross-chat artifacts: %+v %v", artifacts, err)
	}
	for _, artifact := range artifacts {
		if artifact.SessionID != id && artifact.SessionID != side.ID {
			t.Fatalf("foreign artifact leaked: %+v", artifact)
		}
	}
}

func testMainSessionArchivedTasks(t *testing.T, repo *Repository, ctx context.Context, owner, id string, side, other domain.Session) {
	t.Helper()
	now := time.Now().UTC()
	for index, sessionID := range []string{id, side.ID, other.ID, ""} {
		actor := owner
		if sessionID == other.ID {
			actor = owner + "-other"
		}
		run := domain.AgentRun{ID: "agent:" + uuid.NewString(), ProviderID: "internal", ProviderKind: "native", CapabilityID: "general", SessionID: sessionID, CreatedBy: actor, Status: "succeeded", Input: map[string]any{}, Output: map[string]any{}, CallbackToken: uuid.NewString(), TimeoutSeconds: 600, QueuedAt: now, CreatedAt: now, UpdatedAt: now}
		if index == 1 {
			run.UpdatedAt = now.Add(time.Minute)
		}
		if _, err := repo.CreateAgentRun(ctx, run); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.DeleteSession(ctx, owner, side.ID); err != nil {
		t.Fatal(err)
	}
	runs, err := repo.ListAgentRuns(ctx, domain.AgentRunFilter{CreatedBy: owner, Limit: 1, ExcludeArchivedSessions: true})
	if err != nil || len(runs) != 1 || runs[0].SessionID == side.ID || runs[0].CreatedBy != owner {
		t.Fatalf("archived session consumed task limit: %+v %v", runs, err)
	}
	runs, err = repo.ListAgentRuns(ctx, domain.AgentRunFilter{CreatedBy: owner, ExcludeArchivedSessions: true})
	if err != nil || len(runs) != 2 {
		t.Fatalf("active or sessionless tasks lost: %+v %v", runs, err)
	}
	runs, err = repo.ListAgentRuns(ctx, domain.AgentRunFilter{CreatedBy: owner})
	if err != nil || len(runs) != 3 {
		t.Fatalf("runtime task history removed: %+v %v", runs, err)
	}
	testMainSessionPersonalTaskScopes(t, repo, ctx, owner, id)
}

func testMainSessionPersonalTaskScopes(t *testing.T, repo *Repository, ctx context.Context, owner, id string) {
	t.Helper()
	runs, err := repo.ListAgentRuns(ctx, domain.AgentRunFilter{CreatedBy: owner, PersonalSessionsOnly: true, ExcludeArchivedSessions: true})
	if err != nil || len(runs) != 1 || runs[0].SessionID != id {
		t.Fatalf("personal task scope: %+v %v", runs, err)
	}
	runs, err = repo.ListAgentRuns(ctx, domain.AgentRunFilter{CreatedBy: owner, ExcludePersonalSessions: true})
	if err != nil || len(runs) != 1 || runs[0].SessionID != "" {
		t.Fatalf("unapproved task scope: %+v %v", runs, err)
	}
	artifacts, err := repo.ListPersonalArtifacts(ctx, owner)
	if err != nil || len(artifacts) != 1 || artifacts[0].SessionID != id {
		t.Fatalf("archived session artifact leaked: %+v %v", artifacts, err)
	}
}
