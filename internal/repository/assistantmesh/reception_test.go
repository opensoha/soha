package assistantmesh_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	domain "github.com/opensoha/soha/internal/domain/assistantmesh"
	"github.com/opensoha/soha/internal/infrastructure/config"
	dbstore "github.com/opensoha/soha/internal/infrastructure/db"
	"github.com/opensoha/soha/internal/platform/apperrors"
	reception "github.com/opensoha/soha/internal/repository/assistantmesh"
	testpostgres "github.com/opensoha/soha/internal/testsupport/postgres"
	"go.uber.org/zap"
)

func TestReceptionWithPostgres(t *testing.T) {
	store := receptionPostgres(t)
	repo := reception.New(store.DB())
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()

	t.Run("profile_close_invalidates_and_reopen_does_not_resurrect", func(t *testing.T) {
		owner, caller := uuid.NewString(), uuid.NewString()
		mustUser(t, ctx, store, owner, "active")
		mustUser(t, ctx, store, caller, "active")
		now := time.Date(2026, 11, 21, 8, 0, 0, 0, time.UTC)

		missing, err := repo.GetProfile(ctx, owner)
		if err != nil || missing.OwnerID != owner || missing.Open {
			t.Fatalf("absent profile = %#v, %v", missing, err)
		}
		if err := repo.PutProfile(ctx, domain.Profile{OwnerID: owner, Open: true, Intro: "here"}, now, 0); err != nil {
			t.Fatal(err)
		}
		if got, err := repo.GetProfile(ctx, owner); err != nil || !got.Open || got.Intro != "here" {
			t.Fatalf("open profile = %#v, %v", got, err)
		}
		list, err := repo.ListProfiles(ctx, "", 200)
		if err != nil || !containsProfile(list, owner) {
			t.Fatalf("ListProfiles = %#v, %v", list, err)
		}
		if list, err := repo.ListProfiles(ctx, "ffffffff-ffff-4fff-bfff-ffffffffffff", 5); err != nil || len(list) != 0 {
			t.Fatalf("ListProfiles(after max) = %#v, %v", list, err)
		}

		created := pendingInquiry(caller, owner, "close-request", "close-key", now, now.Add(2*time.Hour))
		if _, err := repo.CreateInquiry(ctx, created, generousLimits()); err != nil {
			t.Fatal(err)
		}
		if err := repo.PutProfile(ctx, domain.Profile{OwnerID: owner, Open: false}, now.Add(time.Hour), 24*time.Hour); err != nil {
			t.Fatal(err)
		}
		got, err := repo.GetInquiry(ctx, caller, created.RequestID)
		if err != nil || got.Status != domain.InquiryInvalidated || got.ClosedAt == nil || got.PurgeAfter == nil {
			t.Fatalf("closed inquiry = %#v, %v", got, err)
		}
		if err := repo.PutProfile(ctx, domain.Profile{OwnerID: owner, Open: true}, now.Add(2*time.Hour), 24*time.Hour); err != nil {
			t.Fatal(err)
		}
		got, err = repo.GetInquiry(ctx, caller, created.RequestID)
		if err != nil || got.Status != domain.InquiryInvalidated {
			t.Fatalf("reopened inquiry = %#v, %v", got, err)
		}
	})

	t.Run("idempotent_create_list_and_one_time_reply", func(t *testing.T) {
		owner, caller, other := uuid.NewString(), uuid.NewString(), uuid.NewString()
		for _, id := range []string{owner, caller, other} {
			mustUser(t, ctx, store, id, "active")
		}
		now := time.Date(2026, 11, 22, 8, 0, 0, 0, time.UTC)
		if err := repo.PutProfile(ctx, domain.Profile{OwnerID: owner, Open: true}, now, 0); err != nil {
			t.Fatal(err)
		}
		item := pendingInquiry(caller, owner, "request-1", "question-key", now, now.Add(time.Hour))
		item.Citations = []domain.Citation{{AssetID: "asset", Version: 1, Title: "source", ObservedAt: now}}
		created, err := repo.CreateInquiry(ctx, item, generousLimits())
		if err != nil || created.ID != item.ID || len(created.Citations) != 1 {
			t.Fatalf("CreateInquiry = %#v, %v", created, err)
		}
		duplicate := item
		duplicate.ID = uuid.NewString()
		duplicate.Question = "retry body should not replace the receipt"
		replayed, err := repo.CreateInquiry(ctx, duplicate, generousLimits())
		if err != nil || replayed.ID != item.ID || replayed.Question != item.Question {
			t.Fatalf("duplicate = %#v, %v", replayed, err)
		}
		changedKey := duplicate
		changedKey.QuestionKey = "other-key"
		if _, err := repo.CreateInquiry(ctx, changedKey, generousLimits()); !errors.Is(err, apperrors.ErrConflict) {
			t.Fatalf("duplicate key error = %v", err)
		}
		changedOwner := duplicate
		changedOwner.OwnerID = other
		if _, err := repo.CreateInquiry(ctx, changedOwner, generousLimits()); !errors.Is(err, apperrors.ErrConflict) {
			t.Fatalf("duplicate owner error = %v", err)
		}

		if _, err := repo.ReplyInquiry(ctx, other, item.ID, "not mine", now.Add(time.Minute), time.Hour); !errors.Is(err, apperrors.ErrNotFound) {
			t.Fatalf("foreign reply error = %v", err)
		}
		answered, err := repo.ReplyInquiry(ctx, owner, item.ID, "answer", now.Add(time.Minute), time.Hour)
		if err != nil || answered.Status != domain.InquiryReplied || answered.Answer != "answer" || answered.ClosedAt == nil || answered.PurgeAfter == nil {
			t.Fatalf("ReplyInquiry = %#v, %v", answered, err)
		}
		if _, err := repo.ReplyInquiry(ctx, owner, item.ID, "again", now.Add(2*time.Minute), time.Hour); !errors.Is(err, apperrors.ErrNotFound) {
			t.Fatalf("second reply error = %v", err)
		}

		inbox, err := repo.ListInquiries(ctx, owner, true, now.Add(2*time.Hour), time.Hour)
		if err != nil || len(inbox) != 1 || inbox[0].Question != "" || inbox[0].Answer != "" || len(inbox[0].Citations) != 0 {
			t.Fatalf("purged inbox = %#v, %v", inbox, err)
		}
		outbox, err := repo.ListInquiries(ctx, caller, false, now.Add(2*time.Hour), time.Hour)
		if err != nil || len(outbox) != 1 || outbox[0].ID != item.ID || outbox[0].Status != domain.InquiryReplied {
			t.Fatalf("outbox = %#v, %v", outbox, err)
		}
	})

	t.Run("merge_capacity_rate_and_stale_expiry", func(t *testing.T) {
		now := time.Date(2026, 11, 23, 8, 0, 0, 0, time.UTC)
		seedPair := func() (string, string) {
			owner, caller := uuid.NewString(), uuid.NewString()
			mustUser(t, ctx, store, owner, "active")
			mustUser(t, ctx, store, caller, "active")
			if err := repo.PutProfile(ctx, domain.Profile{OwnerID: owner, Open: true}, now, 0); err != nil {
				t.Fatal(err)
			}
			return owner, caller
		}

		owner, caller := seedPair()
		limits := generousLimits()
		limits.MergeWindow = 10 * time.Minute
		first := pendingInquiry(caller, owner, "merge-1", "same-question", now, now.Add(time.Hour))
		created, err := repo.CreateInquiry(ctx, first, limits)
		if err != nil {
			t.Fatal(err)
		}
		second := pendingInquiry(caller, owner, "merge-2", "same-question", now.Add(time.Minute), now.Add(time.Hour))
		merged, err := repo.CreateInquiry(ctx, second, limits)
		if err != nil || merged.ID != created.ID {
			t.Fatalf("merge = %#v, %v", merged, err)
		}
		afterWindow := pendingInquiry(caller, owner, "merge-3", "same-question", now.Add(11*time.Minute), now.Add(time.Hour))
		if separate, err := repo.CreateInquiry(ctx, afterWindow, limits); err != nil || separate.ID == created.ID {
			t.Fatalf("outside the configured merge window = %#v, %v", separate, err)
		}

		owner, caller = seedPair()
		limits = generousLimits()
		limits.OwnerPending = 1
		if _, err := repo.CreateInquiry(ctx, pendingInquiry(caller, owner, "cap-1", "cap-key", now, now.Add(time.Hour)), limits); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.CreateInquiry(ctx, pendingInquiry(caller, owner, "cap-2", "cap-key-2", now.Add(time.Minute), now.Add(time.Hour)), limits); !errors.Is(err, apperrors.ErrConflict) {
			t.Fatalf("capacity error = %v", err)
		}

		owner, caller = seedPair()
		limits = generousLimits()
		limits.PairPerHour = 1
		if _, err := repo.CreateInquiry(ctx, pendingInquiry(caller, owner, "rate-1", "rate-key", now, now.Add(time.Hour)), limits); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.CreateInquiry(ctx, pendingInquiry(caller, owner, "rate-2", "rate-key-2", now.Add(time.Minute), now.Add(time.Hour)), limits); !errors.Is(err, domain.ErrInquiryRateLimited) {
			t.Fatalf("rate error = %v", err)
		}

		owner, caller = seedPair()
		otherOwner, _ := seedPair()
		limits = generousLimits()
		limits.CallerPerDay = 1
		if _, err := repo.CreateInquiry(ctx, pendingInquiry(caller, owner, "daily-first", "first", now, now.Add(time.Hour)), limits); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.CreateInquiry(ctx, pendingInquiry(caller, otherOwner, "daily-second", "second", now.Add(time.Minute), now.Add(time.Hour)), limits); !errors.Is(err, domain.ErrInquiryRateLimited) {
			t.Fatalf("cross-owner daily rate error = %v", err)
		}

		owner, caller = seedPair()
		stale := pendingInquiry(caller, owner, "stale-1", "stale-key", now, now.Add(30*time.Minute))
		if _, err := repo.CreateInquiry(ctx, stale, generousLimits()); err != nil {
			t.Fatal(err)
		}
		due := now.Add(-time.Minute)
		if err := store.DB().WithContext(ctx).Exec(`UPDATE assistant_mesh_inquiries SET due_at = ? WHERE id = ?`, due, stale.ID).Error; err != nil {
			t.Fatal(err)
		}
		list, err := repo.ListInquiries(ctx, owner, true, now, time.Hour)
		if err != nil || len(list) != 1 || list[0].Status != domain.InquiryExpired ||
			list[0].ClosedAt == nil || !list[0].ClosedAt.Equal(due) ||
			list[0].PurgeAfter == nil || !list[0].PurgeAfter.Equal(due.Add(time.Hour)) {
			t.Fatalf("stale inbox = %#v, %v", list, err)
		}
	})

	t.Run("closed_or_inactive_participants_cannot_receive", func(t *testing.T) {
		now := time.Date(2026, 11, 24, 8, 0, 0, 0, time.UTC)
		owner, caller := uuid.NewString(), uuid.NewString()
		mustUser(t, ctx, store, owner, "active")
		mustUser(t, ctx, store, caller, "active")
		if err := repo.PutProfile(ctx, domain.Profile{OwnerID: owner, Open: false}, now, 0); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.CreateInquiry(ctx, pendingInquiry(caller, owner, "closed-1", "closed-key", now, now.Add(time.Hour)), generousLimits()); !errors.Is(err, apperrors.ErrConflict) {
			t.Fatalf("closed profile error = %v", err)
		}

		owner, caller = uuid.NewString(), uuid.NewString()
		mustUser(t, ctx, store, owner, "active")
		mustUser(t, ctx, store, caller, "disabled")
		if err := repo.PutProfile(ctx, domain.Profile{OwnerID: owner, Open: true}, now, 0); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.CreateInquiry(ctx, pendingInquiry(caller, owner, "inactive-1", "inactive-key", now, now.Add(time.Hour)), generousLimits()); !errors.Is(err, apperrors.ErrConflict) {
			t.Fatalf("inactive caller error = %v", err)
		}
	})

	t.Run("merged_request_alias_is_a_durable_receipt", func(t *testing.T) {
		now := time.Date(2026, 11, 25, 8, 0, 0, 0, time.UTC)
		owner, caller, other := uuid.NewString(), uuid.NewString(), uuid.NewString()
		for _, id := range []string{owner, caller, other} {
			mustUser(t, ctx, store, id, "active")
		}
		if err := repo.PutProfile(ctx, domain.Profile{OwnerID: owner, Open: true}, now, 0); err != nil {
			t.Fatal(err)
		}
		limits := generousLimits()
		limits.MergeWindow = 24 * time.Hour
		first := pendingInquiry(caller, owner, "alias-1", "alias-key", now, now.Add(time.Hour))
		created, err := repo.CreateInquiry(ctx, first, limits)
		if err != nil {
			t.Fatal(err)
		}
		second := pendingInquiry(caller, owner, "alias-2", "alias-key", now.Add(time.Minute), now.Add(time.Hour))
		merged, err := repo.CreateInquiry(ctx, second, limits)
		if err != nil || merged.ID != created.ID {
			t.Fatalf("merged alias = %#v, %v", merged, err)
		}
		if got, err := repo.GetInquiry(ctx, caller, "alias-2"); err != nil || got.ID != created.ID || got.Question != first.Question {
			t.Fatalf("alias lookup = %#v, %v", got, err)
		}
		if replay, err := repo.CreateInquiry(ctx, second, limits); err != nil || replay.ID != created.ID || replay.Question != first.Question {
			t.Fatalf("alias replay = %#v, %v", replay, err)
		}
		changedKey := second
		changedKey.QuestionKey = "other-key"
		if _, err := repo.CreateInquiry(ctx, changedKey, limits); !errors.Is(err, apperrors.ErrConflict) {
			t.Fatalf("alias changed key error = %v", err)
		}
		changedOwner := second
		changedOwner.OwnerID = other
		if _, err := repo.CreateInquiry(ctx, changedOwner, limits); !errors.Is(err, apperrors.ErrConflict) {
			t.Fatalf("alias changed owner error = %v", err)
		}
		if _, err := repo.ReplyInquiry(ctx, owner, created.ID, "reply", now.Add(2*time.Minute), time.Hour); err != nil {
			t.Fatal(err)
		}
		afterReply, err := repo.GetInquiry(ctx, caller, "alias-2")
		if err != nil || afterReply.ID != created.ID || afterReply.Status != domain.InquiryReplied {
			t.Fatalf("alias after reply = %#v, %v", afterReply, err)
		}
		if receipt, err := repo.CreateInquiry(ctx, second, limits); err != nil || receipt.ID != created.ID || receipt.Status != domain.InquiryReplied {
			t.Fatalf("alias retry after reply = %#v, %v", receipt, err)
		}

		expireOwner, expireCaller := uuid.NewString(), uuid.NewString()
		mustUser(t, ctx, store, expireOwner, "active")
		mustUser(t, ctx, store, expireCaller, "active")
		if err := repo.PutProfile(ctx, domain.Profile{OwnerID: expireOwner, Open: true}, now, 0); err != nil {
			t.Fatal(err)
		}
		expireFirst := pendingInquiry(expireCaller, expireOwner, "expire-alias-1", "expire-key", now, now.Add(time.Hour))
		expireCreated, err := repo.CreateInquiry(ctx, expireFirst, limits)
		if err != nil {
			t.Fatal(err)
		}
		expireSecond := pendingInquiry(expireCaller, expireOwner, "expire-alias-2", "expire-key", now.Add(time.Minute), now.Add(time.Hour))
		if merged, err := repo.CreateInquiry(ctx, expireSecond, limits); err != nil || merged.ID != expireCreated.ID {
			t.Fatalf("expire alias = %#v, %v", merged, err)
		}
		if err := store.DB().WithContext(ctx).Exec(`UPDATE assistant_mesh_inquiries SET due_at = ? WHERE id = ?`, now.Add(-time.Minute), expireCreated.ID).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := repo.ListInquiries(ctx, expireOwner, true, now, time.Hour); err != nil {
			t.Fatal(err)
		}
		expired, err := repo.GetInquiry(ctx, expireCaller, "expire-alias-2")
		if err != nil || expired.ID != expireCreated.ID || expired.Status != domain.InquiryExpired {
			t.Fatalf("alias after expiry = %#v, %v", expired, err)
		}
		if receipt, err := repo.CreateInquiry(ctx, expireSecond, limits); err != nil || receipt.ID != expireCreated.ID || receipt.Status != domain.InquiryExpired {
			t.Fatalf("alias retry after expiry = %#v, %v", receipt, err)
		}
	})

	t.Run("access_revocation_invalidates_and_reopen_cannot_resurrect", func(t *testing.T) {
		now := time.Date(2026, 11, 26, 8, 0, 0, 0, time.UTC)
		owner, caller := uuid.NewString(), uuid.NewString()
		mustUser(t, ctx, store, owner, "active")
		mustUser(t, ctx, store, caller, "active")
		if err := repo.PutProfile(ctx, domain.Profile{OwnerID: owner, Open: true}, now, 0); err != nil {
			t.Fatal(err)
		}
		item := pendingInquiry(caller, owner, "revoke-user-1", "revoke-key", now, now.Add(time.Hour))
		if _, err := repo.CreateInquiry(ctx, item, generousLimits()); err != nil {
			t.Fatal(err)
		}
		if err := store.Exec(ctx, `UPDATE users SET status = 'disabled' WHERE id = $1`, owner); err != nil {
			t.Fatal(err)
		}
		got, err := repo.GetInquiry(ctx, caller, item.RequestID)
		if err != nil || got.Status != domain.InquiryInvalidated || got.ClosedAt == nil || got.PurgeAfter == nil {
			t.Fatalf("disabled inquiry = %#v, %v", got, err)
		}
		if profile, err := repo.GetProfile(ctx, owner); err != nil || profile.Open {
			t.Fatalf("disabled profile = %#v, %v", profile, err)
		}
		if err := store.Exec(ctx, `UPDATE users SET status = 'active' WHERE id = $1`, owner); err != nil {
			t.Fatal(err)
		}
		got, err = repo.GetInquiry(ctx, caller, item.RequestID)
		if err != nil || got.Status != domain.InquiryInvalidated {
			t.Fatalf("reopened inquiry = %#v, %v", got, err)
		}
		if profile, err := repo.GetProfile(ctx, owner); err != nil || profile.Open {
			t.Fatalf("profile reopened automatically = %#v, %v", profile, err)
		}
	})

	t.Run("heya_revocation_invalidates_and_approval_cannot_resurrect", func(t *testing.T) {
		now := time.Date(2026, 11, 27, 8, 0, 0, 0, time.UTC)
		owner, caller := uuid.NewString(), uuid.NewString()
		mustUser(t, ctx, store, owner, "active")
		mustUser(t, ctx, store, caller, "active")
		if err := store.Exec(ctx, `UPDATE ai_personal_agent_access SET status = 'revoked' WHERE user_id = $1`, caller); err != nil {
			t.Fatal(err)
		}
		beforeAdmission := pendingInquiry(caller, owner, "revoked-before-admission", "revoked", now, now.Add(time.Hour))
		if _, err := repo.CreateInquiry(ctx, beforeAdmission, generousLimits()); !errors.Is(err, apperrors.ErrAccessDenied) {
			t.Fatalf("stale authorized request admitted after caller Heya revocation: %v", err)
		}
		if err := store.Exec(ctx, `UPDATE ai_personal_agent_access SET status = 'approved' WHERE user_id = $1`, caller); err != nil {
			t.Fatal(err)
		}
		if err := repo.PutProfile(ctx, domain.Profile{OwnerID: owner, Open: true}, now, 0); err != nil {
			t.Fatal(err)
		}
		item := pendingInquiry(caller, owner, "revoke-heya-1", "heya-key", now, now.Add(time.Hour))
		if _, err := repo.CreateInquiry(ctx, item, generousLimits()); err != nil {
			t.Fatal(err)
		}
		if err := store.Exec(ctx, `UPDATE ai_personal_agent_access SET status = 'revoked' WHERE user_id = $1`, owner); err != nil {
			t.Fatal(err)
		}
		got, err := repo.GetInquiry(ctx, caller, item.RequestID)
		if err != nil || got.Status != domain.InquiryInvalidated || got.ClosedAt == nil || got.PurgeAfter == nil {
			t.Fatalf("revoked inquiry = %#v, %v", got, err)
		}
		if profile, err := repo.GetProfile(ctx, owner); err != nil || profile.Open {
			t.Fatalf("revoked profile = %#v, %v", profile, err)
		}
		if err := store.Exec(ctx, `UPDATE ai_personal_agent_access SET status = 'approved' WHERE user_id = $1`, owner); err != nil {
			t.Fatal(err)
		}
		got, err = repo.GetInquiry(ctx, caller, item.RequestID)
		if err != nil || got.Status != domain.InquiryInvalidated {
			t.Fatalf("reapproved inquiry = %#v, %v", got, err)
		}
	})
	t.Run("automatic_transport_keeps_question_out_of_human_inbox", func(t *testing.T) {
		owner, caller := uuid.NewString(), uuid.NewString()
		mustUser(t, ctx, store, owner, "active")
		mustUser(t, ctx, store, caller, "active")
		now := time.Now().UTC()
		if err := repo.PutProfile(ctx, domain.Profile{OwnerID: owner, Open: true}, now, 0); err != nil {
			t.Fatal(err)
		}
		limits := generousLimits()
		limits.OwnerPending = 1
		pending := pendingInquiry(caller, owner, "pending-reception", "pending-reception", now, now.Add(time.Hour))
		if _, err := repo.CreateInquiry(ctx, pending, limits); err != nil {
			t.Fatal(err)
		}
		auto := pendingInquiry(caller, owner, "processing-then-answer", "automatic", now, now.Add(time.Hour))
		auto.Status = domain.InquiryProcessing
		if _, err := repo.CreateInquiry(ctx, auto, limits); err != nil {
			t.Fatalf("full human inbox prevented automatic match: %v", err)
		}
		if inbox, err := repo.ListInquiries(ctx, owner, true, now, time.Hour); err != nil || len(inbox) != 1 || inbox[0].ID != pending.ID {
			t.Fatalf("automatic question appeared in human inbox: %+v, %v", inbox, err)
		}
		if _, err := repo.ReplyInquiry(ctx, owner, auto.ID, "premature", now, time.Hour); !errors.Is(err, apperrors.ErrNotFound) {
			t.Fatalf("processing question could be replied to: %v", err)
		}
		auto.Status, auto.Answer = domain.InquiryAnswered, "grounded"
		auto.Citations = []domain.Citation{{AssetID: uuid.NewString(), Version: 1, Title: "Fact", ObservedAt: now}}
		purge := now.Add(time.Hour)
		auto.ClosedAt, auto.PurgeAfter = &now, &purge
		if err := repo.CompleteInquiry(ctx, auto); err != nil {
			t.Fatal(err)
		}
		if got, err := repo.GetInquiry(ctx, caller, auto.RequestID); err != nil || got.Status != domain.InquiryAnswered || got.Answer != auto.Answer {
			t.Fatalf("caller did not receive automatic answer: %+v, %v", got, err)
		}
		if inbox, err := repo.ListInquiries(ctx, owner, true, now, time.Hour); err != nil || len(inbox) != 1 || inbox[0].ID != pending.ID {
			t.Fatalf("resolved automatic question appeared in human inbox: %+v, %v", inbox, err)
		}
		if _, err := repo.ReplyInquiry(ctx, owner, pending.ID, "answered", now, time.Hour); err != nil {
			t.Fatal(err)
		}
		crashed := pendingInquiry(caller, owner, "processing-recovered", "recovered", now.Add(-2*time.Minute), now.Add(time.Hour))
		crashed.Status = domain.InquiryProcessing
		if _, err := repo.CreateInquiry(ctx, crashed, limits); err != nil {
			t.Fatal(err)
		}
		if err := repo.Cleanup(ctx); err != nil {
			t.Fatal(err)
		}
		if got, err := repo.GetInquiry(ctx, caller, crashed.RequestID); err != nil || got.Status != domain.InquiryPending {
			t.Fatalf("interrupted reception did not recover to pending: %+v, %v", got, err)
		}
	})
}

func receptionPostgres(t *testing.T) *dbstore.Store {
	t.Helper()
	portText := os.Getenv("SOHA_CATALOG_TEST_POSTGRES_PORT")
	if portText == "" {
		t.Skip("set SOHA_CATALOG_TEST_POSTGRES_PORT to an isolated PostgreSQL instance")
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	cfg := testpostgres.Configure(t, config.DatabaseConfig{
		Driver: "postgres", Host: "127.0.0.1", Port: port, Name: "postgres", User: "pgsql",
		Password: "test-only", SSLMode: "disable", MaxOpenConns: 8, MaxIdleConns: 4,
	})
	admin, err := dbstore.New(cfg, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	cfg.Name = "assistant_mesh_reception_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := admin.Exec(t.Context(), `CREATE DATABASE "`+cfg.Name+`"`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := admin.Exec(context.Background(), `DROP DATABASE "`+cfg.Name+`" WITH (FORCE)`); err != nil {
			t.Error(err)
		}
	})
	store, err := dbstore.New(cfg, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	dir := t.TempDir()
	for _, name := range []string{"0001_init.sql", "0113_heya_access.sql", "0154_assistant_mesh_assets.sql", "0155_assistant_mesh_reception.sql", "0157_assistant_mesh_delegations.sql", "0158_assistant_mesh_feishu.sql", "0159_assistant_mesh_inquiry_retention.sql", "0160_assistant_mesh_inheritance_delete_guard.sql", "0163_assistant_mesh_profile_defaults.sql"} {
		contents, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "postgres", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), contents, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.MigrateFromFile(t.Context(), dir); err != nil {
		t.Fatal(err)
	}
	return store
}

func mustUser(t *testing.T, ctx context.Context, store *dbstore.Store, id, status string) {
	t.Helper()
	username := "reception-" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := store.Exec(ctx, `INSERT INTO users(id, username, email, status) VALUES($1, $2, $3, $4)`,
		id, username, username+"@example.test", status); err != nil {
		t.Fatal(err)
	}
	if status == "active" {
		if err := store.Exec(ctx, `INSERT INTO ai_personal_agent_access (user_id, status, requested_at) VALUES ($1, 'approved', now())`, id); err != nil {
			t.Fatal(err)
		}
	}
}

func pendingInquiry(caller, owner, requestID, questionKey string, now, due time.Time) domain.Inquiry {
	return domain.Inquiry{
		ID: uuid.NewString(), CallerID: caller, OwnerID: owner, RequestID: requestID, QuestionKey: questionKey,
		Question: "bounded question", Status: domain.InquiryPending, CreatedAt: now, DueAt: due,
	}
}

func generousLimits() domain.InquiryLimits {
	return domain.InquiryLimits{PairPerHour: 100, CallerPerDay: 100, OwnerPending: 100, MergeWindow: 0, Retention: 24 * time.Hour}
}

func containsProfile(items []domain.Profile, ownerID string) bool {
	for _, item := range items {
		if item.OwnerID == ownerID && item.Open {
			return true
		}
	}
	return false
}
