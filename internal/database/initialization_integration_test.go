package database_test

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/gopher-launch/curu/internal/database"
	"github.com/gopher-launch/curu/internal/initialization"
	"github.com/gopher-launch/curu/internal/seed"
	"gorm.io/gorm"
)

func TestPostgresDatasetInitialization(t *testing.T) {
	adminURL := os.Getenv("CURU_TEST_DATABASE_URL")
	if adminURL == "" {
		t.Skip("CURU_TEST_DATABASE_URL is not set; skipping disposable PostgreSQL initialization check")
	}
	ctx := context.Background()
	admin, err := database.Open(ctx, adminURL)
	if err != nil {
		t.Fatalf("database.Open(admin) error = %v", err)
	}
	defer database.Close(admin)

	const runtimeRole = "curu_initialization_runtime_test"
	const runtimePassword = "curu-initialization-runtime-test-password"
	const initializerRole = "curu_initializer_test"
	const initializerPassword = "curu-initializer-test-password"
	cleanup := func() {
		admin.Exec("DROP SCHEMA IF EXISTS curu CASCADE")
		admin.Exec("DROP ROLE IF EXISTS " + runtimeRole)
		admin.Exec("DROP ROLE IF EXISTS " + initializerRole)
		admin.Exec("GRANT TEMPORARY ON DATABASE " + quoteIdentifier(currentDatabase(t, admin)) + " TO PUBLIC")
	}
	cleanup()
	defer cleanup()
	if err := admin.Exec(fmt.Sprintf("CREATE ROLE %s LOGIN NOINHERIT PASSWORD '%s'", runtimeRole, runtimePassword)).Error; err != nil {
		t.Fatalf("create runtime role: %v", err)
	}
	if err := admin.Exec(fmt.Sprintf("CREATE ROLE %s LOGIN NOINHERIT PASSWORD '%s'", initializerRole, initializerPassword)).Error; err != nil {
		t.Fatalf("create initializer role: %v", err)
	}
	if err := admin.Exec("REVOKE TEMPORARY ON DATABASE " + quoteIdentifier(currentDatabase(t, admin)) + " FROM PUBLIC").Error; err != nil {
		t.Fatalf("revoke disposable database TEMPORARY privilege: %v", err)
	}
	if err := database.Migrate(ctx, admin, runtimeRole); err != nil {
		t.Fatalf("database.Migrate() error = %v", err)
	}
	for _, statement := range []string{
		"GRANT USAGE ON SCHEMA curu TO " + initializerRole,
		"GRANT SELECT ON curu.schema_migrations TO " + initializerRole,
		"GRANT SELECT, INSERT ON curu.dataset_initializations TO " + initializerRole,
		"GRANT SELECT, INSERT ON curu.activities TO " + initializerRole,
		"GRANT INSERT ON curu.activity_progressions TO " + initializerRole,
	} {
		if err := admin.Exec(statement).Error; err != nil {
			t.Fatalf("grant initializer privilege: %v", err)
		}
	}
	initializerURL := databaseURLForRole(t, adminURL, initializerRole, initializerPassword)
	initializer, err := database.Open(ctx, initializerURL)
	if err != nil {
		t.Fatalf("database.Open(initializer) error = %v", err)
	}
	defer database.Close(initializer)
	if err := database.CheckCompatibility(ctx, initializer); err != nil {
		t.Fatalf("database.CheckCompatibility(initializer) error = %v", err)
	}

	library, err := seed.Load()
	if err != nil {
		t.Fatalf("seed.Load() error = %v", err)
	}
	const ownerID = "55555555-5555-4555-8555-555555555555"

	// Content without initialization metadata is never adopted or overwritten.
	if err := admin.Exec(`INSERT INTO curu.activities (id, owner_id, title)
		VALUES ('66666666-6666-4666-8666-666666666666', ?, 'Unrecognized activity')`, ownerID).Error; err != nil {
		t.Fatalf("insert unrecognized fixture: %v", err)
	}
	if _, err := initialization.InitializeSeed(ctx, initializer, ownerID, library); !errors.Is(err, initialization.ErrDatasetNotEmpty) {
		t.Fatalf("InitializeSeed(non-empty) error = %v, want ErrDatasetNotEmpty", err)
	}
	assertCounts(t, admin, 1, 0, 0)
	if err := admin.Exec("DELETE FROM curu.activities").Error; err != nil {
		t.Fatalf("clear non-empty fixture: %v", err)
	}

	// A deliberately induced mid-library insert failure rolls back every row and
	// the completion record, leaving a clean retry target.
	failingTitle := library.Activities[8].Title
	if err := admin.Exec(`ALTER TABLE curu.activities ADD CONSTRAINT initialization_failure_fixture
		CHECK (title <> ?)`, failingTitle).Error; err != nil {
		t.Fatalf("add failure fixture: %v", err)
	}
	if _, err := initialization.InitializeSeed(ctx, initializer, ownerID, library); err == nil {
		t.Fatal("InitializeSeed(failure fixture) error = nil")
	}
	assertCounts(t, admin, 0, 0, 0)
	if err := admin.Exec("ALTER TABLE curu.activities DROP CONSTRAINT initialization_failure_fixture").Error; err != nil {
		t.Fatalf("drop failure fixture: %v", err)
	}

	emptyResult, err := initialization.InitializeEmpty(ctx, initializer)
	if err != nil || emptyResult.Outcome != initialization.OutcomeInitialized {
		t.Fatalf("InitializeEmpty() = %+v, %v", emptyResult, err)
	}
	repeatedEmpty, err := initialization.InitializeEmpty(ctx, initializer)
	if err != nil || repeatedEmpty.Outcome != initialization.OutcomeAlreadyInitialized {
		t.Fatalf("repeated InitializeEmpty() = %+v, %v", repeatedEmpty, err)
	}
	assertCounts(t, admin, 0, 0, 1)

	// Reset only the disposable fixture so the same check can exercise fresh
	// seeded initialization and its concurrent retry behavior.
	if err := admin.Exec("DELETE FROM curu.dataset_initializations").Error; err != nil {
		t.Fatalf("reset empty initialization fixture: %v", err)
	}
	results := make(chan initialization.Result, 2)
	errorsFound := make(chan error, 2)
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			result, err := initialization.InitializeSeed(ctx, initializer, ownerID, library)
			results <- result
			errorsFound <- err
		}()
	}
	wait.Wait()
	close(results)
	close(errorsFound)
	for err := range errorsFound {
		if err != nil {
			t.Fatalf("concurrent InitializeSeed() error = %v", err)
		}
	}
	var initialized, noOp int
	for result := range results {
		switch result.Outcome {
		case initialization.OutcomeInitialized:
			initialized++
		case initialization.OutcomeAlreadyInitialized:
			noOp++
		default:
			t.Fatalf("unexpected concurrent result %+v", result)
		}
	}
	if initialized != 1 || noOp != 1 {
		t.Fatalf("concurrent outcomes initialized/no-op = %d/%d, want 1/1", initialized, noOp)
	}
	assertCounts(t, admin, seed.ExpectedActivities, seed.ExpectedProgressions, 1)

	const editedTitle = "Coach-edited activity"
	if err := admin.Exec(`UPDATE curu.activities SET title = ?, revision = revision + 1
		WHERE id = ?`, editedTitle, library.Activities[0].ID).Error; err != nil {
		t.Fatalf("edit initialized content: %v", err)
	}
	retry, err := initialization.InitializeSeed(ctx, initializer, ownerID, library)
	if err != nil || retry.Outcome != initialization.OutcomeAlreadyInitialized {
		t.Fatalf("InitializeSeed(retry) = %+v, %v", retry, err)
	}
	var retainedTitle string
	if err := admin.Raw("SELECT title FROM curu.activities WHERE id = ?", library.Activities[0].ID).Scan(&retainedTitle).Error; err != nil {
		t.Fatalf("read edited content: %v", err)
	}
	if retainedTitle != editedTitle {
		t.Fatalf("edited title = %q, want %q", retainedTitle, editedTitle)
	}
	if _, err := initialization.InitializeEmpty(ctx, initializer); !errors.Is(err, initialization.ErrAlreadyInitialized) {
		t.Fatalf("InitializeEmpty(after seed) error = %v, want ErrAlreadyInitialized", err)
	}
	if err := database.CheckCompatibility(ctx, initializer); err != nil {
		t.Fatalf("database.CheckCompatibility() after initialization error = %v", err)
	}
	assertCounts(t, admin, seed.ExpectedActivities, seed.ExpectedProgressions, 1)
}

func assertCounts(t *testing.T, db *gorm.DB, activities, progressions, initializations int) {
	t.Helper()
	for query, expected := range map[string]int{
		"SELECT count(*) FROM curu.activities":              activities,
		"SELECT count(*) FROM curu.activity_progressions":   progressions,
		"SELECT count(*) FROM curu.dataset_initializations": initializations,
	} {
		var actual int
		if err := db.Raw(query).Scan(&actual).Error; err != nil {
			t.Fatalf("count query %q: %v", query, err)
		}
		if actual != expected {
			t.Fatalf("count query %q = %d, want %d", query, actual, expected)
		}
	}
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

func quoteIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}
