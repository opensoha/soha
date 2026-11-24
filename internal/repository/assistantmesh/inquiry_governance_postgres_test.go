package assistantmesh_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	domain "github.com/opensoha/soha/internal/domain/assistantmesh"
	dbstore "github.com/opensoha/soha/internal/infrastructure/db"
	"github.com/opensoha/soha/internal/platform/apperrors"
	mesh "github.com/opensoha/soha/internal/repository/assistantmesh"
	userrepo "github.com/opensoha/soha/internal/repository/user"
)

func TestConfiguredInquiryClosureWithPostgres(t *testing.T) {
	store := receptionPostgres(t)
	repo := mesh.New(store.DB())
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Microsecond)
	owner, caller, workerCaller, overdueCaller := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	for _, id := range []string{owner, caller, workerCaller, overdueCaller} {
		mustUser(t, ctx, store, id, "active")
	}
	if err := repo.PutProfile(ctx, domain.Profile{OwnerID: owner, Open: true}, now, 24*time.Hour); err != nil {
		t.Fatal(err)
	}
	userClosed := pendingInquiry(caller, owner, "configured-invalidation", "one", now, now.Add(time.Hour))
	expired := pendingInquiry(workerCaller, owner, "configured-worker", "two", now, now.Add(time.Hour))
	overdue := pendingInquiry(overdueCaller, owner, "already-overdue", "three", now, now.Add(time.Hour))
	for _, item := range []domain.Inquiry{userClosed, expired, overdue} {
		if _, err := repo.CreateInquiry(ctx, item, generousLimits()); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Exec(ctx, `INSERT INTO app_settings (setting_key, category, value)
		VALUES ('assistantMesh.organization', 'ai', '{"inquiryRetentionDays":14}'::json)`); err != nil {
		t.Fatal(err)
	}
	if err := store.Exec(ctx, `UPDATE users SET status='disabled' WHERE id=$1`, caller); err != nil {
		t.Fatal(err)
	}
	closed, err := repo.GetInquiry(ctx, caller, userClosed.RequestID)
	if err != nil || closed.Status != domain.InquiryInvalidated || closed.PurgeAfter == nil || closed.ClosedAt == nil ||
		closed.PurgeAfter.Sub(*closed.ClosedAt) != 14*24*time.Hour {
		t.Fatalf("access-loss closure did not use current retention: %+v, %v", closed, err)
	}
	due := now.Add(-time.Minute)
	if err := store.Exec(ctx, `UPDATE assistant_mesh_inquiries SET due_at=$1 WHERE id=$2`, due, overdue.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.Exec(ctx, `UPDATE app_settings SET value='{"inquiryRetentionDays":3}' WHERE setting_key='assistantMesh.organization'`); err != nil {
		t.Fatal(err)
	}
	frozen, err := repo.GetInquiry(ctx, overdueCaller, overdue.RequestID)
	if err != nil || frozen.PurgeAfter == nil || !frozen.PurgeAfter.Equal(due.Add(14*24*time.Hour)) {
		t.Fatalf("changing duration restamped an overdue inquiry: %+v, %v", frozen, err)
	}
	checkConfiguredWorkerExpiry(t, ctx, store, repo, expired, userClosed, *closed.PurgeAfter, due)
	checkConfiguredInquiryPurge(t, ctx, store, repo, expired, due)
}

func checkConfiguredWorkerExpiry(t *testing.T, ctx context.Context, store *dbstore.Store, repo *mesh.Repository, expired, userClosed domain.Inquiry, closedPurgeAfter, due time.Time) {
	if err := store.Exec(ctx, `UPDATE assistant_mesh_inquiries
		SET due_at=$1, citations='[{"assetId":"published-id","version":3,"title":"private excerpt"}]' WHERE id=$2`, due, expired.ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.CleanupWithInquiryPolicy(ctx, domain.InquiryLimits{Retention: 14 * 24 * time.Hour, OwnerPending: 5}); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetInquiry(ctx, expired.CallerID, expired.RequestID)
	if err != nil || got.Status != domain.InquiryExpired || got.PurgeAfter == nil || !got.PurgeAfter.Equal(due.Add(3*24*time.Hour)) {
		t.Fatalf("worker expiry did not use current retention: %+v, %v", got, err)
	}
	old, err := repo.GetInquiry(ctx, userClosed.CallerID, userClosed.RequestID)
	if err != nil || old.PurgeAfter == nil || !old.PurgeAfter.Equal(closedPurgeAfter) {
		t.Fatalf("retention change rewrote a terminal deadline: %+v, %v", old, err)
	}
}

func checkConfiguredInquiryPurge(t *testing.T, ctx context.Context, store *dbstore.Store, repo *mesh.Repository, expired domain.Inquiry, due time.Time) {
	if err := store.Exec(ctx, `UPDATE assistant_mesh_inquiries SET purge_after=$1, answer='retained answer' WHERE id=$2`, due, expired.ID); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := repo.CleanupWithInquiryPolicy(ctx, domain.InquiryLimits{Retention: 3 * 24 * time.Hour, OwnerPending: 5}); err != nil {
			t.Fatal(err)
		}
	}
	purged, err := repo.GetInquiry(ctx, expired.CallerID, expired.RequestID)
	if err != nil || purged.Question != "" || purged.Answer != "" || purged.Status != domain.InquiryExpired ||
		purged.OwnerID != expired.OwnerID || !purged.DueAt.Equal(due) {
		t.Fatalf("purge did not erase only the retained content: %+v, %v", purged, err)
	}
	var versions string
	if err := store.DB().Raw(`SELECT citation_versions::text FROM assistant_mesh_inquiries WHERE id=?`, expired.ID).Row().Scan(&versions); err != nil ||
		versions != `[{"assetId": "published-id", "version": 3}]` || len(purged.Citations) != 0 {
		t.Fatalf("purge did not retain exactly the citation identifiers: %q, %v", versions, err)
	}
}

func TestInquiryAuditReadLockAndOwnerDeleteGuardWithPostgres(t *testing.T) {
	store := receptionPostgres(t)
	repo := mesh.New(store.DB())
	ctx := t.Context()
	owner, caller := uuid.NewString(), uuid.NewString()
	mustUser(t, ctx, store, owner, "active")
	mustUser(t, ctx, store, caller, "active")
	now := time.Now().UTC()
	if err := repo.PutProfile(ctx, domain.Profile{OwnerID: owner, Open: true}, now, 24*time.Hour); err != nil {
		t.Fatal(err)
	}
	inq := pendingInquiry(caller, owner, "audit", "audit", now, now.Add(time.Hour))
	if _, err := repo.CreateInquiry(ctx, inq, generousLimits()); err != nil {
		t.Fatal(err)
	}
	refused := errors.New("fixture audit recorder refused")
	if err := repo.ReadInquiryForAudit(ctx, inq.ID, func(_ context.Context, got domain.Inquiry) error {
		if got.Question != inq.Question || got.ID != inq.ID {
			t.Fatalf("wrong inquiry loaded: %+v", got)
		}
		return refused
	}); !errors.Is(err, refused) {
		t.Fatalf("audit failure did not abort transaction: %v", err)
	}
	if err := repo.Create(ctx, mesh.Record{
		ID: uuid.NewString(), OwnerID: owner, Kind: domain.KindExperienceCard,
		Payload: []byte(`{"published":{"version":1,"input":{"kind":"experience_card","title":"fixture"}}}`),
	}); err != nil {
		t.Fatal(err)
	}
	if err := userrepo.New(store.DB()).DeleteUser(ctx, owner); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("user hard deletion could remove inheritable assets: %v", err)
	}
}
