package database

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
)

func TestEmbeddedMigrationsAreContiguous(t *testing.T) {
	t.Parallel()

	items, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations() error = %v", err)
	}
	if len(items) != LatestSchemaVersion {
		t.Fatalf("migration count = %d, latest version = %d", len(items), LatestSchemaVersion)
	}
	for index, item := range items {
		if item.version != index+1 || item.checksum == "" || strings.TrimSpace(item.sql) == "" {
			t.Errorf("migration %d = %+v", index, item)
		}
	}
}

func TestVerifyHistoryRejectsChangedMigration(t *testing.T) {
	t.Parallel()

	items, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	err = verifyHistory([]appliedMigration{{Version: 1, Checksum: "changed"}}, items)
	if err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("verifyHistory() error = %v, want checksum failure", err)
	}
}

func TestPostgresMigrationAndRuntimeRestrictions(t *testing.T) {
	adminURL := os.Getenv("CURU_TEST_DATABASE_URL")
	if adminURL == "" {
		t.Skip("CURU_TEST_DATABASE_URL is not set; skipping disposable PostgreSQL integration check")
	}
	ctx := context.Background()
	admin, err := Open(ctx, adminURL)
	if err != nil {
		t.Fatalf("Open(admin) error = %v", err)
	}
	defer Close(admin)

	const runtimeRole = "curu_runtime_test"
	const runtimePassword = "curu-runtime-test-password"
	cleanup := func() {
		admin.Exec("DROP SCHEMA IF EXISTS curu CASCADE")
		admin.Exec("DROP ROLE IF EXISTS " + runtimeRole)
	}
	cleanup()
	defer cleanup()
	if err := admin.Exec(fmt.Sprintf("CREATE ROLE %s LOGIN PASSWORD '%s'", runtimeRole, runtimePassword)).Error; err != nil {
		t.Fatalf("create runtime role: %v", err)
	}

	runtimeURL := databaseURLForRole(t, adminURL, runtimeRole, runtimePassword)
	runtime, err := Open(ctx, runtimeURL)
	if err != nil {
		t.Fatalf("Open(runtime) before migration error = %v", err)
	}
	if err := CheckCompatibility(ctx, runtime); err == nil {
		t.Fatal("CheckCompatibility() before migration error = nil")
	}
	Close(runtime)
	var schemaExists bool
	if err := admin.Raw("SELECT to_regnamespace('curu') IS NOT NULL").Scan(&schemaExists).Error; err != nil {
		t.Fatal(err)
	}
	if schemaExists {
		t.Fatal("runtime compatibility check created the curu schema")
	}

	if err := migrateThrough(ctx, admin, runtimeRole, 1); err != nil {
		t.Fatalf("migrateThrough(version 1) error = %v", err)
	}
	const fixtureID = "11111111-1111-1111-1111-111111111111"
	const ownerID = "22222222-2222-2222-2222-222222222222"
	if err := admin.Exec(`INSERT INTO curu.activities
        (id, owner_id, source_identity, title, content_markdown)
        VALUES (?, ?, 'existing-fixture', 'Retained activity', 'Original content')`, fixtureID, ownerID).Error; err != nil {
		t.Fatalf("insert forward-migration fixture: %v", err)
	}
	if err := Migrate(ctx, admin, runtimeRole); err != nil {
		t.Fatalf("Migrate() forward error = %v", err)
	}
	if err := Migrate(ctx, admin, runtimeRole); err != nil {
		t.Fatalf("Migrate() repeat error = %v", err)
	}
	var title string
	if err := admin.Raw("SELECT title FROM curu.activities WHERE id = ?", fixtureID).Scan(&title).Error; err != nil || title != "Retained activity" {
		t.Fatalf("retained fixture title = %q, error = %v", title, err)
	}

	runtime, err = Open(ctx, runtimeURL)
	if err != nil {
		t.Fatalf("Open(runtime) after migration error = %v", err)
	}
	defer Close(runtime)
	if err := CheckCompatibility(ctx, runtime); err != nil {
		t.Fatalf("CheckCompatibility() error = %v", err)
	}
	if err := runtime.Exec(`INSERT INTO curu.activities
        (id, owner_id, title) VALUES
        ('33333333-3333-3333-3333-333333333333', ?, 'Runtime activity')`, ownerID).Error; err != nil {
		t.Fatalf("runtime activity insert denied: %v", err)
	}
	if err := runtime.Exec("CREATE TABLE curu.runtime_must_not_create (id integer)").Error; err == nil {
		t.Fatal("runtime role created a table")
	}
	if err := runtime.Exec("INSERT INTO curu.schema_migrations (version, name, checksum) VALUES (99, 'bad', 'bad')").Error; err == nil {
		t.Fatal("runtime role changed migration history")
	}
	if err := runtime.Exec("DELETE FROM curu.activities WHERE id = ?", fixtureID).Error; err == nil {
		t.Fatal("runtime role permanently deleted an activity")
	}
	if err := runtime.Exec("INSERT INTO curu.dataset_initializations (kind) VALUES ('empty')").Error; err == nil {
		t.Fatal("runtime role changed bootstrap metadata")
	}
}

func databaseURLForRole(t *testing.T, value, role, password string) string {
	t.Helper()
	parsed, err := url.Parse(value)
	if err != nil {
		t.Fatalf("parse CURU_TEST_DATABASE_URL: %v", err)
	}
	parsed.User = url.UserPassword(role, password)
	return parsed.String()
}
