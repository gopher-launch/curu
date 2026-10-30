// Package recovery captures application-owned evidence for an external
// PostgreSQL backup and restore exercise.
package recovery

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash"

	"github.com/gopher-launch/curu/internal/database"
	"gorm.io/gorm"
)

// Manifest is a content-free summary of all application data delivered by the
// current schema. A manifest captured immediately before a backup must exactly
// match one captured from its isolated restore target.
type Manifest struct {
	Format                 int    `json:"format"`
	SchemaVersion          int    `json:"schema_version"`
	DatasetInitializations int64  `json:"dataset_initializations"`
	Activities             int64  `json:"activities"`
	ArchivedActivities     int64  `json:"archived_activities"`
	ActivityOwners         int64  `json:"activity_owners"`
	ActivityProgressions   int64  `json:"activity_progressions"`
	ApplicationDataSHA256  string `json:"application_data_sha256"`
}

type digestRow struct {
	Value string
}

type digestQuery struct {
	name string
	sql  string
}

// Capture verifies schema compatibility and hashes every application-owned
// field in a deterministic order. It does not expose activity content, owner
// identifiers, credentials, or initialization timestamps in its output.
func Capture(ctx context.Context, db *gorm.DB) (Manifest, error) {
	var manifest Manifest
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		manifest, err = captureSnapshot(ctx, tx)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func captureSnapshot(ctx context.Context, db *gorm.DB) (Manifest, error) {
	if err := database.CheckCompatibility(ctx, db); err != nil {
		return Manifest{}, fmt.Errorf("check schema compatibility: %w", err)
	}

	manifest := Manifest{Format: 1, SchemaVersion: database.LatestSchemaVersion}
	counts := []struct {
		destination *int64
		query       string
	}{
		{&manifest.DatasetInitializations, "SELECT count(*) FROM curu.dataset_initializations"},
		{&manifest.Activities, "SELECT count(*) FROM curu.activities"},
		{&manifest.ArchivedActivities, "SELECT count(*) FROM curu.activities WHERE archived_at IS NOT NULL"},
		{&manifest.ActivityOwners, "SELECT count(DISTINCT owner_id) FROM curu.activities"},
		{&manifest.ActivityProgressions, "SELECT count(*) FROM curu.activity_progressions"},
	}
	for _, item := range counts {
		if err := db.WithContext(ctx).Raw(item.query).Scan(item.destination).Error; err != nil {
			return Manifest{}, fmt.Errorf("capture recovery count: %w", err)
		}
	}

	digest := sha256.New()
	queries := []digestQuery{
		{"schema_migrations", `SELECT jsonb_build_array(version, name, checksum,
			to_char(applied_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'))::text AS value
			FROM curu.schema_migrations ORDER BY version`},
		{"dataset_initializations", `SELECT jsonb_build_array(singleton_id, kind, seed_identity, seed_version, revision,
			to_char(initialized_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'))::text AS value
			FROM curu.dataset_initializations ORDER BY singleton_id`},
		{"activities", `SELECT jsonb_build_array(id, owner_id, source_identity, title, summary, content_markdown,
			duration_seconds, revision,
			to_char(archived_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
			to_char(created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
			to_char(updated_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'))::text AS value
			FROM curu.activities ORDER BY id`},
		{"activity_progressions", `SELECT jsonb_build_array(id, activity_id, position, title, content_markdown,
			duration_seconds,
			to_char(created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
			to_char(updated_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'))::text AS value
			FROM curu.activity_progressions ORDER BY id`},
	}
	for _, query := range queries {
		var rows []digestRow
		if err := db.WithContext(ctx).Raw(query.sql).Scan(&rows).Error; err != nil {
			return Manifest{}, fmt.Errorf("capture %s recovery data: %w", query.name, err)
		}
		writeDigestValue(digest, query.name)
		for _, row := range rows {
			writeDigestValue(digest, row.Value)
		}
	}
	manifest.ApplicationDataSHA256 = hex.EncodeToString(digest.Sum(nil))
	return manifest, nil
}

func writeDigestValue(digest hash.Hash, value string) {
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(value)))
	_, _ = digest.Write(size[:])
	_, _ = digest.Write([]byte(value))
}
