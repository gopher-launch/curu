// Package session implements owner-scoped session planning use cases.
package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gopher-launch/curu/internal/authorization"
)

const (
	MaxTitleBytes         = 200
	MaxTextBytes          = 10000
	MaxBlocks             = 50
	MaxPlacementsPerBlock = 100
)

var (
	ErrNotFound         = errors.New("session not found")
	ErrInvalidInput     = errors.New("invalid session input")
	ErrConflict         = errors.New("session revision conflict")
	ErrArchivedConflict = fmt.Errorf("%w: session was archived", ErrConflict)
)

type Summary struct {
	ID, OwnerID, Title string
	Date               *time.Time
}
type Session struct {
	ID, OwnerID, Title, GroupContext, Intent string
	Date                                     *time.Time
	Revision                                 int64
	Archived                                 bool
	Blocks                                   []Block
}
type Block struct {
	ID                                   string
	Position                             int
	Name, Intent, Notes, ObservationLens string
	Placements                           []Placement
}
type Placement struct {
	ID, SourceActivityID, SourceIdentity string
	Position                             int
	SourceActivityRevision               int64
	Title, Summary, ContentMarkdown      string
	RecommendedDurationSeconds           *int
	DurationSeconds                      *int
	LocalNotes                           string
	Phase, Theme                         string
	Tags                                 []string
	Progressions                         []PlacementProgression
}
type PlacementProgression struct {
	ID, Title, ContentMarkdown string
	Position                   int
	RecommendedDurationSeconds *int
}
type Input struct {
	Title, GroupContext, Intent string
	Date                        *time.Time
	Blocks                      []BlockInput
}
type BlockInput struct {
	ID, Name, Intent, Notes, ObservationLens string
	Placements                               []PlacementInput
}
type PlacementInput struct{ ID, LocalNotes string }

type PlacementActionKind string

const (
	AddPlacementAction     PlacementActionKind = "add"
	ReplacePlacementAction PlacementActionKind = "replace"
	MovePlacementAction    PlacementActionKind = "move"
	RemovePlacementAction  PlacementActionKind = "remove"
)

type PlacementAction struct {
	Kind                       PlacementActionKind
	PlacementID, TargetBlockID string
	ActivityID                 string
}

type ValidationErrors map[string]string

func (e ValidationErrors) Error() string        { return ErrInvalidInput.Error() }
func (e ValidationErrors) Is(target error) bool { return target == ErrInvalidInput }

type Store interface {
	List(context.Context, string, bool) ([]Summary, error)
	Find(context.Context, string, string, bool) (Session, error)
	Create(context.Context, string, Input) (Session, error)
	Update(context.Context, string, string, int64, Input) (Session, error)
	Archive(context.Context, string, string, int64, bool) (Session, error)
	AddPlacement(context.Context, string, string, string, int64, string) (Session, error)
	ApplyPlacementAction(context.Context, string, string, int64, Input, PlacementAction) (Session, error)
}

type Planner struct {
	store  Store
	policy authorization.Policy
}

func NewPlanner(store Store, policy authorization.Policy) (*Planner, error) {
	if store == nil {
		return nil, errors.New("session store is required")
	}
	return &Planner{store: store, policy: policy}, nil
}

func (p *Planner) List(ctx context.Context, principal authorization.Principal, archived bool) ([]Summary, error) {
	if err := p.policy.Authorize(principal, authorization.Read, principal.OwnerID); err != nil {
		return nil, err
	}
	return p.store.List(ctx, principal.OwnerID, archived)
}
func (p *Planner) Detail(ctx context.Context, principal authorization.Principal, id string, archived bool) (Session, error) {
	id, err := authorization.NormalizeUUID(id)
	if err != nil {
		return Session{}, ErrNotFound
	}
	if err := p.policy.Authorize(principal, authorization.Read, principal.OwnerID); err != nil {
		return Session{}, err
	}
	item, err := p.store.Find(ctx, principal.OwnerID, id, archived)
	if err != nil {
		return Session{}, err
	}
	if err := p.policy.Authorize(principal, authorization.Read, item.OwnerID); err != nil {
		return Session{}, err
	}
	return item, nil
}
func (p *Planner) Create(ctx context.Context, principal authorization.Principal, input Input) (Session, error) {
	if err := p.policy.Authorize(principal, authorization.Mutate, principal.OwnerID); err != nil {
		return Session{}, err
	}
	input, err := validate(input)
	if err != nil {
		return Session{}, err
	}
	return p.store.Create(ctx, principal.OwnerID, input)
}
func (p *Planner) Update(ctx context.Context, principal authorization.Principal, id string, revision int64, input Input) (Session, error) {
	id, err := authorization.NormalizeUUID(id)
	if err != nil {
		return Session{}, ErrNotFound
	}
	if err := p.policy.Authorize(principal, authorization.Mutate, principal.OwnerID); err != nil {
		return Session{}, err
	}
	input, err = validate(input)
	if err != nil {
		return Session{}, err
	}
	if revision < 1 {
		return Session{}, ValidationErrors{"revision": "Reload this session before saving."}
	}
	return p.store.Update(ctx, principal.OwnerID, id, revision, input)
}
func (p *Planner) Archive(ctx context.Context, principal authorization.Principal, id string, revision int64, archived bool) (Session, error) {
	id, err := authorization.NormalizeUUID(id)
	if err != nil {
		return Session{}, ErrNotFound
	}
	if err := p.policy.Authorize(principal, authorization.Archive, principal.OwnerID); err != nil {
		return Session{}, err
	}
	if revision < 1 {
		return Session{}, ValidationErrors{"revision": "Reload this session before changing its archive state."}
	}
	return p.store.Archive(ctx, principal.OwnerID, id, revision, archived)
}

// AddPlacement atomically copies one current activity into a session-owned
// snapshot. Selection and editor interactions are delivered by CURU-023.
func (p *Planner) AddPlacement(ctx context.Context, principal authorization.Principal, sessionID, blockID string, revision int64, activityID string) (Session, error) {
	var err error
	if sessionID, err = authorization.NormalizeUUID(sessionID); err != nil {
		return Session{}, ErrNotFound
	}
	if blockID, err = authorization.NormalizeUUID(blockID); err != nil {
		return Session{}, ErrNotFound
	}
	if activityID, err = authorization.NormalizeUUID(activityID); err != nil {
		return Session{}, ErrNotFound
	}
	if err := p.policy.Authorize(principal, authorization.Mutate, principal.OwnerID); err != nil {
		return Session{}, err
	}
	if revision < 1 {
		return Session{}, ValidationErrors{"revision": "Reload this session before adding an activity."}
	}
	return p.store.AddPlacement(ctx, principal.OwnerID, sessionID, blockID, revision, activityID)
}

// ApplyPlacementAction saves the submitted editor aggregate and applies one
// structural placement action in the same revision-checked transaction.
func (p *Planner) ApplyPlacementAction(ctx context.Context, principal authorization.Principal, sessionID string, revision int64, input Input, action PlacementAction) (Session, error) {
	var err error
	if sessionID, err = authorization.NormalizeUUID(sessionID); err != nil {
		return Session{}, ErrNotFound
	}
	if err := p.policy.Authorize(principal, authorization.Mutate, principal.OwnerID); err != nil {
		return Session{}, err
	}
	input, err = validate(input)
	if err != nil {
		return Session{}, err
	}
	if revision < 1 {
		return Session{}, ValidationErrors{"revision": "Reload this session before changing its activities."}
	}
	normalize := func(value string) (string, bool) {
		id, normalizeErr := authorization.NormalizeUUID(value)
		return id, normalizeErr == nil
	}
	switch action.Kind {
	case AddPlacementAction:
		if action.TargetBlockID, _ = normalize(action.TargetBlockID); action.TargetBlockID == "" {
			return Session{}, ValidationErrors{"placement_action": "Choose a saved target block."}
		}
		if action.ActivityID, _ = normalize(action.ActivityID); action.ActivityID == "" {
			return Session{}, ValidationErrors{"placement_action": "Choose an activity."}
		}
	case ReplacePlacementAction:
		if action.PlacementID, _ = normalize(action.PlacementID); action.PlacementID == "" {
			return Session{}, ValidationErrors{"placement_action": "Choose a placed activity."}
		}
		if action.ActivityID, _ = normalize(action.ActivityID); action.ActivityID == "" {
			return Session{}, ValidationErrors{"placement_action": "Choose a replacement activity."}
		}
	case MovePlacementAction:
		if action.PlacementID, _ = normalize(action.PlacementID); action.PlacementID == "" {
			return Session{}, ValidationErrors{"placement_action": "Choose a placed activity."}
		}
		if action.TargetBlockID, _ = normalize(action.TargetBlockID); action.TargetBlockID == "" {
			return Session{}, ValidationErrors{"placement_action": "Choose a saved target block."}
		}
	case RemovePlacementAction:
		if action.PlacementID, _ = normalize(action.PlacementID); action.PlacementID == "" {
			return Session{}, ValidationErrors{"placement_action": "Choose a placed activity."}
		}
	default:
		return Session{}, ValidationErrors{"placement_action": "Choose a supported activity action."}
	}
	return p.store.ApplyPlacementAction(ctx, principal.OwnerID, sessionID, revision, input, action)
}

func validate(input Input) (Input, error) {
	input.Title = strings.TrimSpace(input.Title)
	input.GroupContext = strings.TrimSpace(input.GroupContext)
	input.Intent = strings.TrimSpace(input.Intent)
	errs := ValidationErrors{}
	check := func(field, value string, max int, required bool) {
		if !utf8.ValidString(value) || len(value) > max || required && value == "" {
			errs[field] = "Enter valid text within the displayed limit."
		}
	}
	check("title", input.Title, MaxTitleBytes, true)
	check("group_context", input.GroupContext, MaxTextBytes, false)
	check("intent", input.Intent, MaxTextBytes, false)
	if len(input.Blocks) > MaxBlocks {
		errs["blocks"] = "A session can contain at most 50 blocks."
	}
	for i := range input.Blocks {
		block := &input.Blocks[i]
		if block.ID != "" {
			id, err := authorization.NormalizeUUID(block.ID)
			if err != nil {
				errs["blocks"] = "Reload this session before saving its blocks."
			} else {
				block.ID = id
			}
		}
		block.Name = strings.TrimSpace(block.Name)
		block.Intent = strings.TrimSpace(block.Intent)
		block.Notes = strings.TrimSpace(block.Notes)
		block.ObservationLens = strings.TrimSpace(block.ObservationLens)
		check("block_name", block.Name, MaxTitleBytes, true)
		check("block_intent", block.Intent, MaxTextBytes, false)
		check("block_notes", block.Notes, MaxTextBytes, false)
		check("block_observation_lens", block.ObservationLens, MaxTextBytes, false)
		if len(block.Placements) > MaxPlacementsPerBlock {
			errs["placements"] = "A block can contain at most 100 activities."
		}
		if block.ID == "" && len(block.Placements) > 0 {
			errs["placements"] = "Save a new block before adding activities to it."
		}
		seenPlacements := make(map[string]bool, len(block.Placements))
		for j := range block.Placements {
			placement := &block.Placements[j]
			id, err := authorization.NormalizeUUID(placement.ID)
			if err != nil || seenPlacements[id] {
				errs["placements"] = "Reload this session before saving its activities."
			} else {
				placement.ID = id
				seenPlacements[id] = true
			}
			placement.LocalNotes = strings.TrimSpace(placement.LocalNotes)
			check("placement_notes", placement.LocalNotes, MaxTextBytes, false)
		}
	}
	if input.Date != nil {
		day := time.Date(input.Date.Year(), input.Date.Month(), input.Date.Day(), 0, 0, 0, 0, time.UTC)
		input.Date = &day
	}
	if len(errs) > 0 {
		return Input{}, errs
	}
	return input, nil
}
