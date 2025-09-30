package schema

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"sort"
	"strings"
)

//go:embed migrations/postgres/*.sql
var postgresFS embed.FS

//go:embed migrations/sqlite/*.sql
var sqliteFS embed.FS

// MigrationRunner handles database migrations
type MigrationRunner struct {
	db         *sql.DB
	dbType     string // "postgres" or "sqlite"
	migrations []Migration
}

// Migration represents a single database migration
type Migration struct {
	Version int
	Name    string
	SQL     string
}

// NewMigrationRunner creates a new migration runner
func NewMigrationRunner(db *sql.DB, dbType string) (*MigrationRunner, error) {
	mr := &MigrationRunner{
		db:     db,
		dbType: dbType,
	}

	if err := mr.loadMigrations(); err != nil {
		return nil, fmt.Errorf("failed to load migrations: %w", err)
	}

	return mr, nil
}

// loadMigrations loads migration files from embedded filesystem
func (mr *MigrationRunner) loadMigrations() error {
	var fs embed.FS
	var path string

	switch mr.dbType {
	case "postgres":
		fs = postgresFS
		path = "migrations/postgres"
	case "sqlite":
		fs = sqliteFS
		path = "migrations/sqlite"
	default:
		return fmt.Errorf("unsupported database type: %s", mr.dbType)
	}

	entries, err := fs.ReadDir(path)
	if err != nil {
		return fmt.Errorf("failed to read migration directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		content, err := fs.ReadFile(path + "/" + entry.Name())
		if err != nil {
			return fmt.Errorf("failed to read migration file %s: %w", entry.Name(), err)
		}

		// Extract version from filename (e.g., "001_initial.sql" -> 1)
		var version int
		_, err = fmt.Sscanf(entry.Name(), "%d_", &version)
		if err != nil {
			return fmt.Errorf("invalid migration filename %s: %w", entry.Name(), err)
		}

		mr.migrations = append(mr.migrations, Migration{
			Version: version,
			Name:    entry.Name(),
			SQL:     string(content),
		})
	}

	// Sort migrations by version
	sort.Slice(mr.migrations, func(i, j int) bool {
		return mr.migrations[i].Version < mr.migrations[j].Version
	})

	return nil
}

// Run executes all pending migrations
func (mr *MigrationRunner) Run(ctx context.Context) error {
	// Create schema version table if it doesn't exist
	if err := mr.createSchemaVersionTable(ctx); err != nil {
		return fmt.Errorf("failed to create schema version table: %w", err)
	}

	// Get current version
	currentVersion, err := mr.getCurrentVersion(ctx)
	if err != nil {
		return fmt.Errorf("failed to get current version: %w", err)
	}

	// Run pending migrations
	for _, migration := range mr.migrations {
		if migration.Version <= currentVersion {
			continue
		}

		if err := mr.runMigration(ctx, migration); err != nil {
			return fmt.Errorf("failed to run migration %s: %w", migration.Name, err)
		}
	}

	return nil
}

// createSchemaVersionTable creates the schema_version table
func (mr *MigrationRunner) createSchemaVersionTable(ctx context.Context) error {
	query := `
		CREATE TABLE IF NOT EXISTS schema_version (
			version INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`

	if mr.dbType == "postgres" {
		query = strings.ReplaceAll(query, "TIMESTAMP DEFAULT CURRENT_TIMESTAMP", "TIMESTAMP DEFAULT NOW()")
	}

	_, err := mr.db.ExecContext(ctx, query)
	return err
}

// getCurrentVersion returns the current schema version
func (mr *MigrationRunner) getCurrentVersion(ctx context.Context) (int, error) {
	var version int
	query := "SELECT COALESCE(MAX(version), 0) FROM schema_version"

	err := mr.db.QueryRowContext(ctx, query).Scan(&version)
	if err != nil {
		return 0, err
	}

	return version, nil
}

// runMigration runs a single migration in a transaction
func (mr *MigrationRunner) runMigration(ctx context.Context, migration Migration) error {
	tx, err := mr.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Execute migration SQL
	if _, err := tx.ExecContext(ctx, migration.SQL); err != nil {
		return fmt.Errorf("failed to execute migration SQL: %w", err)
	}

	// Record migration
	recordQuery := "INSERT INTO schema_version (version, name) VALUES ($1, $2)"
	if mr.dbType == "sqlite" {
		recordQuery = "INSERT INTO schema_version (version, name) VALUES (?, ?)"
	}

	if _, err := tx.ExecContext(ctx, recordQuery, migration.Version, migration.Name); err != nil {
		return fmt.Errorf("failed to record migration: %w", err)
	}

	return tx.Commit()
}