package dockerterminal

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	contractresource "github.com/opensoha/soha-contracts/resource"
	docker "github.com/opensoha/soha/internal/domain/docker"
	"github.com/opensoha/soha/internal/infrastructure/config"
	dbstore "github.com/opensoha/soha/internal/infrastructure/db"
	"github.com/opensoha/soha/internal/platform/apperrors"
	testpostgres "github.com/opensoha/soha/internal/testsupport/postgres"
	"go.uber.org/zap"
)

func terminalPostgres(t *testing.T) (*Repository, func() *Repository) {
	t.Helper()
	value := os.Getenv("SOHA_CATALOG_TEST_POSTGRES_PORT")
	if value == "" {
		t.Skip("requires an explicitly isolated PostgreSQL test port")
	}
	port, err := strconv.Atoi(value)
	if err != nil {
		t.Fatal(err)
	}
	cfg := testpostgres.Configure(t, config.DatabaseConfig{Driver: "postgres", Host: "127.0.0.1", Port: port, Name: "postgres", User: "pgsql", Password: "test-only", SSLMode: "disable", MaxOpenConns: 16, MaxIdleConns: 4})
	admin, err := dbstore.New(cfg, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	cfg.Name = "docker_terminal_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err = admin.DB().Exec(`CREATE DATABASE "` + cfg.Name + `"`).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := admin.DB().Exec(`DROP DATABASE "` + cfg.Name + `" WITH (FORCE)`).Error; err != nil {
			t.Error(err)
		}
	})
	store, err := dbstore.New(cfg, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	if err = store.MigrateFromFile(ctx, filepath.Join("..", "..", "..", "migrations", "postgres")); err != nil {
		t.Fatal(err)
	}
	keys := codecRepository(t).keys
	return New(store.DB(), keys), func() *Repository {
		other, err := dbstore.New(cfg, zap.NewNop())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = other.Close() })
		return New(other.DB(), keys)
	}
}

func TestDockerNativeTerminalLeaseWithPostgres(t *testing.T) {
	repository, reopen := terminalPostgres(t)
	ctx := t.Context()
	record := codecRecord()
	if err := repository.Create(ctx, record); err != nil {
		t.Fatal(err)
	}
	t.Run("actor_and_credential_binding", func(t *testing.T) { dockerNativeTerminalLeaseWithPostgresPhase1(t, &repository, &ctx, &record) })
	t.Run("concurrent_ticket_once", func(t *testing.T) { dockerNativeTerminalLeaseWithPostgresPhase2(t, &repository, &ctx, &record) })
	t.Run("reopened_encrypted_authorization", func(t *testing.T) { dockerNativeTerminalLeaseWithPostgresPhase3(t, &reopen, &ctx, &record) })
	t.Run("close_active_then_terminal_cas", func(t *testing.T) { dockerNativeTerminalLeaseWithPostgresPhase4(t, &repository, &ctx, &record) })
	t.Run("close_pending_consumes_ticket", func(t *testing.T) { dockerNativeTerminalLeaseWithPostgresPhase5(t, &repository, &ctx) })
	t.Run("expired_pending_never_consumed", func(t *testing.T) { dockerNativeTerminalLeaseWithPostgresPhase6(t, &repository, &ctx) })
	t.Run("bounded_admission", func(t *testing.T) { dockerNativeTerminalLeaseWithPostgresPhase7(t, &repository, &ctx) })
	t.Run("crashed_active_unknown_and_retention", func(t *testing.T) { dockerNativeTerminalLeaseWithPostgresPhase8(t, &repository, &ctx, &record) })
	t.Run("global_admission", func(t *testing.T) { dockerNativeTerminalLeaseWithPostgresPhase9(t, &repository, &ctx) })
}

func dockerNativeTerminalLeaseWithPostgresPhase1(t *testing.T, sharedRepository **Repository, sharedCtx *context.Context, sharedRecord *docker.NativeTerminalRecord) {

	if _, err := (*sharedRepository).Get((*sharedCtx), "other", (*sharedRecord).Lease.ID); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("actor=%v", err)
	}
	for _, binding := range [][2]string{{"wrong", (*sharedRecord).TicketHash}, {(*sharedRecord).CredentialHash, "wrong"}} {
		if _, err := (*sharedRepository).Consume((*sharedCtx), (*sharedRecord).ActorID, (*sharedRecord).Lease.ID, binding[0], binding[1]); !errors.Is(err, apperrors.ErrConflict) {
			t.Fatalf("binding=%v", err)
		}
	}
}

func dockerNativeTerminalLeaseWithPostgresPhase2(t *testing.T, sharedRepository **Repository, sharedCtx *context.Context, sharedRecord *docker.NativeTerminalRecord) {

	var winners atomic.Int64
	var workers sync.WaitGroup
	for range 12 {
		workers.Go(func() {
			item, err := (*sharedRepository).Consume((*sharedCtx), (*sharedRecord).ActorID, (*sharedRecord).Lease.ID, (*sharedRecord).CredentialHash, (*sharedRecord).TicketHash)
			if err == nil {
				winners.Add(1)
				if item.Lease.Status != "active" || item.TicketHash != "" {
					t.Error("ticket not erased")
				}
			} else if !errors.Is(err, apperrors.ErrConflict) {
				t.Error(err)
			}
		})
	}
	workers.Wait()
	if winners.Load() != 1 {
		t.Fatalf("winners=%d", winners.Load())
	}
}

func dockerNativeTerminalLeaseWithPostgresPhase3(t *testing.T, sharedReopen *func() *Repository, sharedCtx *context.Context, sharedRecord *docker.NativeTerminalRecord) {

	item, err := (*sharedReopen)().Get((*sharedCtx), (*sharedRecord).ActorID, (*sharedRecord).Lease.ID)
	if err != nil || item.Access.AuthzVersion != 7 || item.Principal.AccessTokenID != "private-token-id" || item.Lease.Target.ContainerID != (*sharedRecord).Input.ExpectedContainerID {
		t.Fatalf("reopen=%+v err=%v", item, err)
	}
}

func dockerNativeTerminalLeaseWithPostgresPhase4(t *testing.T, sharedRepository **Repository, sharedCtx *context.Context, sharedRecord *docker.NativeTerminalRecord) {

	item, err := (*sharedRepository).RequestClose((*sharedCtx), (*sharedRecord).ActorID, (*sharedRecord).Lease.ID)
	if err != nil || item.Lease.Status != "closing" || item.Lease.ClosedAt != nil {
		t.Fatalf("close acceptance=%+v err=%v", item.Lease, err)
	}
	now := time.Now().UTC()
	item.Lease.Status, item.Lease.ClosedAt = "closed", &now
	item.Lease.Result = &contractresource.PodTerminalResult{Status: "canceled", ReasonCode: "request_canceled"}
	if err = (*sharedRepository).Finish((*sharedCtx), item); err != nil {
		t.Fatal(err)
	}
	if err = (*sharedRepository).Finish((*sharedCtx), item); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("late finish=%v", err)
	}
}

func dockerNativeTerminalLeaseWithPostgresPhase5(t *testing.T, sharedRepository **Repository, sharedCtx *context.Context) {

	item := codecRecord()
	if err := (*sharedRepository).Create((*sharedCtx), item); err != nil {
		t.Fatal(err)
	}
	closed, err := (*sharedRepository).RequestClose((*sharedCtx), item.ActorID, item.Lease.ID)
	if err != nil || closed.Lease.Status != "closed" || closed.Lease.ClosedAt == nil {
		t.Fatalf("pending close=%+v err=%v", closed.Lease, err)
	}
	if _, err = (*sharedRepository).Consume((*sharedCtx), item.ActorID, item.Lease.ID, item.CredentialHash, item.TicketHash); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("closed ticket consumed")
	}
}

func dockerNativeTerminalLeaseWithPostgresPhase6(t *testing.T, sharedRepository **Repository, sharedCtx *context.Context) {

	item := codecRecord()
	item.Lease.TicketExpiresAt = time.Now().Add(-time.Second)
	row, err := (*sharedRepository).encode(item)
	if err != nil {
		t.Fatal(err)
	}
	if err = (*sharedRepository).db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	if _, err = (*sharedRepository).Consume((*sharedCtx), item.ActorID, item.Lease.ID, item.CredentialHash, item.TicketHash); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("expired ticket consumed")
	}
	if err = (*sharedRepository).Cleanup((*sharedCtx)); err != nil {
		t.Fatal(err)
	}
	got, err := (*sharedRepository).Get((*sharedCtx), item.ActorID, item.Lease.ID)
	if err != nil || got.Lease.Status != "expired" || got.Lease.Result != nil {
		t.Fatalf("expired=%+v err=%v", got.Lease, err)
	}
}

func dockerNativeTerminalLeaseWithPostgresPhase7(t *testing.T, sharedRepository **Repository, sharedCtx *context.Context) {

	items := make([]string, 0, 4)
	for range 4 {
		item := codecRecord()
		item.ActorID, item.Principal.UserID = "capacity", "capacity"
		if err := (*sharedRepository).Create((*sharedCtx), item); err != nil {
			t.Fatal(err)
		}
		items = append(items, item.Lease.ID)
	}
	extra := codecRecord()
	extra.ActorID, extra.Principal.UserID = "capacity", "capacity"
	if err := (*sharedRepository).Create((*sharedCtx), extra); !errors.Is(err, apperrors.ErrServiceUnavailable) {
		t.Fatalf("capacity=%v", err)
	}
	if _, err := (*sharedRepository).RequestClose((*sharedCtx), "capacity", items[0]); err != nil {
		t.Fatal(err)
	}
	if err := (*sharedRepository).Create((*sharedCtx), extra); err != nil {
		t.Fatalf("closed lease held capacity: %v", err)
	}
}

func dockerNativeTerminalLeaseWithPostgresPhase8(t *testing.T, sharedRepository **Repository, sharedCtx *context.Context, sharedRecord *docker.NativeTerminalRecord) {

	item := codecRecord()
	item.Lease.Status = "active"
	item.TicketHash = ""
	item.Lease.DeadlineAt = time.Now().Add(-time.Minute)
	row, err := (*sharedRepository).encode(item)
	if err != nil {
		t.Fatal(err)
	}
	if err = (*sharedRepository).db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	got, err := (*sharedRepository).Get((*sharedCtx), item.ActorID, item.Lease.ID)
	if err != nil || got.Lease.Status != "expired" || got.Lease.Result != nil {
		t.Fatalf("crash invented result: %+v err=%v", got.Lease, err)
	}
	if err = (*sharedRepository).Cleanup((*sharedCtx)); err != nil {
		t.Fatal(err)
	}
	if err = (*sharedRepository).db.Model(&leaseRow{}).Where("id = ?", item.Lease.ID).Update("closed_at", time.Now().Add(-25*time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	if err = (*sharedRepository).Cleanup((*sharedCtx)); err != nil {
		t.Fatal(err)
	}
	if _, err = (*sharedRepository).Get((*sharedCtx), item.ActorID, item.Lease.ID); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("retention=%v", err)
	}
	if _, err = (*sharedRepository).Consume((*sharedCtx), item.ActorID, item.Lease.ID, item.CredentialHash, (*sharedRecord).TicketHash); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("removed session replayed")
	}
}

func dockerNativeTerminalLeaseWithPostgresPhase9(t *testing.T, sharedRepository **Repository, sharedCtx *context.Context) {

	var count int64
	if err := (*sharedRepository).db.Model(&leaseRow{}).Where("deadline_at > now() AND (status IN ('active', 'closing') OR (status = 'pending' AND ticket_expires_at > now()))").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	for count < 64 {
		item := codecRecord()
		item.ActorID = uuid.NewString()
		item.Principal.UserID = item.ActorID
		if err := (*sharedRepository).Create((*sharedCtx), item); err != nil {
			t.Fatal(err)
		}
		count++
	}
	extra := codecRecord()
	extra.ActorID = uuid.NewString()
	extra.Principal.UserID = extra.ActorID
	if err := (*sharedRepository).Create((*sharedCtx), extra); !errors.Is(err, apperrors.ErrServiceUnavailable) {
		t.Fatalf("global capacity=%v", err)
	}
}
