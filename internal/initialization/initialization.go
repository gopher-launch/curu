// Package initialization applies the one-time dataset choice independently
// from schema migration, server startup, and ordinary runtime saves.
package initialization

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/gopher-launch/curu/internal/seed"
	"gorm.io/gorm"
)

type Kind string

const (
	KindEmpty Kind = "empty"
	KindSeed  Kind = "seed"
)

type Outcome string

const (
	OutcomeInitialized        Outcome = "initialized"
	OutcomeAlreadyInitialized Outcome = "already_initialized"
)

var (
	ErrDatasetNotEmpty    = errors.New("dataset contains unrecognized content")
	ErrAlreadyInitialized = errors.New("dataset has a different initialization record")
)

const initializationLockID int64 = 6947483992921287477

type Result struct {
	Outcome      Outcome
	Kind         Kind
	SeedIdentity string
	SeedVersion  string
}

type request struct {
	kind         Kind
	ownerID      string
	seedIdentity string
	seedVersion  string
	activities   []seed.Activity
}

type record struct {
	Kind         Kind
	SeedIdentity *string
	SeedVersion  *string
}

// InitializeEmpty records that this installation intentionally starts without
// library content. An exact completed retry is a no-op.
func InitializeEmpty(ctx context.Context, db *gorm.DB) (Result, error) {
	return initialize(ctx, db, request{kind: KindEmpty})
}

// InitializeSeed atomically inserts the supplied validated library and records
// its identity and version. Once any initialization has completed, this never
// reconciles or replaces working content.
func InitializeSeed(ctx context.Context, db *gorm.DB, ownerID string, library seed.Library) (Result, error) {
	if strings.TrimSpace(ownerID) == "" {
		return Result{}, errors.New("owner ID is required for seed initialization")
	}
	if library.SeedIdentity == "" || library.SeedVersion == "" || len(library.Activities) == 0 {
		return Result{}, errors.New("seed library identity, version, and activities are required")
	}
	return initialize(ctx, db, request{
		kind:         KindSeed,
		ownerID:      ownerID,
		seedIdentity: library.SeedIdentity,
		seedVersion:  library.SeedVersion,
		activities:   library.Activities,
	})
}

func initialize(ctx context.Context, db *gorm.DB, desired request) (Result, error) {
	result := Result{
		Outcome:      OutcomeInitialized,
		Kind:         desired.kind,
		SeedIdentity: desired.seedIdentity,
		SeedVersion:  desired.seedVersion,
	}
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", initializationLockID).Error; err != nil {
			return fmt.Errorf("lock dataset initialization: %w", err)
		}

		var existing record
		query := tx.Raw(`SELECT kind, seed_identity, seed_version
			FROM curu.dataset_initializations WHERE singleton_id = 1`).Scan(&existing)
		if query.Error != nil {
			return fmt.Errorf("read dataset initialization: %w", query.Error)
		}
		if query.RowsAffected != 0 {
			if matches(existing, desired) {
				result.Outcome = OutcomeAlreadyInitialized
				return nil
			}
			return fmt.Errorf("%w: existing kind %q", ErrAlreadyInitialized, existing.Kind)
		}

		var activityCount int64
		if err := tx.Raw("SELECT count(*) FROM curu.activities").Scan(&activityCount).Error; err != nil {
			return fmt.Errorf("inspect dataset content: %w", err)
		}
		if activityCount != 0 {
			return fmt.Errorf("%w: found %d activities without initialization metadata", ErrDatasetNotEmpty, activityCount)
		}

		if desired.kind == KindSeed {
			if err := insertLibrary(tx, desired); err != nil {
				return err
			}
		}
		if err := tx.Exec(`INSERT INTO curu.dataset_initializations
			(kind, seed_identity, seed_version) VALUES (?, ?, ?)`, desired.kind,
			nullable(desired.seedIdentity), nullable(desired.seedVersion)).Error; err != nil {
			return fmt.Errorf("record dataset initialization: %w", err)
		}
		return nil
	})
	if err != nil {
		return Result{}, err
	}
	return result, nil
}

func matches(existing record, desired request) bool {
	if existing.Kind != desired.kind {
		return false
	}
	if desired.kind == KindEmpty {
		return existing.SeedIdentity == nil && existing.SeedVersion == nil
	}
	return existing.SeedIdentity != nil && existing.SeedVersion != nil &&
		*existing.SeedIdentity == desired.seedIdentity && *existing.SeedVersion == desired.seedVersion
}

func insertLibrary(tx *gorm.DB, desired request) error {
	for _, activity := range desired.activities {
		if err := tx.Exec(`INSERT INTO curu.activities
			(id, owner_id, source_identity, title, summary, content_markdown, duration_seconds)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, activity.ID, desired.ownerID, activity.SourceIdentity,
			activity.Title, activity.Summary, activity.ContentMarkdown, activity.DurationSeconds).Error; err != nil {
			return fmt.Errorf("insert seed activity %q: %w", activity.SourceIdentity, err)
		}
		for _, progression := range activity.Progressions {
			if err := tx.Exec(`INSERT INTO curu.activity_progressions
				(id, activity_id, position, title, content_markdown, duration_seconds)
				VALUES (?, ?, ?, ?, ?, ?)`, progression.ID, activity.ID, progression.Position,
				progression.Title, progression.ContentMarkdown, progression.DurationSeconds).Error; err != nil {
				return fmt.Errorf("insert progression %q position %d: %w", activity.SourceIdentity, progression.Position, err)
			}
		}
	}
	return nil
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}
