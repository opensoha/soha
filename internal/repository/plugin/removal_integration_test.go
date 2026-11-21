package plugin_test

import (
	"context"
	"errors"
	"fmt"
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
		Password: "test-only", SSLMode: "disable", MaxOpenConns: 1,
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
	installed, err := plugins.UpsertInstalled(ctx, domainplugin.InstalledPlugin{
		ID: id, Name: "test pack", Version: "1.0.0", Type: "companion-pack",
		Status: "enabled", InstalledAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := companions.PutArtifact(ctx, domaincompanion.Artifact{
		PluginID: id, Version: "1.0.0", SizeBytes: 1, Active: true, InstalledAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	t.Run("artifact_update_rolls_back", func(t *testing.T) {
		updateCtx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		interrupted := errors.New("plugin update interrupted after artifact activation")
		_, err := plugins.UpdateInstalled(updateCtx, installed, installed, func(ctx context.Context, next *domainplugin.InstalledPlugin) error {
			const version = "2.0.0"
			if _, err := companions.PutArtifact(ctx, domaincompanion.Artifact{
				PluginID: id, Version: version, SizeBytes: 1, InstalledAt: now,
			}); err != nil {
				return err
			}
			items, err := companions.ListArtifacts(ctx, id)
			if err != nil || len(items) != 2 {
				return fmt.Errorf("list artifacts in update: count=%d err=%v", len(items), err)
			}
			if _, err := companions.ActivateArtifact(ctx, id, version, now); err != nil {
				return err
			}
			active, err := companions.GetActiveArtifact(ctx, id)
			if err != nil || active.Version != version {
				return fmt.Errorf("active artifact in update: version=%q err=%v", active.Version, err)
			}
			next.Version = active.Version
			return interrupted
		})
		if !errors.Is(err, interrupted) {
			t.Fatalf("interrupted update = %v, want %v", err, interrupted)
		}
		items, err := companions.ListArtifacts(t.Context(), id)
		if err != nil || len(items) != 1 || items[0].Version != installed.Version || !items[0].Active {
			t.Fatalf("artifacts after rollback = %#v, err=%v", items, err)
		}
		current, err := plugins.GetInstalled(t.Context(), id)
		if err != nil || current.Version != installed.Version || !current.UpdatedAt.Equal(installed.UpdatedAt) {
			t.Fatalf("installation after rollback = %#v, err=%v", current, err)
		}
	})

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
