package database

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/gopher-launch/curu/internal/activity"
	"github.com/gopher-launch/curu/internal/seed"
	"gorm.io/gorm"
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
	const assumableRole = "curu_runtime_assumable_test"
	cleanup := func() {
		admin.Exec("REVOKE " + assumableRole + " FROM " + runtimeRole)
		admin.Exec("REVOKE ALL PRIVILEGES ON DATABASE " + quoteIdentifier(currentDatabase(t, admin)) + " FROM " + runtimeRole)
		admin.Exec("DROP SCHEMA IF EXISTS curu CASCADE")
		admin.Exec("DROP SCHEMA IF EXISTS runtime_escape CASCADE")
		admin.Exec("DROP ROLE IF EXISTS " + runtimeRole)
		admin.Exec("DROP ROLE IF EXISTS " + assumableRole)
		admin.Exec("GRANT TEMPORARY ON DATABASE " + quoteIdentifier(currentDatabase(t, admin)) + " TO PUBLIC")
	}
	cleanup()
	defer cleanup()
	if err := admin.Exec(fmt.Sprintf("CREATE ROLE %s LOGIN NOINHERIT PASSWORD '%s'", runtimeRole, runtimePassword)).Error; err != nil {
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

	if err := Migrate(ctx, admin, runtimeRole); err == nil || !strings.Contains(err.Error(), "TEMPORARY") {
		t.Fatalf("Migrate() with inherited TEMPORARY error = %v, want fail-closed TEMPORARY error", err)
	}
	if err := admin.Exec("REVOKE TEMPORARY ON DATABASE " + quoteIdentifier(currentDatabase(t, admin)) + " FROM PUBLIC").Error; err != nil {
		t.Fatalf("revoke disposable database TEMPORARY privilege: %v", err)
	}
	if err := admin.Exec("GRANT CREATE ON DATABASE " + quoteIdentifier(currentDatabase(t, admin)) + " TO " + runtimeRole).Error; err != nil {
		t.Fatalf("grant database CREATE fixture: %v", err)
	}
	if err := Migrate(ctx, admin, runtimeRole); err == nil || !strings.Contains(err.Error(), "database CREATE") {
		t.Fatalf("Migrate() with database CREATE error = %v, want fail-closed database CREATE error", err)
	}
	if err := admin.Exec("REVOKE CREATE ON DATABASE " + quoteIdentifier(currentDatabase(t, admin)) + " FROM " + runtimeRole).Error; err != nil {
		t.Fatalf("revoke database CREATE fixture: %v", err)
	}
	if err := admin.Exec("CREATE SCHEMA runtime_escape").Error; err != nil {
		t.Fatalf("create privilege fixture schema: %v", err)
	}
	if err := admin.Exec("GRANT CREATE ON SCHEMA runtime_escape TO " + runtimeRole).Error; err != nil {
		t.Fatalf("grant privilege fixture: %v", err)
	}
	if err := Migrate(ctx, admin, runtimeRole); err == nil || !strings.Contains(err.Error(), "runtime_escape") {
		t.Fatalf("Migrate() with unintended schema CREATE error = %v, want fail-closed schema error", err)
	}
	if err := admin.Exec("DROP SCHEMA runtime_escape").Error; err != nil {
		t.Fatalf("drop privilege fixture schema: %v", err)
	}

	if err := admin.Exec("CREATE SCHEMA runtime_escape").Error; err != nil {
		t.Fatalf("create SET ROLE fixture schema: %v", err)
	}
	if err := admin.Exec("CREATE ROLE " + assumableRole + " NOLOGIN CREATEROLE").Error; err != nil {
		t.Fatalf("create assumable role: %v", err)
	}
	if err := admin.Exec("GRANT CREATE ON SCHEMA runtime_escape TO " + assumableRole).Error; err != nil {
		t.Fatalf("grant assumable role schema privilege: %v", err)
	}
	if err := admin.Exec("GRANT " + assumableRole + " TO " + runtimeRole).Error; err != nil {
		t.Fatalf("grant runtime role membership: %v", err)
	}
	runtime, err = Open(ctx, runtimeURL)
	if err != nil {
		t.Fatalf("Open(runtime) for SET ROLE fixture error = %v", err)
	}
	if err := runtime.Exec("SET ROLE " + assumableRole).Error; err != nil {
		t.Fatalf("SET ROLE fixture is not assumable: %v", err)
	}
	if err := runtime.Exec("CREATE TABLE runtime_escape.membership_escape (id integer)").Error; err != nil {
		t.Fatalf("SET ROLE fixture lacks expected schema authority: %v", err)
	}
	if err := runtime.Exec("RESET ROLE").Error; err != nil {
		t.Fatalf("reset SET ROLE fixture: %v", err)
	}
	if err := Close(runtime); err != nil {
		t.Fatalf("close SET ROLE fixture connection: %v", err)
	}
	if err := Migrate(ctx, admin, runtimeRole); err == nil || !strings.Contains(err.Error(), "role memberships") {
		t.Fatalf("Migrate() with SET ROLE authority error = %v, want fail-closed membership error", err)
	}
	if err := admin.Raw("SELECT to_regnamespace('curu') IS NOT NULL").Scan(&schemaExists).Error; err != nil {
		t.Fatalf("inspect migration rollback after role membership rejection: %v", err)
	}
	if schemaExists {
		t.Fatal("migration with SET ROLE authority did not roll back the curu schema")
	}
	if err := admin.Exec("REVOKE " + assumableRole + " FROM " + runtimeRole).Error; err != nil {
		t.Fatalf("revoke runtime role membership fixture: %v", err)
	}
	if err := admin.Exec("DROP SCHEMA runtime_escape CASCADE").Error; err != nil {
		t.Fatalf("drop SET ROLE fixture schema: %v", err)
	}
	if err := admin.Exec("DROP ROLE " + assumableRole).Error; err != nil {
		t.Fatalf("drop assumable role fixture: %v", err)
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
	const seedOwnerID = "44444444-4444-4444-8444-444444444444"
	library, err := seed.Load()
	if err != nil {
		t.Fatalf("seed.Load() error = %v", err)
	}
	if err := loadSeedFixture(admin, seedOwnerID, library); err != nil {
		t.Fatalf("load seed fixture: %v", err)
	}
	var activityCount, progressionCount int64
	if err := admin.Raw("SELECT count(*) FROM curu.activities WHERE owner_id = ?", seedOwnerID).Scan(&activityCount).Error; err != nil {
		t.Fatalf("count seeded activities: %v", err)
	}
	if err := admin.Raw(`SELECT count(*) FROM curu.activity_progressions p
		JOIN curu.activities a ON a.id = p.activity_id WHERE a.owner_id = ?`, seedOwnerID).Scan(&progressionCount).Error; err != nil {
		t.Fatalf("count seeded progressions: %v", err)
	}
	if activityCount != seed.ExpectedActivities || progressionCount != seed.ExpectedProgressions {
		t.Fatalf("seeded counts = %d/%d, want %d/%d", activityCount, progressionCount, seed.ExpectedActivities, seed.ExpectedProgressions)
	}

	runtime, err = Open(ctx, runtimeURL)
	if err != nil {
		t.Fatalf("Open(runtime) after migration error = %v", err)
	}
	defer Close(runtime)
	if err := CheckCompatibility(ctx, runtime); err != nil {
		t.Fatalf("CheckCompatibility() error = %v", err)
	}
	repository, err := NewActivityRepository(runtime)
	if err != nil {
		t.Fatalf("NewActivityRepository() error = %v", err)
	}
	searchResults, err := repository.ListCurrent(ctx, seedOwnerID, "Release hands")
	if err != nil {
		t.Fatalf("ListCurrent() progression search error = %v", err)
	}
	if len(searchResults) == 0 {
		t.Fatal("ListCurrent() progression search returned no seeded activity")
	}
	for _, item := range searchResults {
		if item.OwnerID != seedOwnerID {
			t.Fatalf("ListCurrent() crossed owner boundary: %+v", item)
		}
	}
	for _, search := range []string{"Evasion Gauntlet", "acceleration"} {
		results, err := repository.ListCurrent(ctx, seedOwnerID, search)
		if err != nil || len(results) == 0 {
			t.Fatalf("ListCurrent(%q) results = %+v, error = %v", search, results, err)
		}
	}
	literalResults, err := repository.ListCurrent(ctx, seedOwnerID, "%_not-present")
	if err != nil || len(literalResults) != 0 {
		t.Fatalf("ListCurrent() literal metacharacter results = %+v, error = %v", literalResults, err)
	}
	seededDetail, err := repository.FindCurrent(ctx, seedOwnerID, library.Activities[0].ID)
	if err != nil {
		t.Fatalf("FindCurrent() seeded detail error = %v", err)
	}
	if seededDetail.Title != library.Activities[0].Title || len(seededDetail.Progressions) != len(library.Activities[0].Progressions) {
		t.Fatalf("FindCurrent() detail = %+v", seededDetail)
	}
	otherOwner, err := repository.ListCurrent(ctx, ownerID, "Release hands")
	if err != nil || len(otherOwner) != 0 {
		t.Fatalf("ListCurrent() other-owner results = %+v, error = %v", otherOwner, err)
	}
	if err := admin.Exec("UPDATE curu.activities SET archived_at = current_timestamp WHERE id = ?", fixtureID).Error; err != nil {
		t.Fatalf("archive repository fixture: %v", err)
	}
	archivedResults, err := repository.ListCurrent(ctx, ownerID, "Retained activity")
	if err != nil || len(archivedResults) != 0 {
		t.Fatalf("ListCurrent() archived results = %+v, error = %v", archivedResults, err)
	}
	if _, err := repository.FindCurrent(ctx, ownerID, fixtureID); !errors.Is(err, activity.ErrNotFound) {
		t.Fatalf("FindCurrent() archived error = %v, want not found", err)
	}
	if err := runtime.Exec(`INSERT INTO curu.activities
        (id, owner_id, title) VALUES
        ('33333333-3333-3333-3333-333333333333', ?, 'Runtime activity')`, ownerID).Error; err != nil {
		t.Fatalf("runtime activity insert denied: %v", err)
	}
	if err := runtime.Exec("CREATE TABLE curu.runtime_must_not_create (id integer)").Error; err == nil {
		t.Fatal("runtime role created a table")
	}
	if err := runtime.Exec("CREATE TEMP TABLE runtime_must_not_create_temp (id integer)").Error; err == nil {
		t.Fatal("runtime role created a temporary table")
	}
	if err := runtime.Exec("CREATE TABLE public.runtime_must_not_create (id integer)").Error; err == nil {
		t.Fatal("runtime role created a table outside the curu schema")
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

func loadSeedFixture(db *gorm.DB, ownerID string, library seed.Library) error {
	return db.Transaction(func(tx *gorm.DB) error {
		for _, activity := range library.Activities {
			if err := tx.Exec(`INSERT INTO curu.activities
				(id, owner_id, source_identity, title, summary, content_markdown, duration_seconds)
				VALUES (?, ?, ?, ?, ?, ?, ?)`, activity.ID, ownerID, activity.SourceIdentity, activity.Title,
				activity.Summary, activity.ContentMarkdown, activity.DurationSeconds).Error; err != nil {
				return err
			}
			for _, progression := range activity.Progressions {
				if err := tx.Exec(`INSERT INTO curu.activity_progressions
					(id, activity_id, position, title, content_markdown, duration_seconds)
					VALUES (?, ?, ?, ?, ?, ?)`, progression.ID, activity.ID, progression.Position,
					progression.Title, progression.ContentMarkdown, progression.DurationSeconds).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func currentDatabase(t *testing.T, db *gorm.DB) string {
	t.Helper()
	var name string
	if err := db.Raw("SELECT current_database()").Scan(&name).Error; err != nil {
		t.Fatalf("read current database: %v", err)
	}
	return name
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
