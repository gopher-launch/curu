package database

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strings"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const LatestSchemaVersion = 2

const migrationLockID int64 = 4392508880345810201

//go:embed migrations/*.sql
var migrationFiles embed.FS

type migration struct {
	version  int
	name     string
	checksum string
	sql      string
}

type appliedMigration struct {
	Version  int
	Checksum string
}

var rolePattern = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

// Open opens a PostgreSQL connection through GORM without performing schema
// inference or mutation.
func Open(ctx context.Context, dsn string) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.New(postgres.Config{DSN: dsn, PreferSimpleProtocol: true}), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("open PostgreSQL connection: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("access PostgreSQL connection pool: %w", err)
	}
	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("ping PostgreSQL: %w", err)
	}
	return db, nil
}

// Close closes a GORM PostgreSQL connection pool.
func Close(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// Migrate applies all embedded forward migrations and grants only the
// implemented runtime privileges to runtimeRole. It never uses AutoMigrate.
func Migrate(ctx context.Context, db *gorm.DB, runtimeRole string) error {
	return migrateThrough(ctx, db, runtimeRole, LatestSchemaVersion)
}

func migrateThrough(ctx context.Context, db *gorm.DB, runtimeRole string, target int) error {
	if !rolePattern.MatchString(runtimeRole) {
		return errors.New("runtime database role must match [a-z_][a-z0-9_]*")
	}
	migrations, err := loadMigrations()
	if err != nil {
		return err
	}
	if target < 0 || target > LatestSchemaVersion {
		return fmt.Errorf("unsupported target schema version %d", target)
	}

	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", migrationLockID).Error; err != nil {
			return fmt.Errorf("lock migrations: %w", err)
		}
		if err := tx.Exec("CREATE SCHEMA IF NOT EXISTS curu").Error; err != nil {
			return fmt.Errorf("create curu schema: %w", err)
		}
		if err := tx.Exec(`CREATE TABLE IF NOT EXISTS curu.schema_migrations (
            version integer PRIMARY KEY CHECK (version > 0),
            name text NOT NULL,
            checksum text NOT NULL,
            applied_at timestamptz NOT NULL DEFAULT current_timestamp
        )`).Error; err != nil {
			return fmt.Errorf("create migration history: %w", err)
		}

		var applied []appliedMigration
		if err := tx.Raw("SELECT version, checksum FROM curu.schema_migrations ORDER BY version").Scan(&applied).Error; err != nil {
			return fmt.Errorf("read migration history: %w", err)
		}
		if err := verifyHistory(applied, migrations); err != nil {
			return err
		}

		appliedVersions := make(map[int]bool, len(applied))
		for _, item := range applied {
			appliedVersions[item.Version] = true
		}
		for _, item := range migrations {
			if item.version > target || appliedVersions[item.version] {
				continue
			}
			if err := tx.Exec(item.sql).Error; err != nil {
				return fmt.Errorf("apply migration %04d %s: %w", item.version, item.name, err)
			}
			if err := tx.Exec(
				"INSERT INTO curu.schema_migrations (version, name, checksum) VALUES (?, ?, ?)",
				item.version, item.name, item.checksum,
			).Error; err != nil {
				return fmt.Errorf("record migration %04d: %w", item.version, err)
			}
		}

		if err := grantRuntimeAccess(tx, runtimeRole); err != nil {
			return err
		}
		return validateRuntimeRole(tx, runtimeRole)
	})
}

func grantRuntimeAccess(tx *gorm.DB, role string) error {
	quoted := quoteIdentifier(role)
	statements := []string{
		"REVOKE ALL PRIVILEGES ON SCHEMA curu FROM PUBLIC",
		"REVOKE ALL PRIVILEGES ON ALL TABLES IN SCHEMA curu FROM PUBLIC",
		"REVOKE ALL PRIVILEGES ON SCHEMA curu FROM " + quoted,
		"REVOKE ALL PRIVILEGES ON ALL TABLES IN SCHEMA curu FROM " + quoted,
		"GRANT USAGE ON SCHEMA curu TO " + quoted,
		"GRANT SELECT ON curu.schema_migrations TO " + quoted,
		"GRANT SELECT, INSERT, UPDATE ON curu.activities TO " + quoted,
		"GRANT SELECT ON curu.dataset_initializations TO " + quoted,
	}
	// Progressions exist only after migration 2, including during the focused
	// forward-migration fixture which intentionally stops at migration 1.
	var exists bool
	if err := tx.Raw("SELECT to_regclass('curu.activity_progressions') IS NOT NULL").Scan(&exists).Error; err != nil {
		return fmt.Errorf("inspect progression table: %w", err)
	}
	if exists {
		statements = append(statements, "GRANT SELECT, INSERT, UPDATE, DELETE ON curu.activity_progressions TO "+quoted)
	}
	for _, statement := range statements {
		if err := tx.Exec(statement).Error; err != nil {
			return fmt.Errorf("grant runtime access: %w", err)
		}
	}
	return nil
}

func validateRuntimeRole(tx *gorm.DB, role string) error {
	var elevated bool
	if err := tx.Raw(`SELECT rolsuper OR rolcreaterole OR rolcreatedb OR rolreplication OR rolbypassrls
        FROM pg_roles WHERE rolname = ?`, role).Scan(&elevated).Error; err != nil {
		return fmt.Errorf("inspect runtime role attributes: %w", err)
	}
	if elevated {
		return errors.New("runtime database role has an elevated PostgreSQL attribute")
	}

	var canCreateTemporary bool
	if err := tx.Raw(
		"SELECT has_database_privilege(?, current_database(), 'TEMPORARY')", role,
	).Scan(&canCreateTemporary).Error; err != nil {
		return fmt.Errorf("inspect runtime database privileges: %w", err)
	}
	if canCreateTemporary {
		return errors.New("runtime database role has effective TEMPORARY privilege; the database owner must revoke TEMPORARY from PUBLIC and inherited roles")
	}
	var canCreateSchema bool
	if err := tx.Raw(
		"SELECT has_database_privilege(?, current_database(), 'CREATE')", role,
	).Scan(&canCreateSchema).Error; err != nil {
		return fmt.Errorf("inspect runtime database CREATE privilege: %w", err)
	}
	if canCreateSchema {
		return errors.New("runtime database role has effective database CREATE privilege")
	}

	var creatableSchemas string
	if err := tx.Raw(`SELECT COALESCE(string_agg(quote_ident(nspname), ', ' ORDER BY nspname), '')
        FROM pg_namespace
        WHERE nspname NOT LIKE 'pg_temp_%'
          AND nspname NOT LIKE 'pg_toast_temp_%'
          AND has_schema_privilege(?, oid, 'CREATE')`, role).Scan(&creatableSchemas).Error; err != nil {
		return fmt.Errorf("inspect runtime schema privileges: %w", err)
	}
	if creatableSchemas != "" {
		return fmt.Errorf("runtime database role has effective CREATE privilege on schemas: %s", creatableSchemas)
	}
	return nil
}

func quoteIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

// CheckCompatibility verifies that the database has exactly the migration
// history supported by this build. It is deliberately read-only.
func CheckCompatibility(ctx context.Context, db *gorm.DB) error {
	migrations, err := loadMigrations()
	if err != nil {
		return err
	}
	var applied []appliedMigration
	if err := db.WithContext(ctx).Raw(
		"SELECT version, checksum FROM curu.schema_migrations ORDER BY version",
	).Scan(&applied).Error; err != nil {
		return fmt.Errorf("read schema compatibility state: %w", err)
	}
	if err := verifyHistory(applied, migrations); err != nil {
		return err
	}
	if len(applied) != LatestSchemaVersion {
		return fmt.Errorf("schema version %d is incompatible; this build requires version %d", len(applied), LatestSchemaVersion)
	}
	return nil
}

func verifyHistory(applied []appliedMigration, migrations []migration) error {
	if len(applied) > len(migrations) {
		return fmt.Errorf("database schema is newer than this application (found %d migrations, supports %d)", len(applied), len(migrations))
	}
	for index, item := range applied {
		expectedVersion := index + 1
		if item.Version != expectedVersion {
			return fmt.Errorf("migration history is not contiguous at version %d", expectedVersion)
		}
		expected := migrations[index]
		if item.Checksum != expected.checksum {
			return fmt.Errorf("migration %d checksum does not match this application", item.Version)
		}
	}
	return nil
}

func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return nil, fmt.Errorf("read embedded migrations: %w", err)
	}
	items := make([]migration, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		parts := strings.SplitN(strings.TrimSuffix(entry.Name(), ".sql"), "_", 2)
		var version int
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid migration filename %q", entry.Name())
		}
		if _, err := fmt.Sscanf(parts[0], "%04d", &version); err != nil {
			return nil, fmt.Errorf("invalid migration filename %q", entry.Name())
		}
		content, err := migrationFiles.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return nil, fmt.Errorf("read migration %q: %w", entry.Name(), err)
		}
		digest := sha256.Sum256(content)
		items = append(items, migration{version: version, name: entry.Name(), checksum: hex.EncodeToString(digest[:]), sql: string(content)})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].version < items[j].version })
	for index, item := range items {
		if item.version != index+1 {
			return nil, fmt.Errorf("embedded migrations are not contiguous at version %d", index+1)
		}
	}
	return items, nil
}
