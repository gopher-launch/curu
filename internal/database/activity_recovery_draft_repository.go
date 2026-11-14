package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/gopher-launch/curu/internal/activity"
	"gorm.io/gorm"
)

const maxRecoveryDraftPayloadBytes = 1 << 20

type activityRecoveryDraftPayload struct {
	Format int            `json:"format"`
	Input  activity.Input `json:"activity"`
}

type activityRecoveryDraftRow struct {
	ID           string
	OwnerID      string
	ActivityID   string
	BaseRevision int64
	Payload      json.RawMessage `gorm:"type:jsonb"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (activityRecoveryDraftRow) TableName() string { return "curu.activity_recovery_drafts" }

func (r *ActivityRepository) SaveRecoveryDraft(ctx context.Context, ownerID, activityID string, baseRevision int64, input activity.Input) (activity.RecoveryDraft, error) {
	payload, err := json.Marshal(activityRecoveryDraftPayload{Format: 1, Input: input})
	if err != nil {
		return activity.RecoveryDraft{}, fmt.Errorf("encode activity recovery draft: %w", err)
	}
	if len(payload) > maxRecoveryDraftPayloadBytes {
		return activity.RecoveryDraft{}, activity.ValidationErrors{"draft": "The recovery draft is too large to save."}
	}
	id, err := newUUID()
	if err != nil {
		return activity.RecoveryDraft{}, fmt.Errorf("create recovery draft identity: %w", err)
	}
	now := time.Now().UTC()
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Table("curu.activities").Where("id = ? AND owner_id = ?", activityID, ownerID).Count(&count).Error; err != nil {
			return fmt.Errorf("resolve recovery draft activity: %w", err)
		}
		if count == 0 {
			return activity.ErrNotFound
		}
		if err := tx.Exec(`INSERT INTO curu.activity_recovery_drafts
			(id, owner_id, activity_id, base_revision, payload, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?::jsonb, ?, ?)
			ON CONFLICT (owner_id, activity_id) DO UPDATE
			SET base_revision = EXCLUDED.base_revision, payload = EXCLUDED.payload, updated_at = EXCLUDED.updated_at`,
			id, ownerID, activityID, baseRevision, string(payload), now, now).Error; err != nil {
			return fmt.Errorf("save activity recovery draft: %w", err)
		}
		return nil
	})
	if err != nil {
		return activity.RecoveryDraft{}, err
	}
	return r.findRecoveryDraftForActivity(ctx, ownerID, activityID)
}

func (r *ActivityRepository) ListRecoveryDrafts(ctx context.Context, ownerID string) ([]activity.RecoveryDraftSummary, error) {
	var rows []activityRecoveryDraftRow
	if err := r.db.WithContext(ctx).Where("owner_id = ?", ownerID).Order("updated_at DESC, id").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list activity recovery drafts: %w", err)
	}
	items := make([]activity.RecoveryDraftSummary, len(rows))
	for index, row := range rows {
		draft, err := recoveryDraftFromRow(row)
		if err != nil {
			return nil, err
		}
		items[index] = activity.RecoveryDraftSummary{ID: draft.ID, ActivityID: draft.ActivityID, Title: draft.Input.Title, UpdatedAt: draft.UpdatedAt}
	}
	return items, nil
}

func (r *ActivityRepository) FindRecoveryDraft(ctx context.Context, ownerID, id string) (activity.RecoveryDraft, error) {
	var row activityRecoveryDraftRow
	err := r.db.WithContext(ctx).Where("owner_id = ? AND id = ?", ownerID, id).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return activity.RecoveryDraft{}, activity.ErrNotFound
	}
	if err != nil {
		return activity.RecoveryDraft{}, fmt.Errorf("find activity recovery draft: %w", err)
	}
	return recoveryDraftFromRow(row)
}

func (r *ActivityRepository) findRecoveryDraftForActivity(ctx context.Context, ownerID, activityID string) (activity.RecoveryDraft, error) {
	var row activityRecoveryDraftRow
	err := r.db.WithContext(ctx).Where("owner_id = ? AND activity_id = ?", ownerID, activityID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return activity.RecoveryDraft{}, activity.ErrNotFound
	}
	if err != nil {
		return activity.RecoveryDraft{}, fmt.Errorf("find activity recovery draft: %w", err)
	}
	return recoveryDraftFromRow(row)
}

func (r *ActivityRepository) DiscardRecoveryDraft(ctx context.Context, ownerID, id string) error {
	result := r.db.WithContext(ctx).Exec("DELETE FROM curu.activity_recovery_drafts WHERE owner_id = ? AND id = ?", ownerID, id)
	if result.Error != nil {
		return fmt.Errorf("discard activity recovery draft: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return activity.ErrNotFound
	}
	return nil
}

func recoveryDraftFromRow(row activityRecoveryDraftRow) (activity.RecoveryDraft, error) {
	var payload activityRecoveryDraftPayload
	if err := json.Unmarshal(row.Payload, &payload); err != nil || payload.Format != 1 {
		return activity.RecoveryDraft{}, fmt.Errorf("decode activity recovery draft: unsupported or invalid payload")
	}
	return activity.RecoveryDraft{
		ID: row.ID, OwnerID: row.OwnerID, ActivityID: row.ActivityID, BaseRevision: row.BaseRevision,
		Input: payload.Input, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}, nil
}
