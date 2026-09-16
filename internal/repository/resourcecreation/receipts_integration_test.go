package resourcecreation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	domain "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/infrastructure/config"
	dbstore "github.com/opensoha/soha/internal/infrastructure/db"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"go.uber.org/zap"
)

func TestCreationReceiptPersistenceWithPostgres(t *testing.T) {
	portText := os.Getenv("SOHA_CATALOG_TEST_POSTGRES_PORT")
	if portText == "" {
		t.Skip("set SOHA_CATALOG_TEST_POSTGRES_PORT to an isolated PostgreSQL instance")
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.DatabaseConfig{Driver: "postgres", Host: "127.0.0.1", Port: port, Name: "postgres", User: "pgsql", Password: "test-only", SSLMode: "disable", MaxOpenConns: 12, MaxIdleConns: 4}
	admin, err := dbstore.New(cfg, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	cfg.Name = "creation_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := admin.DB().Exec(`CREATE DATABASE "` + cfg.Name + `"`).Error; err != nil {
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
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := store.MigrateFromFile(ctx, filepath.Join("..", "..", "..", "migrations", "postgres")); err != nil {
		t.Fatal(err)
	}
	repo := New(store.DB())
	id := claimCreationReceiptConcurrently(t, ctx, repo)
	document := createDocuments()[0]
	document.Status, document.Error, document.Resource.UID = "succeeded", "", "original-uid"
	if err := repo.UpdateDocument(ctx, id, document); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Complete(ctx, id, domain.ResourceCreateBatchFailed); err != nil {
		t.Fatal(err)
	}
	// Read from a separately opened connection, not an in-memory result.
	reopened, err := dbstore.New(cfg, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reopened.Close() }()
	other := New(reopened.DB())
	receipt, err := other.GetByIdentity(ctx, "actor", "cluster", "request")
	if err != nil || receipt.ID != id || receipt.Documents[0].Resource.UID != "original-uid" || receipt.Documents[1].Resource.UID != "" {
		t.Fatalf("persisted receipt: %+v %v", receipt, err)
	}
	if err := other.UpdateDocument(ctx, id, document); err != nil {
		t.Fatalf("same callback: %v", err)
	}
	document.Resource.UID = "replacement-uid"
	if err := other.UpdateDocument(ctx, id, document); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("original UID was overwritten: %v", err)
	}
	if _, err := other.GetByIdentity(ctx, "another-actor", "cluster", "request"); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("actor identity escaped: %v", err)
	}
}

func claimCreationReceiptConcurrently(t *testing.T, ctx context.Context, repo *Repository) string {
	t.Helper()
	results := make(chan domain.ResourceCreateBatchClaim, 8)
	failures := make(chan error, 8)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			claim, err := repo.Claim(ctx, "actor", "cluster", "request", strings.Repeat("a", 64), createDocuments())
			if err != nil {
				failures <- err
			} else {
				results <- claim
			}
		}()
	}
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	created, id := 0, ""
	for claim := range results {
		if claim.Created {
			created++
		}
		if id != "" && id != claim.Batch.ID {
			t.Fatal("duplicate batch")
		}
		id = claim.Batch.ID
	}
	if created != 1 {
		t.Fatalf("created %d batches", created)
	}
	return id
}
