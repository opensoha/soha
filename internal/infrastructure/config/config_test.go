package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveMigrationPathUsesDriverDirectoryForMigrationRoot(t *testing.T) {
	root := t.TempDir()
	postgresDir := filepath.Join(root, "postgres")
	if err := os.MkdirAll(postgresDir, 0o755); err != nil {
		t.Fatalf("mkdir postgres dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(postgresDir, "0001_init.sql"), []byte("-- init"), 0o644); err != nil {
		t.Fatalf("write migration: %v", err)
	}
	if err := os.WriteFile(filepath.Join(postgresDir, "0011_application_services.sql"), []byte("-- app services"), 0o644); err != nil {
		t.Fatalf("write migration: %v", err)
	}

	cfg := DatabaseConfig{Driver: "postgres", MigrationPath: root}
	if got := cfg.ResolveMigrationPath(); got != postgresDir {
		t.Fatalf("ResolveMigrationPath() = %q, want %q", got, postgresDir)
	}
}

func TestResolveMigrationPathKeepsLegacySingleFileRedirect(t *testing.T) {
	root := t.TempDir()
	legacyPath := filepath.Join(root, "migrations", "0001_init.sql")
	postgresDir := filepath.Join(root, "migrations", "postgres")
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o755); err != nil {
		t.Fatalf("mkdir legacy dir: %v", err)
	}
	if err := os.MkdirAll(postgresDir, 0o755); err != nil {
		t.Fatalf("mkdir postgres dir: %v", err)
	}
	if err := os.WriteFile(legacyPath, []byte("-- legacy"), 0o644); err != nil {
		t.Fatalf("write legacy migration: %v", err)
	}
	driverInit := filepath.Join(postgresDir, "0001_init.sql")
	if err := os.WriteFile(driverInit, []byte("-- driver"), 0o644); err != nil {
		t.Fatalf("write driver migration: %v", err)
	}

	cfg := DatabaseConfig{Driver: "postgres", MigrationFile: legacyPath}
	if got := cfg.ResolveMigrationPath(); got != driverInit {
		t.Fatalf("ResolveMigrationPath() = %q, want %q", got, driverInit)
	}
}
