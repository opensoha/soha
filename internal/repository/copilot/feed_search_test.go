package copilot

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	domain "github.com/opensoha/soha/internal/domain/copilot"
)

func TestFeedSearchWithPostgres(t *testing.T) {
	repo, _, ctx := newCopilotPostgresTestRepository(t)
	owner := uuid.NewString()
	now := time.Now().UTC().Truncate(time.Microsecond)
	t.Run("message window owner and long history", func(t *testing.T) { testMessageWindowOwnerAndLongHistory(t, repo, ctx, owner, now) })
	t.Run("feed read and feedback", func(t *testing.T) { testFeedReadAndFeedback(t, repo, ctx, owner, now) })
	t.Run("refresh claim and settings fence", func(t *testing.T) { testRefreshClaimAndSettingsFence(t, repo, ctx, owner, now) })
	t.Run("expired lease no replay", func(t *testing.T) { testExpiredLeaseNoReplay(t, repo, ctx, owner, now) })
}

func testMessageWindowOwnerAndLongHistory(t *testing.T, repo *Repository, ctx context.Context, owner string, now time.Time) {
	session, err := repo.CreateSession(ctx, domain.Session{ID: uuid.NewString(), CreatedBy: owner, Title: "long", Metadata: map[string]any{}, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for i := range 121 {
		id := uuid.NewString()
		ids = append(ids, id)
		if _, err := repo.CreateMessage(ctx, domain.Message{ID: id, SessionID: session.ID, Role: "user", Content: fmt.Sprintf("message %d", i), Metadata: map[string]any{}, CreatedAt: now.Add(time.Duration(i) * time.Second)}); err != nil {
			t.Fatal(err)
		}
	}
	window, err := repo.ReadMessageWindow(ctx, owner, session.ID, domain.MessageWindowInput{Around: ids[115], Limit: 11})
	if err != nil || len(window.Messages) != 11 || window.Messages[5].ID != ids[115] || window.BeforeCursor == "" || window.AfterCursor != "" {
		t.Fatalf("long window: %+v %v", window, err)
	}
	previous, err := repo.ReadMessageWindow(ctx, owner, session.ID, domain.MessageWindowInput{Before: window.BeforeCursor, Limit: 11})
	if err != nil || previous.Messages[len(previous.Messages)-1].ID != ids[109] {
		t.Fatalf("previous: %+v %v", previous, err)
	}
	latest, err := repo.ReadMessageWindow(ctx, owner, session.ID, domain.MessageWindowInput{Limit: 11})
	if err != nil || latest.Messages[10].ID != ids[120] {
		t.Fatal("latest window", err)
	}
	if _, err := repo.ReadMessageWindow(ctx, "foreign", session.ID, domain.MessageWindowInput{Around: ids[115], Limit: 11}); err == nil {
		t.Fatal("foreign anchor read")
	}
	testMessageSearchOwnerPagination(t, repo, ctx, owner)
	if err := repo.DeleteSession(ctx, owner, session.ID); err != nil {
		t.Fatal(err)
	}
	archived, err := repo.SearchMessageCandidates(ctx, owner, domain.MessageCursor{}, 17)
	if err != nil || len(archived) != 0 {
		t.Fatal("archived messages visible")
	}
}

func testFeedReadAndFeedback(t *testing.T, repo *Repository, ctx context.Context, owner string, now time.Time) {
	record, items, _, err := repo.ReadFeed(ctx, owner, "", 20)
	if err != nil || record.Settings.AutoUpdate || record.State != "idle" || len(items) != 0 {
		t.Fatalf("read queued work: %+v %v", record, err)
	}
	if _, claimed, err := repo.ClaimFeedRefresh(ctx, now); err != nil || claimed {
		t.Fatal("read triggered refresh", err)
	}
	if err := repo.QueueFeedRefresh(ctx, owner, now); err != nil {
		t.Fatal(err)
	}
	record, claimed, err := repo.ClaimFeedRefresh(ctx, now)
	if err != nil || !claimed {
		t.Fatal("claim", err)
	}
	item := domain.FeedItem{ID: uuid.NewString(), Title: "source", Body: "summary", Reason: "interest", Sources: []domain.FeedSource{{URL: "https://example.com/article", Title: "Article", Excerpt: "actual"}}}
	if err := repo.FinishFeedRefresh(ctx, record, []domain.FeedItem{item, item}, "", now); err != nil {
		t.Fatal(err)
	}
	_, items, _, err = repo.ReadFeed(ctx, owner, "", 20)
	if err != nil || len(items) != 1 {
		t.Fatal("dedup", len(items), err)
	}
	testFeedFeedbackPersistence(t, repo, ctx, owner, item.ID)
	if err := repo.QueueFeedRefresh(ctx, owner, now.Add(time.Second)); err == nil {
		t.Fatal("cooldown bypass")
	}
}

func testRefreshClaimAndSettingsFence(t *testing.T, repo *Repository, ctx context.Context, owner string, now time.Time) {
	actor := uuid.NewString()
	if err := repo.QueueFeedRefresh(ctx, actor, now); err != nil {
		t.Fatal(err)
	}
	var claims atomic.Int64
	records := make(chan domain.FeedRecord, 8)
	runInspectionConcurrently(t, func() error {
		record, ok, err := repo.ClaimFeedRefresh(ctx, now)
		if ok {
			claims.Add(1)
			records <- record
		}
		return err
	})
	close(records)
	if claims.Load() != 1 {
		t.Fatal("duplicate claims", claims.Load())
	}
	record := <-records
	settings := domain.FeedSettings{Instructions: "new preference", AutoUpdate: false}
	if err := repo.SaveFeedSettings(ctx, actor, settings, now); err != nil {
		t.Fatal(err)
	}
	if err := repo.FinishFeedRefresh(ctx, record, []domain.FeedItem{{ID: uuid.NewString(), Title: "stale", Sources: []domain.FeedSource{{URL: "https://example.com/stale"}}}}, "", now); err != nil {
		t.Fatal(err)
	}
	current, items, _, err := repo.ReadFeed(ctx, actor, "", 20)
	if err != nil || current.Settings.Instructions != "new preference" || current.State != "idle" || len(items) != 0 {
		t.Fatal("late result crossed preferences fence", err)
	}
	testFeedDailySchedule(t, repo, ctx, actor, settings, now)
}

func testExpiredLeaseNoReplay(t *testing.T, repo *Repository, ctx context.Context, owner string, now time.Time) {
	actor := uuid.NewString()
	if err := repo.QueueFeedRefresh(ctx, actor, now); err != nil {
		t.Fatal(err)
	}
	record, ok, err := repo.ClaimFeedRefresh(ctx, now)
	if err != nil || !ok {
		t.Fatal(err)
	}
	// Reopening the repository models restart without restoring an in-memory job.
	restarted := New(repo.db)
	if _, ok, err := restarted.ClaimFeedRefresh(ctx, now.Add(4*time.Minute)); err != nil || ok {
		t.Fatal("expired job replayed", err)
	}
	current, _, _, err := restarted.ReadFeed(ctx, actor, "", 20)
	if err != nil || current.State != "failed" {
		t.Fatal("expired job not terminal")
	}
	if err := repo.FinishFeedRefresh(ctx, record, nil, "", now.Add(4*time.Minute)); err != nil {
		t.Fatal(err)
	}
	current, _, _, _ = repo.ReadFeed(ctx, actor, "", 20)
	if current.State != "failed" {
		t.Fatal("late completion revived expired job")
	}
}

func testMessageSearchOwnerPagination(t *testing.T, repo *Repository, ctx context.Context, owner string) {
	cursor := domain.MessageCursor{}
	seen := map[string]bool{}
	for {
		items, err := repo.SearchMessageCandidates(ctx, owner, cursor, 17)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range items {
			if seen[item.Message.ID] {
				t.Fatal("duplicate search row")
			}
			seen[item.Message.ID] = true
			cursor = domain.MessageCursor{ID: item.Message.ID, CreatedAt: item.Message.CreatedAt}
		}
		if len(items) < 17 {
			break
		}
	}
	if len(seen) != 121 {
		t.Fatal("missing search rows", len(seen))
	}
	foreign, err := repo.SearchMessageCandidates(ctx, "foreign", domain.MessageCursor{}, 17)
	if err != nil || len(foreign) != 0 {
		t.Fatal("owner leak")
	}
}

func testFeedFeedbackPersistence(t *testing.T, repo *Repository, ctx context.Context, owner, itemID string) {
	liked := true
	if _, err := repo.SetFeedFeedback(ctx, "foreign", itemID, &liked, nil); err == nil {
		t.Fatal("foreign feedback")
	}
	if _, err := repo.GetFeedItem(ctx, "foreign", itemID); err == nil {
		t.Fatal("foreign discussion read")
	}
	changed, err := repo.SetFeedFeedback(ctx, owner, itemID, &liked, nil)
	if err != nil || !changed.Liked {
		t.Fatal("like persistence", err)
	}
	hidden := true
	if _, err := repo.SetFeedFeedback(ctx, owner, itemID, nil, &hidden); err != nil {
		t.Fatal(err)
	}
	_, items, _, err := repo.ReadFeed(ctx, owner, "", 20)
	if err != nil || len(items) != 0 {
		t.Fatal("hide persistence")
	}
}

func testFeedDailySchedule(t *testing.T, repo *Repository, ctx context.Context, actor string, settings domain.FeedSettings, now time.Time) {
	settings.AutoUpdate = true
	if err := repo.SaveFeedSettings(ctx, actor, settings, now); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := repo.ClaimFeedRefresh(ctx, now.Add(23*time.Hour)); err != nil || ok {
		t.Fatal("daily update early")
	}
	daily, ok, err := repo.ClaimFeedRefresh(ctx, now.Add(25*time.Hour))
	if err != nil || !ok || daily.Owner != actor {
		t.Fatal("daily update absent", err)
	}
	if err := repo.SaveFeedSettings(ctx, actor, domain.FeedSettings{Instructions: "disabled"}, now); err != nil {
		t.Fatal(err)
	}
}
