// runtime-upgrade is an operator-only offline maintenance command. No operation
// is wired into server startup. DSN and backup content never appear in reports.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	upgrade "github.com/opensoha/soha/internal/repository/runtimeupgrade"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	mode := flag.String("mode", "dry-run", "dry-run, apply, or rollback")
	digest := flag.String("digest", "", "reviewed plan/checkpoint SHA256")
	backup := flag.String("backup", "", "existing nonempty 0600 PostgreSQL backup file (apply only)")
	quarantine := flag.String("quarantine-runs", "", "explicit comma-separated legacy run IDs, maximum 1000; expired leases only")
	flag.Parse()
	if *mode != "dry-run" && *mode != "apply" && *mode != "rollback" {
		return fmt.Errorf("invalid mode")
	}
	dsn := os.Getenv("SOHA_UPGRADE_DATABASE_URL")
	if dsn == "" {
		return fmt.Errorf("SOHA_UPGRADE_DATABASE_URL required")
	}
	backupHash := ""
	if *mode == "apply" {
		var err error
		backupHash, err = checkBackup(*backup)
		if err != nil {
			return err
		}
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return fmt.Errorf("database connection failed (details suppressed)")
	}
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("database unavailable")
	}
	defer sqlDB.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	r := upgrade.New(db)
	switch *mode {
	case "dry-run":
		p, err := r.DryRun(ctx)
		if err != nil {
			return fmt.Errorf("inventory failed; verify schema and database privileges")
		}
		return json.NewEncoder(os.Stdout).Encode(p)
	case "apply":
		ids := []string{}
		if *quarantine != "" {
			ids = strings.Split(*quarantine, ",")
		}
		if err := r.Apply(ctx, upgrade.ApplyOptions{Digest: *digest, BackupSHA256: backupHash, QuarantineRuns: ids}); err != nil {
			return fmt.Errorf("apply rejected or failed; review fresh inventory, drain/live_lease actions and checkpoint; transaction did not partially commit")
		}
	case "rollback":
		if len(*digest) != 64 {
			return fmt.Errorf("checkpoint digest required")
		}
		if err := r.Rollback(ctx, *digest); err != nil {
			return fmt.Errorf("rollback rejected or failed; verify checkpoint and stop leased memory workers before retry")
		}
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]string{"mode": *mode, "checkpoint": *digest, "result": "complete"})
}
func checkBackup(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("readable backup file required")
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil || !stat.Mode().IsRegular() || stat.Size() == 0 || stat.Mode().Perm() != 0600 {
		return "", fmt.Errorf("backup must be a nonempty regular file with mode 0600")
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("backup checksum failed")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
