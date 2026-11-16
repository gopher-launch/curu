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
	MaxTimingSeconds      = 86400
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
	AllocationSeconds                        *int
	Revision                                 int64
	Archived                                 bool
	Blocks                                   []Block
}
type Block struct {
	ID                                   string
	Position                             int
	Name, Intent, Notes, ObservationLens string
	AllocationSeconds                    *int
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
	AllocationSeconds           *int
	Blocks                      []BlockInput
}
type BlockInput struct {
	ID, Name, Intent, Notes, ObservationLens string
	AllocationSeconds                        *int
	Placements                               []PlacementInput
}
type PlacementInput struct {
	ID, LocalNotes  string
	DurationSeconds *int
}

type TimingSummary struct {
	HasTiming, Complete bool
	TotalSeconds        int
	Warnings            []string
	Blocks              []BlockTiming
}

type BlockTiming struct {
	Seconds            int
	HasValue, Complete bool
	Warnings           []string
}

// SummarizeTiming counts each block once. An explicit block allocation wins;
// otherwise the block contributes the complete or partial sum of placement
// durations. Recommendations are deliberately excluded.
func SummarizeTiming(item Session) TimingSummary {
	summary := TimingSummary{Complete: true, Blocks: make([]BlockTiming, len(item.Blocks))}
	for i, block := range item.Blocks {
		bt := BlockTiming{Complete: true}
		placementTotal, timed := 0, 0
		for _, placement := range block.Placements {
			if placement.DurationSeconds == nil {
				bt.Complete = false
				continue
			}
			placementTotal += *placement.DurationSeconds
			timed++
		}
		if block.AllocationSeconds != nil {
			bt.HasValue, bt.Seconds = true, *block.AllocationSeconds
			if len(block.Placements) > 0 && timed == len(block.Placements) && placementTotal != bt.Seconds {
				bt.Warnings = append(bt.Warnings, "The block allocation differs from its activity total.")
			}
			bt.Complete = true
		} else if timed > 0 {
			bt.HasValue, bt.Seconds = true, placementTotal
			if timed < len(block.Placements) {
				bt.Warnings = append(bt.Warnings, "This is a partial total because some activities are untimed.")
			}
		} else if len(block.Placements) == 0 {
			bt.Complete = false
		}
		if bt.HasValue {
			summary.HasTiming = true
			summary.TotalSeconds += bt.Seconds
		}
		if !bt.Complete {
			summary.Complete = false
		}
		summary.Blocks[i] = bt
	}
	if item.AllocationSeconds != nil {
		summary.HasTiming = true
		if summary.Complete && summary.TotalSeconds != *item.AllocationSeconds {
			summary.Warnings = append(summary.Warnings, "The session allocation differs from the block total.")
		} else if !summary.Complete {
			summary.Warnings = append(summary.Warnings, "The session total is partial because some blocks have unknown time.")
		}
	} else if summary.HasTiming && !summary.Complete {
		summary.Warnings = append(summary.Warnings, "The session total is partial because some blocks have unknown time.")
	}
	for _, block := range summary.Blocks {
		summary.Warnings = append(summary.Warnings, block.Warnings...)
	}
	return summary
}

type PlacementActionKind string

const (
	AddPlacementAction      PlacementActionKind = "add"
	ReplacePlacementAction  PlacementActionKind = "replace"
	MovePlacementAction     PlacementActionKind = "move"
	RemovePlacementAction   PlacementActionKind = "remove"
	MoveBlockUpAction       PlacementActionKind = "move_block_up"
	MoveBlockDownAction     PlacementActionKind = "move_block_down"
	MovePlacementUpAction   PlacementActionKind = "move_placement_up"
	MovePlacementDownAction PlacementActionKind = "move_placement_down"
)

type PlacementAction struct {
	Kind                                PlacementActionKind
	BlockID, PlacementID, TargetBlockID string
	ActivityID                          string
}

// RecoveryDraft is a durable, owner-scoped copy of a conflicting editor.
// Plan contains the submitted aggregate with its copied activity snapshots;
// Action records the structural intent that lost the revision race.
type RecoveryDraft struct {
	ID, OwnerID, SessionID string
	BaseRevision           int64
	Plan                   Session
	Action                 PlacementAction
	CurrentArchived        bool
	CreatedAt, UpdatedAt   time.Time
}

type RecoveryDraftSummary struct {
	ID, SessionID, Title string
	UpdatedAt            time.Time
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
	SaveRecoveryDraft(context.Context, string, string, int64, Session, PlacementAction, bool) (RecoveryDraft, error)
	ListRecoveryDrafts(context.Context, string) ([]RecoveryDraftSummary, error)
	FindRecoveryDraft(context.Context, string, string) (RecoveryDraft, error)
	DiscardRecoveryDraft(context.Context, string, string) error
}

func (p *Planner) SaveRecoveryDraft(ctx context.Context, principal authorization.Principal, sessionID string, baseRevision int64, snapshot Session, input Input, action PlacementAction, currentArchived bool) (RecoveryDraft, error) {
	var err error
	if sessionID, err = authorization.NormalizeUUID(sessionID); err != nil {
		return RecoveryDraft{}, ErrNotFound
	}
	if err := p.policy.Authorize(principal, authorization.Recovery, principal.OwnerID); err != nil {
		return RecoveryDraft{}, err
	}
	if baseRevision < 1 || snapshot.ID != sessionID || snapshot.OwnerID != principal.OwnerID {
		return RecoveryDraft{}, ValidationErrors{"revision": "Reload the conflicting session before saving its recovery copy."}
	}
	input, err = validate(input)
	if err != nil {
		return RecoveryDraft{}, err
	}
	action, err = normalizeRecoveryAction(action)
	if err != nil {
		return RecoveryDraft{}, err
	}
	plan, err := recoverSubmittedPlan(snapshot, input, action)
	if err != nil {
		return RecoveryDraft{}, err
	}
	return p.store.SaveRecoveryDraft(ctx, principal.OwnerID, sessionID, baseRevision, plan, action, currentArchived)
}

func normalizeRecoveryAction(action PlacementAction) (PlacementAction, error) {
	normalize := func(value string) (string, error) {
		if value == "" {
			return "", nil
		}
		id, err := authorization.NormalizeUUID(value)
		if err != nil {
			return "", ValidationErrors{"recovery_action": "The attempted session action is invalid."}
		}
		return id, nil
	}
	var err error
	if action.BlockID, err = normalize(action.BlockID); err != nil {
		return PlacementAction{}, err
	}
	if action.PlacementID, err = normalize(action.PlacementID); err != nil {
		return PlacementAction{}, err
	}
	if action.TargetBlockID, err = normalize(action.TargetBlockID); err != nil {
		return PlacementAction{}, err
	}
	if action.ActivityID, err = normalize(action.ActivityID); err != nil {
		return PlacementAction{}, err
	}
	switch action.Kind {
	case "":
		if action.BlockID != "" || action.PlacementID != "" || action.TargetBlockID != "" || action.ActivityID != "" {
			return PlacementAction{}, ValidationErrors{"recovery_action": "The attempted session action is invalid."}
		}
	case AddPlacementAction:
		if action.TargetBlockID == "" || action.ActivityID == "" {
			return PlacementAction{}, ValidationErrors{"recovery_action": "The attempted add action is incomplete."}
		}
	case ReplacePlacementAction:
		if action.PlacementID == "" || action.ActivityID == "" {
			return PlacementAction{}, ValidationErrors{"recovery_action": "The attempted replacement is incomplete."}
		}
	case MovePlacementAction:
		if action.PlacementID == "" || action.TargetBlockID == "" {
			return PlacementAction{}, ValidationErrors{"recovery_action": "The attempted move is incomplete."}
		}
	case RemovePlacementAction, MovePlacementUpAction, MovePlacementDownAction:
		if action.PlacementID == "" {
			return PlacementAction{}, ValidationErrors{"recovery_action": "The attempted activity action is incomplete."}
		}
	case MoveBlockUpAction, MoveBlockDownAction:
		if action.BlockID == "" {
			return PlacementAction{}, ValidationErrors{"recovery_action": "The attempted block action is incomplete."}
		}
	default:
		return PlacementAction{}, ValidationErrors{"recovery_action": "The attempted session action is invalid."}
	}
	return action, nil
}

func (p *Planner) ListRecoveryDrafts(ctx context.Context, principal authorization.Principal) ([]RecoveryDraftSummary, error) {
	if err := p.policy.Authorize(principal, authorization.Recovery, principal.OwnerID); err != nil {
		return nil, err
	}
	return p.store.ListRecoveryDrafts(ctx, principal.OwnerID)
}

func (p *Planner) RecoveryDraft(ctx context.Context, principal authorization.Principal, id string) (RecoveryDraft, error) {
	id, err := authorization.NormalizeUUID(id)
	if err != nil {
		return RecoveryDraft{}, ErrNotFound
	}
	if err := p.policy.Authorize(principal, authorization.Recovery, principal.OwnerID); err != nil {
		return RecoveryDraft{}, err
	}
	draft, err := p.store.FindRecoveryDraft(ctx, principal.OwnerID, id)
	if err != nil {
		return RecoveryDraft{}, err
	}
	if err := p.policy.Authorize(principal, authorization.Recovery, draft.OwnerID); err != nil {
		return RecoveryDraft{}, err
	}
	return draft, nil
}

func (p *Planner) DiscardRecoveryDraft(ctx context.Context, principal authorization.Principal, id string) error {
	id, err := authorization.NormalizeUUID(id)
	if err != nil {
		return ErrNotFound
	}
	if err := p.policy.Authorize(principal, authorization.Recovery, principal.OwnerID); err != nil {
		return err
	}
	return p.store.DiscardRecoveryDraft(ctx, principal.OwnerID, id)
}

func recoverSubmittedPlan(snapshot Session, input Input, action PlacementAction) (Session, error) {
	byBlock := make(map[string]Block, len(snapshot.Blocks))
	for _, block := range snapshot.Blocks {
		byBlock[block.ID] = block
	}
	plan := snapshot
	plan.Title, plan.Date, plan.GroupContext, plan.Intent, plan.AllocationSeconds = input.Title, input.Date, input.GroupContext, input.Intent, input.AllocationSeconds
	plan.Blocks = make([]Block, 0, len(input.Blocks))
	for position, submitted := range input.Blocks {
		block := byBlock[submitted.ID]
		block.ID, block.Position, block.Name, block.Intent, block.Notes, block.ObservationLens, block.AllocationSeconds = submitted.ID, position, submitted.Name, submitted.Intent, submitted.Notes, submitted.ObservationLens, submitted.AllocationSeconds
		byPlacement := make(map[string]Placement, len(block.Placements))
		for _, placement := range block.Placements {
			byPlacement[placement.ID] = placement
		}
		block.Placements = make([]Placement, 0, len(submitted.Placements))
		for p, entered := range submitted.Placements {
			placement, ok := byPlacement[entered.ID]
			if !ok {
				return Session{}, ValidationErrors{"placements": "The recovery snapshot no longer matches the submitted activities."}
			}
			placement.Position, placement.LocalNotes, placement.DurationSeconds = p, entered.LocalNotes, entered.DurationSeconds
			block.Placements = append(block.Placements, placement)
		}
		plan.Blocks = append(plan.Blocks, block)
	}
	// Actions that only rearrange or remove already-copied content can be
	// represented exactly. Add/replace retain their explicit intent alongside
	// the complete pre-action submitted plan because no stale action is replayed.
	switch action.Kind {
	case MoveBlockUpAction, MoveBlockDownAction:
		delta := -1
		if action.Kind == MoveBlockDownAction {
			delta = 1
		}
		index := blockIndex(plan.Blocks, action.BlockID)
		target := index + delta
		if index >= 0 && target >= 0 && target < len(plan.Blocks) {
			plan.Blocks[index], plan.Blocks[target] = plan.Blocks[target], plan.Blocks[index]
		}
	case MovePlacementUpAction, MovePlacementDownAction:
		delta := -1
		if action.Kind == MovePlacementDownAction {
			delta = 1
		}
		for i := range plan.Blocks {
			index := placementIndex(plan.Blocks[i].Placements, action.PlacementID)
			target := index + delta
			if index >= 0 && target >= 0 && target < len(plan.Blocks[i].Placements) {
				plan.Blocks[i].Placements[index], plan.Blocks[i].Placements[target] = plan.Blocks[i].Placements[target], plan.Blocks[i].Placements[index]
				break
			}
		}
	case MovePlacementAction:
		targetBlock := blockIndex(plan.Blocks, action.TargetBlockID)
		if targetBlock < 0 {
			break
		}
		var moved Placement
		found := false
		for i := range plan.Blocks {
			if index := placementIndex(plan.Blocks[i].Placements, action.PlacementID); index >= 0 {
				moved = plan.Blocks[i].Placements[index]
				plan.Blocks[i].Placements = append(plan.Blocks[i].Placements[:index], plan.Blocks[i].Placements[index+1:]...)
				found = true
				break
			}
		}
		if found {
			plan.Blocks[targetBlock].Placements = append(plan.Blocks[targetBlock].Placements, moved)
		}
	case RemovePlacementAction:
		for i := range plan.Blocks {
			if index := placementIndex(plan.Blocks[i].Placements, action.PlacementID); index >= 0 {
				plan.Blocks[i].Placements = append(plan.Blocks[i].Placements[:index], plan.Blocks[i].Placements[index+1:]...)
				break
			}
		}
	}
	for i := range plan.Blocks {
		plan.Blocks[i].Position = i
		for j := range plan.Blocks[i].Placements {
			plan.Blocks[i].Placements[j].Position = j
		}
	}
	return plan, nil
}

func blockIndex(blocks []Block, id string) int {
	for i := range blocks {
		if blocks[i].ID == id {
			return i
		}
	}
	return -1
}
func placementIndex(items []Placement, id string) int {
	for i := range items {
		if items[i].ID == id {
			return i
		}
	}
	return -1
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
// block or placement structural action in the same revision-checked transaction.
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
	case MoveBlockUpAction, MoveBlockDownAction:
		if action.BlockID, _ = normalize(action.BlockID); action.BlockID == "" {
			return Session{}, ValidationErrors{"placement_action": "Choose a saved block to reorder."}
		}
	case MovePlacementUpAction, MovePlacementDownAction:
		if action.PlacementID, _ = normalize(action.PlacementID); action.PlacementID == "" {
			return Session{}, ValidationErrors{"placement_action": "Choose a placed activity to reorder."}
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
	checkTiming := func(field string, value *int) {
		if value != nil && (*value < 0 || *value > MaxTimingSeconds) {
			errs[field] = "Enter seconds from 0 through 86400, or leave blank."
		}
	}
	checkTiming("allocation_seconds", input.AllocationSeconds)
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
		checkTiming("block_allocation_seconds", block.AllocationSeconds)
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
			checkTiming("placement_duration_seconds", placement.DurationSeconds)
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
