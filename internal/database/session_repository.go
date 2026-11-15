package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/gopher-launch/curu/internal/activity"
	"github.com/gopher-launch/curu/internal/session"
	"github.com/lib/pq"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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
	Placements                           []sessionPlacementRow `gorm:"foreignKey:BlockID"`
}

func (sessionBlockRow) TableName() string { return "curu.session_blocks" }

type sessionPlacementRow struct {
	ID, BlockID, SourceActivityID   string
	SourceIdentity                  *string
	Position                        int
	SourceActivityRevision          int64
	Title, Summary, ContentMarkdown string
	RecommendedDurationSeconds      *int
	Phase, Theme                    string
	Tags                            pq.StringArray                   `gorm:"type:text[]"`
	Progressions                    []sessionPlacementProgressionRow `gorm:"foreignKey:PlacementID"`
}

func (sessionPlacementRow) TableName() string { return "curu.session_activity_placements" }

type sessionPlacementProgressionRow struct {
	ID, PlacementID, Title, ContentMarkdown string
	Position                                int
	RecommendedDurationSeconds              *int
}

func (sessionPlacementProgressionRow) TableName() string {
	return "curu.session_placement_progressions"
}

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
	err := r.db.WithContext(ctx).
		Preload("Blocks", func(db *gorm.DB) *gorm.DB { return db.Order("position, id") }).
		Preload("Blocks.Placements", func(db *gorm.DB) *gorm.DB { return db.Order("position, id") }).
		Preload("Blocks.Placements.Progressions", func(db *gorm.DB) *gorm.DB { return db.Order("position, id") }).
		Where("owner_id = ? AND id = ? AND "+predicate, owner, id).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return session.Session{}, session.ErrNotFound
	}
	if err != nil {
		return session.Session{}, fmt.Errorf("find session: %w", err)
	}
	item := session.Session{ID: row.ID, OwnerID: row.OwnerID, Title: row.Title, Date: row.SessionDate, GroupContext: row.GroupContext, Intent: row.Intent, Revision: row.Revision, Archived: archived, Blocks: make([]session.Block, len(row.Blocks))}
	for i, b := range row.Blocks {
		block := session.Block{ID: b.ID, Position: b.Position, Name: b.Name, Intent: b.Intent, Notes: b.Notes, ObservationLens: b.ObservationLens, Placements: make([]session.Placement, len(b.Placements))}
		for j, p := range b.Placements {
			placement := session.Placement{ID: p.ID, Position: p.Position, SourceActivityID: p.SourceActivityID, SourceActivityRevision: p.SourceActivityRevision, Title: p.Title, Summary: p.Summary, ContentMarkdown: p.ContentMarkdown, RecommendedDurationSeconds: p.RecommendedDurationSeconds, Phase: p.Phase, Theme: p.Theme, Tags: []string(p.Tags), Progressions: make([]session.PlacementProgression, len(p.Progressions))}
			if p.SourceIdentity != nil {
				placement.SourceIdentity = *p.SourceIdentity
			}
			for k, progression := range p.Progressions {
				placement.Progressions[k] = session.PlacementProgression{ID: progression.ID, Position: progression.Position, Title: progression.Title, ContentMarkdown: progression.ContentMarkdown, RecommendedDurationSeconds: progression.RecommendedDurationSeconds}
			}
			block.Placements[j] = placement
		}
		item.Blocks[i] = block
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
		return createSessionBlocks(tx, id, input.Blocks)
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
		return reconcileSessionBlocks(tx, id, input.Blocks)
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
func createSessionBlocks(tx *gorm.DB, sessionID string, blocks []session.BlockInput) error {
	for position, block := range blocks {
		if block.ID != "" {
			return session.ValidationErrors{"blocks": "A new session cannot reuse existing blocks."}
		}
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

func reconcileSessionBlocks(tx *gorm.DB, sessionID string, blocks []session.BlockInput) error {
	var existing []string
	if err := tx.Table("curu.session_blocks").Where("session_id = ?", sessionID).Pluck("id", &existing).Error; err != nil {
		return fmt.Errorf("read session blocks: %w", err)
	}
	available := make(map[string]bool, len(existing))
	for _, id := range existing {
		available[id] = true
	}
	// Vacate the unique positions before assigning the submitted ordering.
	if err := tx.Exec("UPDATE curu.session_blocks SET position = position + 100000 WHERE session_id = ?", sessionID).Error; err != nil {
		return fmt.Errorf("prepare session block ordering: %w", err)
	}
	for position, block := range blocks {
		if block.ID == "" {
			id, err := newUUID()
			if err != nil {
				return err
			}
			if err := tx.Exec(`INSERT INTO curu.session_blocks (id, session_id, position, name, intent, notes, observation_lens) VALUES (?, ?, ?, ?, ?, ?, ?)`, id, sessionID, position, block.Name, block.Intent, block.Notes, block.ObservationLens).Error; err != nil {
				return fmt.Errorf("insert session block: %w", err)
			}
			continue
		}
		if !available[block.ID] {
			return session.ValidationErrors{"blocks": "A submitted block does not belong to this session."}
		}
		if err := tx.Exec(`UPDATE curu.session_blocks SET position=?, name=?, intent=?, notes=?, observation_lens=?, updated_at=? WHERE id=? AND session_id=?`, position, block.Name, block.Intent, block.Notes, block.ObservationLens, time.Now().UTC(), block.ID, sessionID).Error; err != nil {
			return fmt.Errorf("update session block: %w", err)
		}
		delete(available, block.ID)
	}
	for id := range available {
		if err := tx.Exec("DELETE FROM curu.session_blocks WHERE id = ? AND session_id = ?", id, sessionID).Error; err != nil {
			return fmt.Errorf("remove session block: %w", err)
		}
	}
	return nil
}

func (r *SessionRepository) AddPlacement(ctx context.Context, owner, sessionID, blockID string, revision int64, activityID string) (session.Session, error) {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var source activityRow
		err := tx.Clauses(clause.Locking{Strength: "SHARE"}).
			Preload("Progressions", func(db *gorm.DB) *gorm.DB { return db.Order("position, id") }).
			Where("id = ? AND owner_id = ? AND archived_at IS NULL", activityID, owner).First(&source).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return activity.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("read source activity snapshot: %w", err)
		}
		result := tx.Exec(`UPDATE curu.sessions SET revision=revision+1, updated_at=? WHERE id=? AND owner_id=? AND archived_at IS NULL AND revision=?`, time.Now().UTC(), sessionID, owner, revision)
		if result.Error != nil {
			return fmt.Errorf("advance session for placement: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return resolveSessionUpdate(tx, owner, sessionID)
		}
		var blockCount int64
		if err := tx.Table("curu.session_blocks").Where("id = ? AND session_id = ?", blockID, sessionID).Count(&blockCount).Error; err != nil {
			return fmt.Errorf("resolve placement block: %w", err)
		}
		if blockCount == 0 {
			return session.ErrNotFound
		}
		var position int
		if err := tx.Raw("SELECT COALESCE(max(position) + 1, 0) FROM curu.session_activity_placements WHERE block_id = ?", blockID).Scan(&position).Error; err != nil {
			return fmt.Errorf("choose placement position: %w", err)
		}
		placementID, err := newUUID()
		if err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO curu.session_activity_placements
			(id, block_id, position, source_activity_id, source_activity_revision, source_identity, title, summary, content_markdown, recommended_duration_seconds, phase, theme, tags)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, placementID, blockID, position, source.ID, source.Revision, source.SourceIdentity, source.Title, source.Summary, source.ContentMarkdown, source.DurationSeconds, source.Phase, source.Theme, pq.Array(source.Tags)).Error; err != nil {
			return fmt.Errorf("insert activity placement snapshot: %w", err)
		}
		for _, progression := range source.Progressions {
			id, err := newUUID()
			if err != nil {
				return err
			}
			if err := tx.Exec(`INSERT INTO curu.session_placement_progressions
				(id, placement_id, position, title, content_markdown, recommended_duration_seconds)
				VALUES (?, ?, ?, ?, ?, ?)`, id, placementID, progression.Position, progression.Title, progression.ContentMarkdown, progression.DurationSeconds).Error; err != nil {
				return fmt.Errorf("insert placement progression snapshot: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return session.Session{}, err
	}
	return r.Find(ctx, owner, sessionID, false)
}
