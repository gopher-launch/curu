package database

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/gopher-launch/curu/internal/activity"
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
	Title           string
	Summary         string
	ContentMarkdown string
	DurationSeconds *int
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
	query := r.db.WithContext(ctx).Table("curu.activities AS activities").
		Select("activities.id, activities.owner_id, activities.title, activities.summary, activities.duration_seconds").
		Where("activities.owner_id = ? AND activities.archived_at IS NULL", ownerID)
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
		return nil, fmt.Errorf("list current activities: %w", err)
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
	var row activityRow
	err := r.db.WithContext(ctx).
		Preload("Progressions", func(db *gorm.DB) *gorm.DB { return db.Order("position, id") }).
		Where("owner_id = ? AND id = ? AND archived_at IS NULL", ownerID, id).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return activity.Activity{}, activity.ErrNotFound
	}
	if err != nil {
		return activity.Activity{}, fmt.Errorf("find current activity: %w", err)
	}
	item := activity.Activity{
		ID: row.ID, OwnerID: row.OwnerID, Title: row.Title, Summary: row.Summary,
		ContentMarkdown: row.ContentMarkdown, DurationSeconds: row.DurationSeconds,
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

func escapeLike(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	return strings.ReplaceAll(value, `_`, `\_`)
}
