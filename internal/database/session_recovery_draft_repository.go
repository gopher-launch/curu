package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/gopher-launch/curu/internal/session"
	"gorm.io/gorm"
)

type sessionRecoveryDraftPayload struct {
	Format          int                     `json:"format"`
	Plan            session.Session         `json:"plan"`
	Action          session.PlacementAction `json:"action,omitempty"`
	CurrentArchived bool                    `json:"current_archived,omitempty"`
}

type sessionRecoveryDraftRow struct {
	ID, OwnerID, SessionID string
	BaseRevision           int64
	Payload                json.RawMessage `gorm:"type:jsonb"`
	CreatedAt, UpdatedAt   time.Time
}

func (sessionRecoveryDraftRow) TableName() string { return "curu.session_recovery_drafts" }

func (r *SessionRepository) SaveRecoveryDraft(ctx context.Context, ownerID, sessionID string, baseRevision int64, plan session.Session, action session.PlacementAction, currentArchived bool) (session.RecoveryDraft, error) {
	payload, err := json.Marshal(sessionRecoveryDraftPayload{Format: 1, Plan: plan, Action: action, CurrentArchived: currentArchived})
	if err != nil {
		return session.RecoveryDraft{}, fmt.Errorf("encode session recovery draft: %w", err)
	}
	if len(payload) > maxRecoveryDraftPayloadBytes {
		return session.RecoveryDraft{}, session.ValidationErrors{"draft": "The recovery copy is too large to save."}
	}
	id, err := newUUID()
	if err != nil {
		return session.RecoveryDraft{}, fmt.Errorf("create session recovery draft identity: %w", err)
	}
	now := time.Now().UTC()
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Table("curu.sessions").Where("id=? AND owner_id=?", sessionID, ownerID).Count(&count).Error; err != nil {
			return fmt.Errorf("resolve recovery session: %w", err)
		}
		if count == 0 {
			return session.ErrNotFound
		}
		return tx.Exec(`INSERT INTO curu.session_recovery_drafts
			(id, owner_id, session_id, base_revision, payload, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?::jsonb, ?, ?)
			ON CONFLICT (owner_id, session_id) DO UPDATE
			SET base_revision=EXCLUDED.base_revision, payload=EXCLUDED.payload, updated_at=EXCLUDED.updated_at`,
			id, ownerID, sessionID, baseRevision, string(payload), now, now).Error
	})
	if err != nil {
		return session.RecoveryDraft{}, fmt.Errorf("save session recovery draft: %w", err)
	}
	return r.findRecoveryDraftForSession(ctx, ownerID, sessionID)
}

func (r *SessionRepository) ListRecoveryDrafts(ctx context.Context, ownerID string) ([]session.RecoveryDraftSummary, error) {
	var rows []sessionRecoveryDraftRow
	if err := r.db.WithContext(ctx).Where("owner_id=?", ownerID).Order("updated_at DESC, id").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list session recovery drafts: %w", err)
	}
	items := make([]session.RecoveryDraftSummary, len(rows))
	for i, row := range rows {
		draft, err := sessionRecoveryDraftFromRow(row)
		if err != nil {
			return nil, err
		}
		items[i] = session.RecoveryDraftSummary{ID: draft.ID, SessionID: draft.SessionID, Title: draft.Plan.Title, UpdatedAt: draft.UpdatedAt}
	}
	return items, nil
}

func (r *SessionRepository) FindRecoveryDraft(ctx context.Context, ownerID, id string) (session.RecoveryDraft, error) {
	var row sessionRecoveryDraftRow
	err := r.db.WithContext(ctx).Where("owner_id=? AND id=?", ownerID, id).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return session.RecoveryDraft{}, session.ErrNotFound
	}
	if err != nil {
		return session.RecoveryDraft{}, fmt.Errorf("find session recovery draft: %w", err)
	}
	return sessionRecoveryDraftFromRow(row)
}

func (r *SessionRepository) findRecoveryDraftForSession(ctx context.Context, ownerID, sessionID string) (session.RecoveryDraft, error) {
	var row sessionRecoveryDraftRow
	err := r.db.WithContext(ctx).Where("owner_id=? AND session_id=?", ownerID, sessionID).First(&row).Error
	if err != nil {
		return session.RecoveryDraft{}, fmt.Errorf("verify session recovery draft: %w", err)
	}
	return sessionRecoveryDraftFromRow(row)
}

func (r *SessionRepository) DiscardRecoveryDraft(ctx context.Context, ownerID, id string) error {
	result := r.db.WithContext(ctx).Exec("DELETE FROM curu.session_recovery_drafts WHERE owner_id=? AND id=?", ownerID, id)
	if result.Error != nil {
		return fmt.Errorf("discard session recovery draft: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return session.ErrNotFound
	}
	return nil
}

func sessionRecoveryDraftFromRow(row sessionRecoveryDraftRow) (session.RecoveryDraft, error) {
	var payload sessionRecoveryDraftPayload
	if err := json.Unmarshal(row.Payload, &payload); err != nil || payload.Format != 1 {
		return session.RecoveryDraft{}, fmt.Errorf("decode session recovery draft: unsupported or invalid payload")
	}
	return session.RecoveryDraft{ID: row.ID, OwnerID: row.OwnerID, SessionID: row.SessionID, BaseRevision: row.BaseRevision, Plan: payload.Plan, Action: payload.Action, CurrentArchived: payload.CurrentArchived, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}, nil
}
