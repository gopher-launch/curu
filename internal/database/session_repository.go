package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/gopher-launch/curu/internal/session"
	"gorm.io/gorm"
)

type SessionRepository struct{ db *gorm.DB }

func NewSessionRepository(db *gorm.DB) (*SessionRepository, error) {
	if db == nil {
		return nil, errors.New("database is required")
	}
	return &SessionRepository{db: db}, nil
}

type sessionRow struct {
	ID, OwnerID, Title, GroupContext, Intent string
	SessionDate                              *time.Time
	Revision                                 int64
	Blocks                                   []sessionBlockRow `gorm:"foreignKey:SessionID"`
}

func (sessionRow) TableName() string { return "curu.sessions" }

type sessionBlockRow struct {
	ID, SessionID                        string
	Position                             int
	Name, Intent, Notes, ObservationLens string
}

func (sessionBlockRow) TableName() string { return "curu.session_blocks" }

func (r *SessionRepository) List(ctx context.Context, owner string, archived bool) ([]session.Summary, error) {
	predicate := "archived_at IS NULL"
	if archived {
		predicate = "archived_at IS NOT NULL"
	}
	var rows []sessionRow
	if err := r.db.WithContext(ctx).Where("owner_id = ? AND "+predicate, owner).Order("session_date NULLS LAST, lower(title), id").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	items := make([]session.Summary, len(rows))
	for i, row := range rows {
		items[i] = session.Summary{ID: row.ID, OwnerID: row.OwnerID, Title: row.Title, Date: row.SessionDate}
	}
	return items, nil
}
func (r *SessionRepository) Find(ctx context.Context, owner, id string, archived bool) (session.Session, error) {
	predicate := "archived_at IS NULL"
	if archived {
		predicate = "archived_at IS NOT NULL"
	}
	var row sessionRow
	err := r.db.WithContext(ctx).Preload("Blocks", func(db *gorm.DB) *gorm.DB { return db.Order("position, id") }).Where("owner_id = ? AND id = ? AND "+predicate, owner, id).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return session.Session{}, session.ErrNotFound
	}
	if err != nil {
		return session.Session{}, fmt.Errorf("find session: %w", err)
	}
	item := session.Session{ID: row.ID, OwnerID: row.OwnerID, Title: row.Title, Date: row.SessionDate, GroupContext: row.GroupContext, Intent: row.Intent, Revision: row.Revision, Archived: archived, Blocks: make([]session.Block, len(row.Blocks))}
	for i, b := range row.Blocks {
		item.Blocks[i] = session.Block{ID: b.ID, Position: b.Position, Name: b.Name, Intent: b.Intent, Notes: b.Notes, ObservationLens: b.ObservationLens}
	}
	return item, nil
}
func (r *SessionRepository) Create(ctx context.Context, owner string, input session.Input) (session.Session, error) {
	id, err := newUUID()
	if err != nil {
		return session.Session{}, err
	}
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`INSERT INTO curu.sessions (id, owner_id, title, session_date, group_context, intent) VALUES (?, ?, ?, ?, ?, ?)`, id, owner, input.Title, input.Date, input.GroupContext, input.Intent).Error; err != nil {
			return fmt.Errorf("insert session: %w", err)
		}
		return replaceSessionBlocks(tx, id, input.Blocks)
	})
	if err != nil {
		return session.Session{}, err
	}
	return r.Find(ctx, owner, id, false)
}
func (r *SessionRepository) Update(ctx context.Context, owner, id string, revision int64, input session.Input) (session.Session, error) {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Exec(`UPDATE curu.sessions SET title=?, session_date=?, group_context=?, intent=?, revision=revision+1, updated_at=? WHERE id=? AND owner_id=? AND archived_at IS NULL AND revision=?`, input.Title, input.Date, input.GroupContext, input.Intent, time.Now().UTC(), id, owner, revision)
		if result.Error != nil {
			return fmt.Errorf("update session: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return resolveSessionUpdate(tx, owner, id)
		}
		if err := tx.Exec("DELETE FROM curu.session_blocks WHERE session_id = ?", id).Error; err != nil {
			return fmt.Errorf("replace session blocks: %w", err)
		}
		return replaceSessionBlocks(tx, id, input.Blocks)
	})
	if err != nil {
		return session.Session{}, err
	}
	return r.Find(ctx, owner, id, false)
}

func resolveSessionUpdate(db *gorm.DB, owner, id string) error {
	var state struct {
		Archived bool
	}
	err := db.Table("curu.sessions").
		Select("archived_at IS NOT NULL AS archived").
		Where("owner_id = ? AND id = ?", owner, id).
		Take(&state).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return session.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("resolve session update: %w", err)
	}
	if state.Archived {
		return session.ErrArchivedConflict
	}
	return session.ErrConflict
}

func (r *SessionRepository) Archive(ctx context.Context, owner, id string, revision int64, archived bool) (session.Session, error) {
	predicate := "archived_at IS NULL"
	value := any(time.Now().UTC())
	if !archived {
		predicate = "archived_at IS NOT NULL"
		value = nil
	}
	result := r.db.WithContext(ctx).Exec(`UPDATE curu.sessions SET archived_at=?, revision=revision+1, updated_at=? WHERE id=? AND owner_id=? AND `+predicate+` AND revision=?`, value, time.Now().UTC(), id, owner, revision)
	if result.Error != nil {
		return session.Session{}, fmt.Errorf("change session archive state: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		if err := resolveSessionChange(r.db.WithContext(ctx), owner, id, !archived); err != nil {
			return session.Session{}, err
		}
	}
	return r.Find(ctx, owner, id, archived)
}
func resolveSessionChange(db *gorm.DB, owner, id string, archived bool) error {
	predicate := "archived_at IS NULL"
	if archived {
		predicate = "archived_at IS NOT NULL"
	}
	var count int64
	if err := db.Table("curu.sessions").Where("owner_id=? AND id=? AND "+predicate, owner, id).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return session.ErrNotFound
	}
	return session.ErrConflict
}
func replaceSessionBlocks(tx *gorm.DB, sessionID string, blocks []session.BlockInput) error {
	for position, block := range blocks {
		id, err := newUUID()
		if err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO curu.session_blocks (id, session_id, position, name, intent, notes, observation_lens) VALUES (?, ?, ?, ?, ?, ?, ?)`, id, sessionID, position, block.Name, block.Intent, block.Notes, block.ObservationLens).Error; err != nil {
			return fmt.Errorf("insert session block: %w", err)
		}
	}
	return nil
}
