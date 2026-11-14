// Package activity implements the inward-facing activity library use cases.
package activity

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gopher-launch/curu/internal/authorization"
)

const MaxSearchBytes = 200

const (
	MaxTitleBytes      = 200
	MaxSummaryBytes    = 1000
	MaxContentBytes    = 100000
	MaxClassification  = 100
	MaxTags            = 10
	MaxTagBytes        = 50
	MaxProgressions    = 50
	MaxDurationSeconds = 24 * 60 * 60
)

var (
	ErrNotFound     = errors.New("activity not found")
	ErrInvalidQuery = errors.New("invalid activity search")
	ErrInvalidInput = errors.New("invalid activity input")
	ErrConflict     = errors.New("activity revision conflict")
)

type Summary struct {
	ID              string
	OwnerID         string
	Title           string
	Summary         string
	DurationSeconds *int
}

type Activity struct {
	ID              string
	OwnerID         string
	Title           string
	Summary         string
	ContentMarkdown string
	DurationSeconds *int
	Phase           string
	Theme           string
	Tags            []string
	Revision        int64
	Archived        bool
	Progressions    []Progression
}

type Progression struct {
	ID              string
	Position        int
	Title           string
	ContentMarkdown string
	DurationSeconds *int
}

type Input struct {
	Title           string
	Summary         string
	ContentMarkdown string
	DurationSeconds *int
	Phase           string
	Theme           string
	Tags            []string
	Progressions    []ProgressionInput
}

type ProgressionInput struct {
	Title           string
	ContentMarkdown string
	DurationSeconds *int
}

// RecoveryDraft is an explicitly saved, owner-scoped copy of one conflicting
// activity submission. It is never applied to the authoritative activity.
type RecoveryDraft struct {
	ID, OwnerID, ActivityID string
	BaseRevision            int64
	Input                   Input
	CreatedAt, UpdatedAt    time.Time
}

type RecoveryDraftSummary struct {
	ID, ActivityID, Title string
	UpdatedAt             time.Time
}

type ValidationErrors map[string]string

func (e ValidationErrors) Error() string { return ErrInvalidInput.Error() }

func (e ValidationErrors) Is(target error) bool { return target == ErrInvalidInput }

// Store is the owner-scoped persistence boundary used by the library.
type Store interface {
	ListCurrent(context.Context, string, string) ([]Summary, error)
	ListArchived(context.Context, string, string) ([]Summary, error)
	FindCurrent(context.Context, string, string) (Activity, error)
	FindArchived(context.Context, string, string) (Activity, error)
	Create(context.Context, string, Input) (Activity, error)
	Update(context.Context, string, string, int64, Input) (Activity, error)
	Archive(context.Context, string, string, int64) (Activity, error)
	Restore(context.Context, string, string, int64) (Activity, error)
	ListRecoveryDrafts(context.Context, string) ([]RecoveryDraftSummary, error)
	FindRecoveryDraft(context.Context, string, string) (RecoveryDraft, error)
	SaveRecoveryDraft(context.Context, string, string, int64, Input) (RecoveryDraft, error)
	DiscardRecoveryDraft(context.Context, string, string) error
}

func (l *Library) SaveRecoveryDraft(ctx context.Context, principal authorization.Principal, activityID string, baseRevision int64, input Input) (RecoveryDraft, error) {
	activityID, err := authorization.NormalizeUUID(activityID)
	if err != nil {
		return RecoveryDraft{}, ErrNotFound
	}
	if err := l.policy.Authorize(principal, authorization.Recovery, principal.OwnerID); err != nil {
		return RecoveryDraft{}, err
	}
	input, err = validateInput(input)
	if err != nil {
		return RecoveryDraft{}, err
	}
	if baseRevision < 1 {
		return RecoveryDraft{}, ValidationErrors{"revision": "The activity revision is invalid. Reload and try again."}
	}
	return l.store.SaveRecoveryDraft(ctx, principal.OwnerID, activityID, baseRevision, input)
}

func (l *Library) ListRecoveryDrafts(ctx context.Context, principal authorization.Principal) ([]RecoveryDraftSummary, error) {
	if err := l.policy.Authorize(principal, authorization.Recovery, principal.OwnerID); err != nil {
		return nil, err
	}
	return l.store.ListRecoveryDrafts(ctx, principal.OwnerID)
}

func (l *Library) RecoveryDraft(ctx context.Context, principal authorization.Principal, id string) (RecoveryDraft, error) {
	id, err := authorization.NormalizeUUID(id)
	if err != nil {
		return RecoveryDraft{}, ErrNotFound
	}
	if err := l.policy.Authorize(principal, authorization.Recovery, principal.OwnerID); err != nil {
		return RecoveryDraft{}, err
	}
	draft, err := l.store.FindRecoveryDraft(ctx, principal.OwnerID, id)
	if err != nil {
		return RecoveryDraft{}, err
	}
	if err := l.policy.Authorize(principal, authorization.Recovery, draft.OwnerID); err != nil {
		return RecoveryDraft{}, err
	}
	return draft, nil
}

func (l *Library) DiscardRecoveryDraft(ctx context.Context, principal authorization.Principal, id string) error {
	id, err := authorization.NormalizeUUID(id)
	if err != nil {
		return ErrNotFound
	}
	if err := l.policy.Authorize(principal, authorization.Recovery, principal.OwnerID); err != nil {
		return err
	}
	return l.store.DiscardRecoveryDraft(ctx, principal.OwnerID, id)
}

func (l *Library) Create(ctx context.Context, principal authorization.Principal, input Input) (Activity, error) {
	if err := l.policy.Authorize(principal, authorization.Mutate, principal.OwnerID); err != nil {
		return Activity{}, err
	}
	input, err := validateInput(input)
	if err != nil {
		return Activity{}, err
	}
	return l.store.Create(ctx, principal.OwnerID, input)
}

func (l *Library) Update(ctx context.Context, principal authorization.Principal, id string, revision int64, input Input) (Activity, error) {
	id, err := authorization.NormalizeUUID(id)
	if err != nil {
		return Activity{}, ErrNotFound
	}
	if err := l.policy.Authorize(principal, authorization.Mutate, principal.OwnerID); err != nil {
		return Activity{}, err
	}
	input, err = validateInput(input)
	if err != nil {
		return Activity{}, err
	}
	if revision < 1 {
		return Activity{}, ValidationErrors{"revision": "The activity revision is invalid. Reload and try again."}
	}
	return l.store.Update(ctx, principal.OwnerID, id, revision, input)
}

func (l *Library) Archive(ctx context.Context, principal authorization.Principal, id string, revision int64) (Activity, error) {
	return l.changeArchiveState(ctx, principal, id, revision, true)
}

func (l *Library) Restore(ctx context.Context, principal authorization.Principal, id string, revision int64) (Activity, error) {
	return l.changeArchiveState(ctx, principal, id, revision, false)
}

func (l *Library) changeArchiveState(ctx context.Context, principal authorization.Principal, id string, revision int64, archive bool) (Activity, error) {
	id, err := authorization.NormalizeUUID(id)
	if err != nil {
		return Activity{}, ErrNotFound
	}
	if err := l.policy.Authorize(principal, authorization.Archive, principal.OwnerID); err != nil {
		return Activity{}, err
	}
	if revision < 1 {
		return Activity{}, ValidationErrors{"revision": "The activity revision is invalid. Reload and try again."}
	}
	if archive {
		return l.store.Archive(ctx, principal.OwnerID, id, revision)
	}
	return l.store.Restore(ctx, principal.OwnerID, id, revision)
}

func validateInput(input Input) (Input, error) {
	input.Title = strings.TrimSpace(input.Title)
	input.Summary = strings.TrimSpace(input.Summary)
	input.Phase = strings.TrimSpace(input.Phase)
	input.Theme = strings.TrimSpace(input.Theme)
	errorsByField := ValidationErrors{}
	validateText := func(field, value string, maximum int, required bool) {
		if !utf8.ValidString(value) || len(value) > maximum || (required && value == "") {
			errorsByField[field] = "Enter valid text within the displayed limit."
		}
	}
	validateText("title", input.Title, MaxTitleBytes, true)
	validateText("summary", input.Summary, MaxSummaryBytes, false)
	validateText("content_markdown", input.ContentMarkdown, MaxContentBytes, false)
	validateText("phase", input.Phase, MaxClassification, false)
	validateText("theme", input.Theme, MaxClassification, false)
	if input.DurationSeconds != nil && (*input.DurationSeconds < 0 || *input.DurationSeconds > MaxDurationSeconds) {
		errorsByField["duration_seconds"] = "Use whole seconds from 0 through 86400, or leave this blank."
	}
	if len(input.Tags) > MaxTags {
		errorsByField["tags"] = "Use no more than 10 tags."
	}
	seenTags := map[string]bool{}
	cleanTags := make([]string, 0, len(input.Tags))
	for _, tag := range input.Tags {
		tag = strings.TrimSpace(tag)
		key := strings.ToLower(tag)
		if tag == "" || !utf8.ValidString(tag) || len(tag) > MaxTagBytes {
			errorsByField["tags"] = "Each tag must contain 1 to 50 bytes."
			continue
		}
		if !seenTags[key] {
			seenTags[key] = true
			cleanTags = append(cleanTags, tag)
		}
	}
	input.Tags = cleanTags
	if len(input.Progressions) > MaxProgressions {
		errorsByField["progressions"] = "Use no more than 50 progressions."
	}
	for index := range input.Progressions {
		item := &input.Progressions[index]
		item.Title = strings.TrimSpace(item.Title)
		prefix := fmt.Sprintf("progressions.%d.", index)
		validateText(prefix+"title", item.Title, MaxTitleBytes, false)
		validateText(prefix+"content_markdown", item.ContentMarkdown, MaxContentBytes, false)
		if item.Title == "" && strings.TrimSpace(item.ContentMarkdown) == "" {
			errorsByField[prefix+"content_markdown"] = "Add a title or coaching content, or remove this progression."
		}
		if item.DurationSeconds != nil && (*item.DurationSeconds < 0 || *item.DurationSeconds > MaxDurationSeconds) {
			errorsByField[prefix+"duration_seconds"] = "Use whole seconds from 0 through 86400, or leave this blank."
		}
	}
	if len(errorsByField) != 0 {
		return input, errorsByField
	}
	return input, nil
}

type Library struct {
	store  Store
	policy authorization.Policy
}

func NewLibrary(store Store, policy authorization.Policy) (*Library, error) {
	if store == nil {
		return nil, errors.New("activity store is required")
	}
	return &Library{store: store, policy: policy}, nil
}

func (l *Library) List(ctx context.Context, principal authorization.Principal, search string) ([]Summary, error) {
	return l.list(ctx, principal, search, false)
}

func (l *Library) ListArchived(ctx context.Context, principal authorization.Principal, search string) ([]Summary, error) {
	return l.list(ctx, principal, search, true)
}

func (l *Library) list(ctx context.Context, principal authorization.Principal, search string, archived bool) ([]Summary, error) {
	search = strings.TrimSpace(search)
	if !utf8.ValidString(search) || len(search) > MaxSearchBytes {
		return nil, ErrInvalidQuery
	}
	if err := l.policy.Authorize(principal, authorization.Read, principal.OwnerID); err != nil {
		return nil, err
	}
	var items []Summary
	var err error
	if archived {
		items, err = l.store.ListArchived(ctx, principal.OwnerID, search)
	} else {
		items, err = l.store.ListCurrent(ctx, principal.OwnerID, search)
	}
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if err := l.policy.Authorize(principal, authorization.Read, item.OwnerID); err != nil {
			return nil, err
		}
	}
	return items, nil
}

func (l *Library) Detail(ctx context.Context, principal authorization.Principal, id string) (Activity, error) {
	return l.detail(ctx, principal, id, false)
}

func (l *Library) ArchivedDetail(ctx context.Context, principal authorization.Principal, id string) (Activity, error) {
	return l.detail(ctx, principal, id, true)
}

func (l *Library) detail(ctx context.Context, principal authorization.Principal, id string, archived bool) (Activity, error) {
	id, err := authorization.NormalizeUUID(id)
	if err != nil {
		return Activity{}, ErrNotFound
	}
	if err := l.policy.Authorize(principal, authorization.Read, principal.OwnerID); err != nil {
		return Activity{}, err
	}
	var item Activity
	if archived {
		item, err = l.store.FindArchived(ctx, principal.OwnerID, id)
	} else {
		item, err = l.store.FindCurrent(ctx, principal.OwnerID, id)
	}
	if err != nil {
		return Activity{}, err
	}
	if err := l.policy.Authorize(principal, authorization.Read, item.OwnerID); err != nil {
		return Activity{}, err
	}
	return item, nil
}
