package db

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type Store struct {
	db     *gorm.DB
	sqlDB  *sql.DB
	driver string
}

func New(cfg cfgpkg.DatabaseConfig) (*Store, error) {
	driver := strings.ToLower(strings.TrimSpace(cfg.Driver))
	if driver == "" {
		driver = "postgres"
	}

	var dialector gorm.Dialector
	switch driver {
	case "postgres":
		dialector = postgres.Open(cfg.DSN())
	default:
		return nil, fmt.Errorf("unsupported database driver: %s", driver)
	}

	db, err := gorm.Open(dialector, &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("open gorm %s: %w", driver, err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get sql db: %w", err)
	}
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	return &Store{db: db, sqlDB: sqlDB, driver: driver}, nil
}

func (s *Store) Driver() string {
	return s.driver
}

func (s *Store) DB() *gorm.DB {
	return s.db
}

func (s *Store) SQLDB() *sql.DB {
	return s.sqlDB
}

func (s *Store) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return s.sqlDB.PingContext(ctx)
}

func (s *Store) MigrateFromFile(ctx context.Context, path string) error {
	migrationFiles, err := resolveMigrationFiles(path)
	if err != nil {
		return err
	}
	if len(migrationFiles) == 0 {
		return nil
	}
	if err := s.ensureMigrationTable(ctx); err != nil {
		return err
	}
	for _, migrationFile := range migrationFiles {
		applied, err := s.migrationApplied(ctx, migrationFile)
		if err != nil {
			return err
		}
		if applied {
			continue
		}
		migrationRoot, err := os.OpenRoot(filepath.Dir(migrationFile))
		if err != nil {
			return fmt.Errorf("open migration directory %s: %w", filepath.Dir(migrationFile), err)
		}
		statement, readErr := migrationRoot.ReadFile(filepath.Base(migrationFile))
		closeErr := migrationRoot.Close()
		if readErr != nil {
			return fmt.Errorf("read migration file %s: %w", migrationFile, readErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close migration directory %s: %w", filepath.Dir(migrationFile), closeErr)
		}
		if err := s.executeMigrationStatement(ctx, string(statement)); err != nil {
			return fmt.Errorf("execute migration file %s: %w", migrationFile, err)
		}
		if err := s.recordMigration(ctx, migrationFile); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Exec(ctx context.Context, statement string, args ...any) error {
	_, err := s.sqlDB.ExecContext(ctx, statement, args...)
	return err
}

func (s *Store) Close() error {
	return s.sqlDB.Close()
}

func (s *Store) executeMigrationStatement(ctx context.Context, statement string) error {
	conn, err := s.sqlDB.Conn(ctx)
	if err != nil {
		return fmt.Errorf("get migration connection: %w", err)
	}
	defer func() { _ = conn.Close() }()

	if _, err := conn.ExecContext(ctx, statement); err != nil {
		_, _ = conn.ExecContext(ctx, `ROLLBACK`)
		_, _ = conn.ExecContext(ctx, `RESET ALL`)
		return err
	}
	if _, err := conn.ExecContext(ctx, `RESET ALL`); err != nil {
		return fmt.Errorf("reset migration connection session: %w", err)
	}
	return nil
}

func (s *Store) ensureMigrationTable(ctx context.Context) error {
	_, err := s.sqlDB.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS public.schema_migrations (
			filename TEXT PRIMARY KEY,
			executed_at TIMESTAMP NOT NULL DEFAULT NOW()
		)
	`)
	if err != nil {
		return fmt.Errorf("ensure schema migrations table: %w", err)
	}
	return nil
}

func (s *Store) migrationApplied(ctx context.Context, filename string) (bool, error) {
	var count int
	if err := s.sqlDB.QueryRowContext(ctx, `SELECT COUNT(1) FROM public.schema_migrations WHERE filename = $1`, filename).Scan(&count); err != nil {
		return false, fmt.Errorf("query schema migrations: %w", err)
	}
	return count > 0, nil
}

func (s *Store) recordMigration(ctx context.Context, filename string) error {
	if _, err := s.sqlDB.ExecContext(ctx, `INSERT INTO public.schema_migrations (filename, executed_at) VALUES ($1, $2)`, filename, time.Now().UTC()); err != nil {
		return fmt.Errorf("record schema migration %s: %w", filename, err)
	}
	return nil
}

func resolveMigrationFiles(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat migration path: %w", err)
	}
	if info.IsDir() {
		return listMigrationFiles(path)
	}
	if strings.EqualFold(filepath.Ext(path), ".sql") {
		dir := filepath.Dir(path)
		files, err := listMigrationFiles(dir)
		if err == nil && len(files) > 0 {
			return files, nil
		}
		return []string{path}, nil
	}
	return []string{path}, nil
}

func listMigrationFiles(dir string) ([]string, error) {
	items := make([]string, 0)
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != dir {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.EqualFold(filepath.Ext(path), ".sql") {
			return nil
		}
		items = append(items, path)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list migration files: %w", err)
	}
	sort.Strings(items)
	return items, nil
}
