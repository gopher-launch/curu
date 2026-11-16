package database

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/gopher-launch/curu/internal/activity"
	"github.com/gopher-launch/curu/internal/seed"
	"github.com/gopher-launch/curu/internal/session"
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
	created, err := repository.Create(ctx, ownerID, activity.Input{Title: "User activity", Phase: "Warmup", Theme: "Passing", Tags: []string{"ball", "handling"}, Progressions: []activity.ProgressionInput{{Title: "Harder", ContentMarkdown: "Add pressure."}}})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.Revision != 1 || created.Phase != "Warmup" || len(created.Tags) != 2 || len(created.Progressions) != 1 {
		t.Fatalf("Create() result = %+v", created)
	}
	updated, err := repository.Update(ctx, ownerID, created.ID, created.Revision, activity.Input{Title: "Renamed activity", Phase: "Main", Tags: []string{"contact"}})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.ID != created.ID || updated.Revision != 2 || updated.Title != "Renamed activity" || len(updated.Progressions) != 0 {
		t.Fatalf("Update() result = %+v", updated)
	}
	if _, err := repository.Update(ctx, ownerID, created.ID, created.Revision, activity.Input{Title: "Stale overwrite"}); !errors.Is(err, activity.ErrConflict) {
		t.Fatalf("stale Update() error = %v, want conflict", err)
	}
	current, err := repository.FindCurrent(ctx, ownerID, created.ID)
	if err != nil || current.Title != "Renamed activity" || current.Revision != 2 {
		t.Fatalf("current activity after stale update = %+v, error = %v", current, err)
	}
	draftInput := activity.Input{Title: "Stale submitted title", Summary: "Exact submitted summary", ContentMarkdown: "Exact submitted content", Tags: []string{"recovered"}, Progressions: []activity.ProgressionInput{{Title: "Harder", ContentMarkdown: "Exact progression"}}}
	draft, err := repository.SaveRecoveryDraft(ctx, ownerID, created.ID, created.Revision, draftInput)
	if err != nil {
		t.Fatalf("SaveRecoveryDraft() error = %v", err)
	}
	loadedDraft, err := repository.FindRecoveryDraft(ctx, ownerID, draft.ID)
	if err != nil || loadedDraft.ActivityID != created.ID || loadedDraft.BaseRevision != created.Revision || loadedDraft.Input.Title != draftInput.Title || loadedDraft.Input.ContentMarkdown != draftInput.ContentMarkdown || len(loadedDraft.Input.Progressions) != 1 || loadedDraft.Input.Progressions[0].ContentMarkdown != "Exact progression" {
		t.Fatalf("FindRecoveryDraft() = %+v, error = %v", loadedDraft, err)
	}
	if _, err := repository.FindRecoveryDraft(ctx, "55555555-5555-4555-8555-555555555555", draft.ID); !errors.Is(err, activity.ErrNotFound) {
		t.Fatalf("other-owner FindRecoveryDraft() error = %v, want not found", err)
	}
	if drafts, err := repository.ListRecoveryDrafts(ctx, ownerID); err != nil || len(drafts) != 1 || drafts[0].ID != draft.ID || drafts[0].Title != draftInput.Title {
		t.Fatalf("ListRecoveryDrafts() = %+v, error = %v", drafts, err)
	}
	replaced, err := repository.SaveRecoveryDraft(ctx, ownerID, created.ID, current.Revision, activity.Input{Title: "Replacement recovery"})
	if err != nil || replaced.ID != draft.ID || replaced.Input.Title != "Replacement recovery" {
		t.Fatalf("replacement SaveRecoveryDraft() = %+v, error = %v", replaced, err)
	}
	if err := repository.DiscardRecoveryDraft(ctx, ownerID, draft.ID); err != nil {
		t.Fatalf("DiscardRecoveryDraft() error = %v", err)
	}
	if _, err := repository.FindRecoveryDraft(ctx, ownerID, draft.ID); !errors.Is(err, activity.ErrNotFound) {
		t.Fatalf("discarded FindRecoveryDraft() error = %v, want not found", err)
	}
	if _, err := repository.Archive(ctx, ownerID, created.ID, created.Revision); !errors.Is(err, activity.ErrConflict) {
		t.Fatalf("stale Archive() error = %v, want conflict", err)
	}
	archived, err := repository.Archive(ctx, ownerID, created.ID, current.Revision)
	if err != nil || !archived.Archived || archived.Revision != 3 || archived.Title != current.Title || len(archived.Progressions) != len(current.Progressions) {
		t.Fatalf("Archive() result = %+v, error = %v", archived, err)
	}
	if results, err := repository.ListCurrent(ctx, ownerID, "Renamed activity"); err != nil || len(results) != 0 {
		t.Fatalf("ListCurrent() after archive = %+v, error = %v", results, err)
	}
	if results, err := repository.ListArchived(ctx, ownerID, "Renamed activity"); err != nil || len(results) != 1 || results[0].ID != created.ID {
		t.Fatalf("ListArchived() after archive = %+v, error = %v", results, err)
	}
	if _, err := repository.Restore(ctx, ownerID, created.ID, current.Revision); !errors.Is(err, activity.ErrConflict) {
		t.Fatalf("stale Restore() error = %v, want conflict", err)
	}
	restored, err := repository.Restore(ctx, ownerID, created.ID, archived.Revision)
	if err != nil || restored.Archived || restored.Revision != 4 || restored.ID != created.ID || restored.Title != current.Title {
		t.Fatalf("Restore() result = %+v, error = %v", restored, err)
	}
	if results, err := repository.ListArchived(ctx, "55555555-5555-4555-8555-555555555555", "Renamed activity"); err != nil || len(results) != 0 {
		t.Fatalf("ListArchived() crossed owner boundary = %+v, error = %v", results, err)
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
	sessions, err := NewSessionRepository(runtime)
	if err != nil {
		t.Fatalf("NewSessionRepository() error = %v", err)
	}
	createdSession, err := sessions.Create(ctx, ownerID, session.Input{Title: "Undated plan", GroupContext: "U12", Intent: "Support", Blocks: []session.BlockInput{{Name: "Arrival", Notes: "Choose freely", ObservationLens: "Scanning"}}})
	if err != nil || createdSession.Date != nil || createdSession.Revision != 1 || len(createdSession.Blocks) != 1 {
		t.Fatalf("session Create() = %+v, error = %v", createdSession, err)
	}
	updatedSession, err := sessions.Update(ctx, ownerID, createdSession.ID, createdSession.Revision, session.Input{Title: "Reopened plan", Blocks: []session.BlockInput{{Name: "Warmup"}, {Name: "Main", Intent: "Decision making"}}})
	if err != nil || updatedSession.Revision != 2 || len(updatedSession.Blocks) != 2 || updatedSession.Blocks[1].Position != 1 {
		t.Fatalf("session Update() = %+v, error = %v", updatedSession, err)
	}
	snapshotSource, err := repository.Update(ctx, ownerID, restored.ID, restored.Revision, activity.Input{
		Title: "Snapshot source", Summary: "Coherent recommendation", ContentMarkdown: "## Setup\nUse the whole space.",
		DurationSeconds: intPointer(90), Phase: "Warmup", Theme: "Handling", Tags: []string{"ball", "scan"},
		Progressions: []activity.ProgressionInput{{Title: "Add pressure", ContentMarkdown: "Close the space.", DurationSeconds: intPointer(30)}},
	})
	if err != nil {
		t.Fatalf("prepare snapshot source: %v", err)
	}
	firstSnapshot, err := sessions.AddPlacement(ctx, ownerID, updatedSession.ID, updatedSession.Blocks[0].ID, updatedSession.Revision, snapshotSource.ID)
	if err != nil || firstSnapshot.Revision != 3 || len(firstSnapshot.Blocks[0].Placements) != 1 {
		t.Fatalf("first AddPlacement() = %+v, error = %v", firstSnapshot, err)
	}
	secondSnapshot, err := sessions.AddPlacement(ctx, ownerID, firstSnapshot.ID, firstSnapshot.Blocks[0].ID, firstSnapshot.Revision, snapshotSource.ID)
	if err != nil || secondSnapshot.Revision != 4 || len(secondSnapshot.Blocks[0].Placements) != 2 || secondSnapshot.Blocks[0].Placements[1].Position != 1 {
		t.Fatalf("second AddPlacement() = %+v, error = %v", secondSnapshot, err)
	}
	placement := secondSnapshot.Blocks[0].Placements[0]
	if placement.SourceActivityID != snapshotSource.ID || placement.SourceActivityRevision != snapshotSource.Revision || placement.SourceIdentity != "" ||
		placement.Title != snapshotSource.Title || placement.Summary != snapshotSource.Summary || placement.ContentMarkdown != snapshotSource.ContentMarkdown ||
		placement.RecommendedDurationSeconds == nil || *placement.RecommendedDurationSeconds != 90 || placement.Phase != snapshotSource.Phase || placement.Theme != snapshotSource.Theme || !slices.Equal(placement.Tags, snapshotSource.Tags) ||
		len(placement.Progressions) != 1 || placement.Progressions[0].ContentMarkdown != snapshotSource.Progressions[0].ContentMarkdown || placement.Progressions[0].RecommendedDurationSeconds == nil || *placement.Progressions[0].RecommendedDurationSeconds != 30 {
		t.Fatalf("placement snapshot = %+v, want coherent copy of %+v", placement, snapshotSource)
	}
	if err := runtime.Exec("UPDATE curu.session_activity_placements SET title = 'Plan-local adaptation' WHERE id = ?", placement.ID).Error; err != nil {
		t.Fatalf("adapt placement snapshot: %v", err)
	}
	var sourceTitle string
	if err := runtime.Raw("SELECT title FROM curu.activities WHERE id = ?", snapshotSource.ID).Scan(&sourceTitle).Error; err != nil || sourceTitle != snapshotSource.Title {
		t.Fatalf("source after placement adaptation = %q, error = %v", sourceTitle, err)
	}
	// Saving ordinary session/block fields retains stable block identities and
	// their placements; only an explicitly removed block cascades its snapshots.
	retained, err := sessions.Update(ctx, ownerID, secondSnapshot.ID, secondSnapshot.Revision, session.Input{Title: "Snapshot plan", Blocks: []session.BlockInput{
		{ID: secondSnapshot.Blocks[0].ID, Name: "Warmup edited"},
		{ID: secondSnapshot.Blocks[1].ID, Name: "Main", Intent: "Decision making"},
	}})
	if err != nil || retained.Revision != 5 || len(retained.Blocks[0].Placements) != 2 || retained.Blocks[0].Placements[0].Title != "Plan-local adaptation" {
		t.Fatalf("session save retained placements = %+v, error = %v", retained, err)
	}
	// Structural editor actions save entered aggregate fields and placement
	// notes atomically while preserving plan-owned duration and source data.
	replacementSource, err := repository.Create(ctx, ownerID, activity.Input{Title: "Replacement source", ContentMarkdown: "Fresh source content", Tags: []string{}})
	if err != nil {
		t.Fatalf("create replacement source: %v", err)
	}
	actionSession, err := sessions.Create(ctx, ownerID, session.Input{Title: "Action plan", Blocks: []session.BlockInput{{Name: "Arrival"}, {Name: "Warmup"}}})
	if err != nil {
		t.Fatalf("create action session: %v", err)
	}
	actionSession, err = sessions.AddPlacement(ctx, ownerID, actionSession.ID, actionSession.Blocks[0].ID, actionSession.Revision, snapshotSource.ID)
	if err != nil {
		t.Fatalf("add action placement: %v", err)
	}
	actionPlacement := actionSession.Blocks[0].Placements[0]
	if err := runtime.Exec("UPDATE curu.session_activity_placements SET local_notes='Retain this note', duration_seconds=45 WHERE id=?", actionPlacement.ID).Error; err != nil {
		t.Fatalf("set plan-local placement properties: %v", err)
	}
	actionInput := session.Input{Title: "Action plan edited", AllocationSeconds: intPointer(600), Blocks: []session.BlockInput{
		{ID: actionSession.Blocks[0].ID, Name: "Arrival edited", AllocationSeconds: intPointer(180), Placements: []session.PlacementInput{{ID: actionPlacement.ID, LocalNotes: "Unsaved note saved", DurationSeconds: intPointer(75)}}},
		{ID: actionSession.Blocks[1].ID, Name: "Warmup edited"},
	}}
	actionSession, err = sessions.ApplyPlacementAction(ctx, ownerID, actionSession.ID, actionSession.Revision, actionInput, session.PlacementAction{Kind: session.ReplacePlacementAction, PlacementID: actionPlacement.ID, ActivityID: replacementSource.ID})
	if err != nil || actionSession.Title != "Action plan edited" || actionSession.Blocks[0].Name != "Arrival edited" || len(actionSession.Blocks[0].Placements) != 1 {
		t.Fatalf("replace placement action = %+v, error = %v", actionSession, err)
	}
	replacedPlacement := actionSession.Blocks[0].Placements[0]
	if actionSession.AllocationSeconds == nil || *actionSession.AllocationSeconds != 600 || actionSession.Blocks[0].AllocationSeconds == nil || *actionSession.Blocks[0].AllocationSeconds != 180 || replacedPlacement.SourceActivityID != replacementSource.ID || replacedPlacement.ContentMarkdown != replacementSource.ContentMarkdown || replacedPlacement.LocalNotes != "Unsaved note saved" || replacedPlacement.DurationSeconds == nil || *replacedPlacement.DurationSeconds != 75 {
		t.Fatalf("replaced placement did not preserve local properties: %+v", replacedPlacement)
	}
	actionInput.Blocks[0].Placements[0].LocalNotes = replacedPlacement.LocalNotes
	actionSession, err = sessions.ApplyPlacementAction(ctx, ownerID, actionSession.ID, actionSession.Revision, actionInput, session.PlacementAction{Kind: session.MovePlacementAction, PlacementID: replacedPlacement.ID, TargetBlockID: actionSession.Blocks[1].ID})
	if err != nil || len(actionSession.Blocks[0].Placements) != 0 || len(actionSession.Blocks[1].Placements) != 1 || actionSession.Blocks[1].Placements[0].LocalNotes != "Unsaved note saved" || actionSession.Blocks[1].Placements[0].DurationSeconds == nil || *actionSession.Blocks[1].Placements[0].DurationSeconds != 75 {
		t.Fatalf("move placement action = %+v, error = %v", actionSession, err)
	}
	moved := actionSession.Blocks[1].Placements[0]
	if _, err := sessions.ApplyPlacementAction(ctx, ownerID, actionSession.ID, actionSession.Revision-1, actionInput, session.PlacementAction{Kind: session.MovePlacementAction, PlacementID: moved.ID, TargetBlockID: actionSession.Blocks[0].ID}); !errors.Is(err, session.ErrConflict) {
		t.Fatalf("stale move error = %v, want conflict", err)
	}
	moveTruth, err := sessions.Find(ctx, ownerID, actionSession.ID, false)
	if err != nil || len(moveTruth.Blocks[0].Placements) != 0 || len(moveTruth.Blocks[1].Placements) != 1 || moveTruth.Revision != actionSession.Revision {
		t.Fatalf("truth after stale move = %+v, error = %v", moveTruth, err)
	}
	actionInput.Blocks[0].Placements = nil
	actionInput.Blocks[1].Placements = []session.PlacementInput{{ID: moved.ID, LocalNotes: moved.LocalNotes}}
	actionSession, err = sessions.ApplyPlacementAction(ctx, ownerID, actionSession.ID, actionSession.Revision, actionInput, session.PlacementAction{Kind: session.RemovePlacementAction, PlacementID: moved.ID})
	if err != nil || len(actionSession.Blocks[1].Placements) != 0 {
		t.Fatalf("remove placement action = %+v, error = %v", actionSession, err)
	}
	if sourceAfterRemoval, findErr := repository.FindCurrent(ctx, ownerID, replacementSource.ID); findErr != nil || sourceAfterRemoval.Title != replacementSource.Title {
		t.Fatalf("library source after plan removal = %+v, error = %v", sourceAfterRemoval, findErr)
	}
	sessionDraft, err := sessions.SaveRecoveryDraft(ctx, ownerID, actionSession.ID, actionSession.Revision-1, actionSession, session.PlacementAction{Kind: session.RemovePlacementAction, PlacementID: moved.ID}, false)
	if err != nil || sessionDraft.Plan.Title != actionSession.Title || sessionDraft.Action.Kind != session.RemovePlacementAction {
		t.Fatalf("SaveRecoveryDraft() = %+v, error = %v", sessionDraft, err)
	}
	replacedSessionDraft, err := sessions.SaveRecoveryDraft(ctx, ownerID, actionSession.ID, actionSession.Revision, actionSession, session.PlacementAction{Kind: session.MovePlacementAction, PlacementID: moved.ID, TargetBlockID: actionSession.Blocks[0].ID}, true)
	if err != nil || replacedSessionDraft.ID != sessionDraft.ID || replacedSessionDraft.BaseRevision != actionSession.Revision || replacedSessionDraft.Action.Kind != session.MovePlacementAction || !replacedSessionDraft.CurrentArchived {
		t.Fatalf("replacement SaveRecoveryDraft() = %+v, error = %v", replacedSessionDraft, err)
	}
	sessionDraft = replacedSessionDraft
	loadedSessionDraft, err := sessions.FindRecoveryDraft(ctx, ownerID, sessionDraft.ID)
	if err != nil || loadedSessionDraft.ID != sessionDraft.ID || loadedSessionDraft.Plan.Title != actionSession.Title || !loadedSessionDraft.CurrentArchived {
		t.Fatalf("FindRecoveryDraft() = %+v, error = %v", loadedSessionDraft, err)
	}
	if _, err := sessions.FindRecoveryDraft(ctx, "55555555-5555-4555-8555-555555555555", sessionDraft.ID); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("other-owner FindRecoveryDraft() error = %v", err)
	}
	if drafts, err := sessions.ListRecoveryDrafts(ctx, ownerID); err != nil || len(drafts) != 1 || drafts[0].ID != sessionDraft.ID {
		t.Fatalf("ListRecoveryDrafts() = %+v, error = %v", drafts, err)
	}
	if err := sessions.DiscardRecoveryDraft(ctx, ownerID, sessionDraft.ID); err != nil {
		t.Fatalf("DiscardRecoveryDraft() error = %v", err)
	}
	// Reordering uses the same aggregate revision and transaction as entered
	// editor fields. Boundaries reject without advancing the revision, and a
	// repeated request carrying the consumed revision is stale.
	reorderSession, err := sessions.Create(ctx, ownerID, session.Input{Title: "Reorder plan", Blocks: []session.BlockInput{{Name: "First"}, {Name: "Second"}, {Name: "Third"}}})
	if err != nil {
		t.Fatalf("create reorder session: %v", err)
	}
	for range 3 {
		reorderSession, err = sessions.AddPlacement(ctx, ownerID, reorderSession.ID, reorderSession.Blocks[0].ID, reorderSession.Revision, replacementSource.ID)
		if err != nil {
			t.Fatalf("add reorder placement: %v", err)
		}
	}
	blockRevision := reorderSession.Revision
	reorderInput := sessionInputForTest(reorderSession)
	reorderInput.Title = "Reorder plan edited"
	reorderSession, err = sessions.ApplyPlacementAction(ctx, ownerID, reorderSession.ID, blockRevision, reorderInput, session.PlacementAction{Kind: session.MoveBlockUpAction, BlockID: reorderSession.Blocks[1].ID})
	if err != nil || reorderSession.Revision != blockRevision+1 || reorderSession.Title != "Reorder plan edited" || reorderSession.Blocks[0].Name != "Second" || reorderSession.Blocks[1].Name != "First" || reorderSession.Blocks[2].Name != "Third" {
		t.Fatalf("block reorder = %+v, error = %v", reorderSession, err)
	}
	if _, err := sessions.ApplyPlacementAction(ctx, ownerID, reorderSession.ID, blockRevision, reorderInput, session.PlacementAction{Kind: session.MoveBlockUpAction, BlockID: reorderSession.Blocks[1].ID}); !errors.Is(err, session.ErrConflict) {
		t.Fatalf("duplicate reorder error = %v, want conflict", err)
	}
	boundaryInput := sessionInputForTest(reorderSession)
	boundaryInput.Title = "Must roll back"
	if _, err := sessions.ApplyPlacementAction(ctx, ownerID, reorderSession.ID, reorderSession.Revision, boundaryInput, session.PlacementAction{Kind: session.MoveBlockUpAction, BlockID: reorderSession.Blocks[0].ID}); !errors.Is(err, session.ErrInvalidInput) {
		t.Fatalf("block boundary error = %v, want invalid input", err)
	}
	afterBoundary, err := sessions.Find(ctx, ownerID, reorderSession.ID, false)
	if err != nil || afterBoundary.Revision != reorderSession.Revision || afterBoundary.Title != reorderSession.Title {
		t.Fatalf("block boundary did not roll back = %+v, error = %v", afterBoundary, err)
	}
	placementBlock := afterBoundary.Blocks[1]
	middlePlacement := placementBlock.Placements[1]
	reorderSession, err = sessions.ApplyPlacementAction(ctx, ownerID, afterBoundary.ID, afterBoundary.Revision, sessionInputForTest(afterBoundary), session.PlacementAction{Kind: session.MovePlacementUpAction, PlacementID: middlePlacement.ID})
	if err != nil || reorderSession.Blocks[1].Placements[0].ID != middlePlacement.ID || reorderSession.Blocks[1].Placements[1].ID != placementBlock.Placements[0].ID {
		t.Fatalf("placement reorder = %+v, error = %v", reorderSession, err)
	}
	placementRevision := reorderSession.Revision
	if _, err := sessions.ApplyPlacementAction(ctx, ownerID, reorderSession.ID, placementRevision, sessionInputForTest(reorderSession), session.PlacementAction{Kind: session.MovePlacementUpAction, PlacementID: middlePlacement.ID}); !errors.Is(err, session.ErrInvalidInput) {
		t.Fatalf("placement boundary error = %v, want invalid input", err)
	}
	afterPlacementBoundary, err := sessions.Find(ctx, ownerID, reorderSession.ID, false)
	if err != nil || afterPlacementBoundary.Revision != placementRevision || afterPlacementBoundary.Blocks[1].Placements[0].ID != middlePlacement.ID {
		t.Fatalf("placement boundary did not roll back = %+v, error = %v", afterPlacementBoundary, err)
	}
	if err := admin.Exec("DELETE FROM curu.sessions WHERE id IN (?, ?)", actionSession.ID, reorderSession.ID).Error; err != nil {
		t.Fatalf("remove isolated structural-action fixtures: %v", err)
	}
	beforeRollback := retained
	if _, err := sessions.AddPlacement(ctx, ownerID, retained.ID, "77777777-7777-4777-8777-777777777777", retained.Revision, snapshotSource.ID); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("AddPlacement() invalid block error = %v, want not found", err)
	}
	afterRollback, err := sessions.Find(ctx, ownerID, retained.ID, false)
	if err != nil || afterRollback.Revision != beforeRollback.Revision || len(afterRollback.Blocks[0].Placements) != 2 {
		t.Fatalf("session after placement rollback = %+v, error = %v", afterRollback, err)
	}
	if err := admin.Exec("DELETE FROM curu.activities WHERE id = ?", snapshotSource.ID).Error; err != nil {
		t.Fatalf("remove source activity fixture: %v", err)
	}
	afterSourceRemoval, err := sessions.Find(ctx, ownerID, retained.ID, false)
	if err != nil || len(afterSourceRemoval.Blocks[0].Placements) != 2 || afterSourceRemoval.Blocks[0].Placements[0].SourceActivityID != snapshotSource.ID || afterSourceRemoval.Blocks[0].Placements[0].Title != "Plan-local adaptation" {
		t.Fatalf("snapshot after source removal = %+v, error = %v", afterSourceRemoval, err)
	}
	if _, err := sessions.Update(ctx, ownerID, createdSession.ID, createdSession.Revision, session.Input{Title: "Stale", Blocks: []session.BlockInput{{Name: "Lost"}}}); !errors.Is(err, session.ErrConflict) {
		t.Fatalf("stale session Update() error = %v", err)
	}
	currentSession, err := sessions.Find(ctx, ownerID, createdSession.ID, false)
	if err != nil || currentSession.Title != "Snapshot plan" || len(currentSession.Blocks) != 2 || len(currentSession.Blocks[0].Placements) != 2 {
		t.Fatalf("session after stale update = %+v, error = %v", currentSession, err)
	}
	archivedSession, err := sessions.Archive(ctx, ownerID, createdSession.ID, currentSession.Revision, true)
	if err != nil || !archivedSession.Archived || len(archivedSession.Blocks) != 2 {
		t.Fatalf("session Archive() = %+v, error = %v", archivedSession, err)
	}
	if _, err := sessions.Update(ctx, ownerID, createdSession.ID, currentSession.Revision, session.Input{Title: "Submitted after archive", Blocks: []session.BlockInput{{Name: "Preserved block"}}}); !errors.Is(err, session.ErrArchivedConflict) || !errors.Is(err, session.ErrConflict) {
		t.Fatalf("session Update() after concurrent archive error = %v, want archived conflict", err)
	}
	archivedTruth, err := sessions.Find(ctx, ownerID, createdSession.ID, true)
	if err != nil || archivedTruth.Title != "Snapshot plan" || len(archivedTruth.Blocks) != 2 || len(archivedTruth.Blocks[0].Placements) != 2 {
		t.Fatalf("archived session after stale update = %+v, error = %v", archivedTruth, err)
	}
	if currentItems, err := sessions.List(ctx, ownerID, false); err != nil || len(currentItems) != 0 {
		t.Fatalf("current sessions after archive = %+v, error = %v", currentItems, err)
	}
	restoredSession, err := sessions.Archive(ctx, ownerID, createdSession.ID, archivedSession.Revision, false)
	if err != nil || restoredSession.Archived || restoredSession.Revision != 7 || len(restoredSession.Blocks) != 2 || len(restoredSession.Blocks[0].Placements) != 2 {
		t.Fatalf("session Restore() = %+v, error = %v", restoredSession, err)
	}
	if _, err := sessions.Find(ctx, "55555555-5555-4555-8555-555555555555", createdSession.ID, false); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("other-owner session Find() error = %v", err)
	}
	if _, err := sessions.Update(ctx, "55555555-5555-4555-8555-555555555555", createdSession.ID, restoredSession.Revision, session.Input{Title: "Other owner"}); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("other-owner session Update() error = %v, want not found", err)
	}
	if _, err := sessions.Update(ctx, ownerID, "66666666-6666-4666-8666-666666666666", 1, session.Input{Title: "Missing"}); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("missing session Update() error = %v, want not found", err)
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
	if err := runtime.Exec("DELETE FROM curu.sessions WHERE id = ?", createdSession.ID).Error; err == nil {
		t.Fatal("runtime role permanently deleted a session")
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

func intPointer(value int) *int { return &value }

func sessionInputForTest(item session.Session) session.Input {
	input := session.Input{Title: item.Title, Date: item.Date, GroupContext: item.GroupContext, Intent: item.Intent, AllocationSeconds: item.AllocationSeconds}
	for _, block := range item.Blocks {
		blockInput := session.BlockInput{ID: block.ID, Name: block.Name, Intent: block.Intent, Notes: block.Notes, ObservationLens: block.ObservationLens, AllocationSeconds: block.AllocationSeconds}
		for _, placement := range block.Placements {
			blockInput.Placements = append(blockInput.Placements, session.PlacementInput{ID: placement.ID, LocalNotes: placement.LocalNotes, DurationSeconds: placement.DurationSeconds})
		}
		input.Blocks = append(input.Blocks, blockInput)
	}
	return input
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
