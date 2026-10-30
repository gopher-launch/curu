package database_test

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gopher-launch/curu/internal/database"
	"github.com/gopher-launch/curu/internal/initialization"
	"github.com/gopher-launch/curu/internal/recovery"
	"github.com/gopher-launch/curu/internal/seed"
)

func TestPostgresBackupRestoreRecovery(t *testing.T) {
	adminURL := os.Getenv("CURU_TEST_DATABASE_URL")
	if adminURL == "" {
		t.Skip("CURU_TEST_DATABASE_URL is not set; skipping disposable PostgreSQL recovery check")
	}
	pgDump, err := exec.LookPath("pg_dump")
	if err != nil {
		t.Fatal("pg_dump is required when CURU_TEST_DATABASE_URL selects the recovery exercise")
	}
	pgRestore, err := exec.LookPath("pg_restore")
	if err != nil {
		t.Fatal("pg_restore is required when CURU_TEST_DATABASE_URL selects the recovery exercise")
	}

	ctx := context.Background()
	admin, err := database.Open(ctx, adminURL)
	if err != nil {
		t.Fatalf("database.Open(admin) error = %v", err)
	}
	defer database.Close(admin)

	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	sourceName := "curu_recovery_source_" + suffix
	restoreName := "curu_recovery_restore_" + suffix
	runtimeRole := "curu_recovery_runtime_" + suffix
	if len(runtimeRole) > 63 {
		runtimeRole = runtimeRole[:63]
	}
	const runtimePassword = "curu-recovery-runtime-password"
	cleanup := func() {
		for _, name := range []string{sourceName, restoreName} {
			admin.Exec("SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = ? AND pid <> pg_backend_pid()", name)
			admin.Exec("DROP DATABASE IF EXISTS " + quoteIdentifier(name))
		}
		admin.Exec("DROP ROLE IF EXISTS " + quoteIdentifier(runtimeRole))
	}
	cleanup()
	defer cleanup()
	if err := admin.Exec(fmt.Sprintf("CREATE ROLE %s LOGIN NOINHERIT PASSWORD '%s'", quoteIdentifier(runtimeRole), runtimePassword)).Error; err != nil {
		t.Fatalf("create recovery runtime role: %v", err)
	}
	for _, name := range []string{sourceName, restoreName} {
		if err := admin.Exec("CREATE DATABASE " + quoteIdentifier(name)).Error; err != nil {
			t.Fatalf("create isolated database %s: %v", name, err)
		}
	}

	sourceURL := databaseURLWithName(t, adminURL, sourceName)
	restoreURL := databaseURLWithName(t, adminURL, restoreName)
	for _, dsn := range []string{sourceURL, restoreURL} {
		db, err := database.Open(ctx, dsn)
		if err != nil {
			t.Fatalf("open isolated database: %v", err)
		}
		name := currentDatabase(t, db)
		for _, statement := range []string{
			"REVOKE TEMPORARY ON DATABASE " + quoteIdentifier(name) + " FROM PUBLIC",
			"REVOKE CREATE ON DATABASE " + quoteIdentifier(name) + " FROM PUBLIC",
			"REVOKE CREATE ON SCHEMA public FROM PUBLIC",
		} {
			if err := db.Exec(statement).Error; err != nil {
				database.Close(db)
				t.Fatalf("restrict isolated database: %v", err)
			}
		}
		database.Close(db)
	}

	source, err := database.Open(ctx, sourceURL)
	if err != nil {
		t.Fatalf("open recovery source: %v", err)
	}
	if err := database.Migrate(ctx, source, runtimeRole); err != nil {
		t.Fatalf("migrate recovery source: %v", err)
	}
	library, err := seed.Load()
	if err != nil {
		t.Fatalf("seed.Load() error = %v", err)
	}
	const ownerID = "88888888-8888-4888-8888-888888888888"
	if _, err := initialization.InitializeSeed(ctx, source, ownerID, library); err != nil {
		t.Fatalf("initialize recovery source: %v", err)
	}
	if err := source.Exec(`UPDATE curu.activities
		SET title = 'Recovery exercise edit', revision = revision + 1, updated_at = current_timestamp
		WHERE id = ?`, library.Activities[0].ID).Error; err != nil {
		t.Fatalf("edit recovery source: %v", err)
	}
	if err := source.Exec(`UPDATE curu.activities
		SET archived_at = current_timestamp, revision = revision + 1, updated_at = current_timestamp
		WHERE id = ?`, library.Activities[1].ID).Error; err != nil {
		t.Fatalf("archive recovery source activity: %v", err)
	}
	expected, err := recovery.Capture(ctx, source)
	if err != nil {
		t.Fatalf("capture source recovery manifest: %v", err)
	}
	if err := database.Close(source); err != nil {
		t.Fatalf("close recovery source: %v", err)
	}
	if expected.Activities != seed.ExpectedActivities || expected.ActivityProgressions != seed.ExpectedProgressions ||
		expected.DatasetInitializations != 1 || expected.ArchivedActivities != 1 || expected.ActivityOwners != 1 {
		t.Fatalf("source manifest = %+v, want complete edited seed fixture", expected)
	}

	archive := filepath.Join(t.TempDir(), "curu.dump")
	runPostgresTool(t, pgDump, "--format=custom", "--no-owner", "--no-privileges", "--file", archive, sourceURL)
	runPostgresTool(t, pgRestore, "--exit-on-error", "--no-owner", "--no-privileges", "--dbname", restoreURL, archive)

	restoredOwner, err := database.Open(ctx, restoreURL)
	if err != nil {
		t.Fatalf("open restored database: %v", err)
	}
	if err := database.Migrate(ctx, restoredOwner, runtimeRole); err != nil {
		t.Fatalf("reconcile restored runtime grants: %v", err)
	}
	database.Close(restoredOwner)

	runtimeURL := databaseURLForRole(t, restoreURL, runtimeRole, runtimePassword)
	restored, err := database.Open(ctx, runtimeURL)
	if err != nil {
		t.Fatalf("open restored database as runtime: %v", err)
	}
	defer database.Close(restored)
	actual, err := recovery.Capture(ctx, restored)
	if err != nil {
		t.Fatalf("capture restored recovery manifest: %v", err)
	}
	if actual != expected {
		t.Fatalf("restored manifest = %+v, want %+v", actual, expected)
	}
	var edited struct {
		Title    string
		Revision int64
	}
	if err := restored.Raw("SELECT title, revision FROM curu.activities WHERE id = ?", library.Activities[0].ID).Scan(&edited).Error; err != nil {
		t.Fatalf("read restored edit: %v", err)
	}
	if edited.Title != "Recovery exercise edit" || edited.Revision != 2 {
		t.Fatalf("restored edit = %+v, want edited title at revision 2", edited)
	}
	var archived struct {
		IsArchived bool
		Revision   int64
	}
	if err := restored.Raw(`SELECT archived_at IS NOT NULL AS is_archived, revision
		FROM curu.activities WHERE id = ?`, library.Activities[1].ID).Scan(&archived).Error; err != nil {
		t.Fatalf("read restored archive state: %v", err)
	}
	if !archived.IsArchived || archived.Revision != 2 {
		t.Fatalf("restored archive state = %+v, want archived revision 2", archived)
	}
	var initializationRecord struct {
		Kind         string
		SeedIdentity string
		SeedVersion  string
	}
	if err := restored.Raw(`SELECT kind, seed_identity, seed_version
		FROM curu.dataset_initializations WHERE singleton_id = 1`).Scan(&initializationRecord).Error; err != nil {
		t.Fatalf("read restored initialization metadata: %v", err)
	}
	if initializationRecord.Kind != "seed" || initializationRecord.SeedIdentity != library.SeedIdentity ||
		initializationRecord.SeedVersion != library.SeedVersion {
		t.Fatalf("restored initialization metadata = %+v, want %s version %s", initializationRecord, library.SeedIdentity, library.SeedVersion)
	}
}

func databaseURLWithName(t *testing.T, value, name string) string {
	t.Helper()
	parsed, err := url.Parse(value)
	if err != nil {
		t.Fatalf("parse database URL: %v", err)
	}
	parsed.Path = "/" + name
	parsed.RawPath = ""
	return parsed.String()
}

func runPostgresTool(t *testing.T, tool string, arguments ...string) {
	t.Helper()
	command := exec.Command(tool, arguments...)
	output, err := command.CombinedOutput()
	if err != nil {
		// PostgreSQL tools do not echo connection URLs in ordinary diagnostics;
		// redact the supplied URLs defensively before reporting their output.
		message := string(output)
		for _, argument := range arguments {
			if strings.HasPrefix(argument, "postgres://") || strings.HasPrefix(argument, "postgresql://") {
				message = strings.ReplaceAll(message, argument, "[database URL]")
			}
		}
		t.Fatalf("%s failed: %v: %s", filepath.Base(tool), err, message)
	}
}
