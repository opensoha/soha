package plugin_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	domaincompanion "github.com/opensoha/soha/internal/domain/companion"
	domainplugin "github.com/opensoha/soha/internal/domain/plugin"
	"github.com/opensoha/soha/internal/infrastructure/config"
	dbstore "github.com/opensoha/soha/internal/infrastructure/db"
	"github.com/opensoha/soha/internal/platform/apperrors"
	repocompanion "github.com/opensoha/soha/internal/repository/companion"
	repoplugin "github.com/opensoha/soha/internal/repository/plugin"
	"go.uber.org/zap"
)

func TestPluginRemovalRollsBackCompanionRetirementWithPostgres(t *testing.T) {
	portText := os.Getenv("SOHA_CATALOG_TEST_POSTGRES_PORT")
	if portText == "" {
		t.Skip("requires an isolated PostgreSQL instance")
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	store, err := dbstore.New(config.DatabaseConfig{
		Driver: "postgres", Host: "127.0.0.1", Port: port, Name: "soha", User: "pgsql",
		Password: "test-only", SSLMode: "disable",
	}, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := t.Context()
	if err := store.MigrateFromFile(ctx, filepath.Join("..", "..", "..", "migrations", "postgres")); err != nil {
		t.Fatal(err)
	}
	plugins := repoplugin.New(store.DB())
	companions := repocompanion.New(store.DB())
	id := uuid.NewString()
	now := time.Now().UTC()
	t.Cleanup(func() {
		_ = store.DB().Exec("DELETE FROM companion_artifacts WHERE plugin_id = ?", id).Error
		_ = store.DB().Exec("DELETE FROM installed_plugins WHERE id = ?", id).Error
	})
	if _, err := plugins.UpsertInstalled(ctx, domainplugin.InstalledPlugin{
		ID: id, Name: "test pack", Version: "1.0.0", Type: "companion-pack",
		Status: "enabled", InstalledAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := companions.PutArtifact(ctx, domaincompanion.Artifact{
		PluginID: id, Version: "1.0.0", SizeBytes: 1, Active: true, InstalledAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	retire := func(ctx context.Context, item domainplugin.InstalledPlugin) error {
		return companions.RetireArtifacts(ctx, item.ID, now)
	}
	interrupted := errors.New("retirement interrupted after write")
	if _, err := plugins.RemoveInstalled(ctx, id, func(ctx context.Context, item domainplugin.InstalledPlugin) error {
		if err := retire(ctx, item); err != nil {
			return err
		}
		return interrupted
	}); !errors.Is(err, interrupted) {
		t.Fatalf("interrupted removal = %v", err)
	}
	if _, err := plugins.GetInstalled(ctx, id); err != nil {
		t.Fatalf("installation must survive rollback: %v", err)
	}
	if _, err := companions.GetActiveArtifact(ctx, id); err != nil {
		t.Fatalf("active artifact must survive rollback: %v", err)
	}
	if _, err := plugins.RemoveInstalled(ctx, id, retire); err != nil {
		t.Fatalf("retry removal: %v", err)
	}
	if _, err := plugins.GetInstalled(ctx, id); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("installation after retry = %v", err)
	}
	var active, retired bool
	err = store.DB().WithContext(ctx).Raw(
		"SELECT active, retired_at IS NOT NULL FROM companion_artifacts WHERE plugin_id = ? AND version = ?",
		id, "1.0.0",
	).Row().Scan(&active, &retired)
	if err != nil || active || !retired {
		t.Fatalf("artifact after retry: active=%v retired=%v err=%v", active, retired, err)
	}
}
