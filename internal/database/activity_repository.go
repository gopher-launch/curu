package database

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gopher-launch/curu/internal/activity"
	"github.com/lib/pq"
	"gorm.io/gorm"
)

type ActivityRepository struct {
	db *gorm.DB
}

func NewActivityRepository(db *gorm.DB) (*ActivityRepository, error) {
	if db == nil {
		return nil, errors.New("database is required")
	}
	return &ActivityRepository{db: db}, nil
}

type activityRow struct {
	ID              string
	OwnerID         string
	SourceIdentity  *string
	Title           string
	Summary         string
	ContentMarkdown string
	DurationSeconds *int
	Phase           string
	Theme           string
	Tags            pq.StringArray `gorm:"type:text[]"`
	Revision        int64
	Progressions    []progressionRow `gorm:"foreignKey:ActivityID"`
}

func (activityRow) TableName() string { return "curu.activities" }

type progressionRow struct {
	ID              string
	ActivityID      string
	Position        int
	Title           string
	ContentMarkdown string
	DurationSeconds *int
}

func (progressionRow) TableName() string { return "curu.activity_progressions" }

func (r *ActivityRepository) ListCurrent(ctx context.Context, ownerID, search string) ([]activity.Summary, error) {
	return r.list(ctx, ownerID, search, false)
}

func (r *ActivityRepository) ListArchived(ctx context.Context, ownerID, search string) ([]activity.Summary, error) {
	return r.list(ctx, ownerID, search, true)
}

func (r *ActivityRepository) list(ctx context.Context, ownerID, search string, archived bool) ([]activity.Summary, error) {
	archivePredicate := "activities.archived_at IS NULL"
	operation := "list current activities"
	if archived {
		archivePredicate = "activities.archived_at IS NOT NULL"
		operation = "list archived activities"
	}
	query := r.db.WithContext(ctx).Table("curu.activities AS activities").
		Select("activities.id, activities.owner_id, activities.title, activities.summary, activities.duration_seconds").
		Where("activities.owner_id = ? AND "+archivePredicate, ownerID)
	if search != "" {
		pattern := "%" + escapeLike(search) + "%"
		query = query.Where(`(
			activities.title ILIKE ? ESCAPE '\' OR
			activities.summary ILIKE ? ESCAPE '\' OR
			activities.content_markdown ILIKE ? ESCAPE '\' OR
			EXISTS (
				SELECT 1 FROM curu.activity_progressions AS progressions
				WHERE progressions.activity_id = activities.id
				AND (progressions.title ILIKE ? ESCAPE '\' OR progressions.content_markdown ILIKE ? ESCAPE '\')
			)
		)`, pattern, pattern, pattern, pattern, pattern)
	}
	var rows []activityRow
	if err := query.Order("lower(activities.title), activities.title, activities.id").Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("%s: %w", operation, err)
	}
	items := make([]activity.Summary, len(rows))
	for index, row := range rows {
		items[index] = activity.Summary{
			ID: row.ID, OwnerID: row.OwnerID, Title: row.Title,
			Summary: row.Summary, DurationSeconds: row.DurationSeconds,
		}
	}
	return items, nil
}

func (r *ActivityRepository) FindCurrent(ctx context.Context, ownerID, id string) (activity.Activity, error) {
	return r.find(ctx, ownerID, id, false)
}

func (r *ActivityRepository) FindArchived(ctx context.Context, ownerID, id string) (activity.Activity, error) {
	return r.find(ctx, ownerID, id, true)
}

func (r *ActivityRepository) find(ctx context.Context, ownerID, id string, archived bool) (activity.Activity, error) {
	archivePredicate := "archived_at IS NULL"
	operation := "find current activity"
	if archived {
		archivePredicate = "archived_at IS NOT NULL"
		operation = "find archived activity"
	}
	var row activityRow
	err := r.db.WithContext(ctx).
		Preload("Progressions", func(db *gorm.DB) *gorm.DB { return db.Order("position, id") }).
		Where("owner_id = ? AND id = ? AND "+archivePredicate, ownerID, id).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return activity.Activity{}, activity.ErrNotFound
	}
	if err != nil {
		return activity.Activity{}, fmt.Errorf("%s: %w", operation, err)
	}
	item := activity.Activity{
		ID: row.ID, OwnerID: row.OwnerID, Title: row.Title, Summary: row.Summary,
		ContentMarkdown: row.ContentMarkdown, DurationSeconds: row.DurationSeconds,
		Phase: row.Phase, Theme: row.Theme, Tags: []string(row.Tags), Revision: row.Revision, Archived: archived,
		Progressions: make([]activity.Progression, len(row.Progressions)),
	}
	for index, progression := range row.Progressions {
		item.Progressions[index] = activity.Progression{
			ID: progression.ID, Position: progression.Position, Title: progression.Title,
			ContentMarkdown: progression.ContentMarkdown, DurationSeconds: progression.DurationSeconds,
		}
	}
	return item, nil
}

func (r *ActivityRepository) Create(ctx context.Context, ownerID string, input activity.Input) (activity.Activity, error) {
	id, err := newUUID()
	if err != nil {
		return activity.Activity{}, fmt.Errorf("create activity identity: %w", err)
	}
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`INSERT INTO curu.activities
			(id, owner_id, title, summary, content_markdown, duration_seconds, phase, theme, tags)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, id, ownerID, input.Title, input.Summary,
			input.ContentMarkdown, input.DurationSeconds, input.Phase, input.Theme, pq.Array(input.Tags)).Error; err != nil {
			return fmt.Errorf("insert activity: %w", err)
		}
		return replaceProgressions(tx, id, input.Progressions)
	})
	if err != nil {
		return activity.Activity{}, err
	}
	return r.FindCurrent(ctx, ownerID, id)
}

func (r *ActivityRepository) Update(ctx context.Context, ownerID, id string, expectedRevision int64, input activity.Input) (activity.Activity, error) {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Exec(`UPDATE curu.activities SET title = ?, summary = ?, content_markdown = ?,
			duration_seconds = ?, phase = ?, theme = ?, tags = ?, revision = revision + 1, updated_at = ?
			WHERE id = ? AND owner_id = ? AND archived_at IS NULL AND revision = ?`, input.Title, input.Summary,
			input.ContentMarkdown, input.DurationSeconds, input.Phase, input.Theme, pq.Array(input.Tags), time.Now().UTC(), id, ownerID, expectedRevision)
		if result.Error != nil {
			return fmt.Errorf("update activity: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			var count int64
			if err := tx.Table("curu.activities").Where("id = ? AND owner_id = ? AND archived_at IS NULL", id, ownerID).Count(&count).Error; err != nil {
				return fmt.Errorf("resolve activity update: %w", err)
			}
			if count == 0 {
				return activity.ErrNotFound
			}
			return activity.ErrConflict
		}
		if err := tx.Exec("DELETE FROM curu.activity_progressions WHERE activity_id = ?", id).Error; err != nil {
			return fmt.Errorf("replace activity progressions: %w", err)
		}
		return replaceProgressions(tx, id, input.Progressions)
	})
	if err != nil {
		return activity.Activity{}, err
	}
	return r.FindCurrent(ctx, ownerID, id)
}

func (r *ActivityRepository) Archive(ctx context.Context, ownerID, id string, expectedRevision int64) (activity.Activity, error) {
	if err := r.changeArchiveState(ctx, ownerID, id, expectedRevision, true); err != nil {
		return activity.Activity{}, err
	}
	return r.FindArchived(ctx, ownerID, id)
}

func (r *ActivityRepository) Restore(ctx context.Context, ownerID, id string, expectedRevision int64) (activity.Activity, error) {
	if err := r.changeArchiveState(ctx, ownerID, id, expectedRevision, false); err != nil {
		return activity.Activity{}, err
	}
	return r.FindCurrent(ctx, ownerID, id)
}

func (r *ActivityRepository) changeArchiveState(ctx context.Context, ownerID, id string, expectedRevision int64, archive bool) error {
	now := time.Now().UTC()
	sourcePredicate := "archived_at IS NULL"
	archiveValue := any(now)
	operation := "archive activity"
	if !archive {
		sourcePredicate = "archived_at IS NOT NULL"
		archiveValue = nil
		operation = "restore activity"
	}
	result := r.db.WithContext(ctx).Exec(`UPDATE curu.activities
		SET archived_at = ?, revision = revision + 1, updated_at = ?
		WHERE id = ? AND owner_id = ? AND `+sourcePredicate+` AND revision = ?`, archiveValue, now, id, ownerID, expectedRevision)
	if result.Error != nil {
		return fmt.Errorf("%s: %w", operation, result.Error)
	}
	if result.RowsAffected != 0 {
		return nil
	}
	var count int64
	if err := r.db.WithContext(ctx).Table("curu.activities").Where("id = ? AND owner_id = ? AND "+sourcePredicate, id, ownerID).Count(&count).Error; err != nil {
		return fmt.Errorf("resolve %s: %w", operation, err)
	}
	if count == 0 {
		return activity.ErrNotFound
	}
	return activity.ErrConflict
}

func replaceProgressions(tx *gorm.DB, activityID string, inputs []activity.ProgressionInput) error {
	for position, input := range inputs {
		id, err := newUUID()
		if err != nil {
			return fmt.Errorf("create progression identity: %w", err)
		}
		if err := tx.Exec(`INSERT INTO curu.activity_progressions
			(id, activity_id, position, title, content_markdown, duration_seconds)
			VALUES (?, ?, ?, ?, ?, ?)`, id, activityID, position, input.Title,
			input.ContentMarkdown, input.DurationSeconds).Error; err != nil {
			return fmt.Errorf("insert progression %d: %w", position, err)
		}
	}
	return nil
}

func newUUID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		value[0:4], value[4:6], value[6:8], value[8:10], value[10:16]), nil
}

func escapeLike(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	return strings.ReplaceAll(value, `_`, `\_`)
}
