// Package activity implements the inward-facing activity library use cases.
package activity

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/gopher-launch/curu/internal/authorization"
)

const MaxSearchBytes = 200

var (
	ErrNotFound     = errors.New("activity not found")
	ErrInvalidQuery = errors.New("invalid activity search")
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
	Progressions    []Progression
}

type Progression struct {
	ID              string
	Position        int
	Title           string
	ContentMarkdown string
	DurationSeconds *int
}

// Store is the owner-scoped persistence boundary used by the library.
type Store interface {
	ListCurrent(context.Context, string, string) ([]Summary, error)
	FindCurrent(context.Context, string, string) (Activity, error)
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
	search = strings.TrimSpace(search)
	if !utf8.ValidString(search) || len(search) > MaxSearchBytes {
		return nil, ErrInvalidQuery
	}
	if err := l.policy.Authorize(principal, authorization.Read, principal.OwnerID); err != nil {
		return nil, err
	}
	items, err := l.store.ListCurrent(ctx, principal.OwnerID, search)
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
	id, err := authorization.NormalizeUUID(id)
	if err != nil {
		return Activity{}, ErrNotFound
	}
	if err := l.policy.Authorize(principal, authorization.Read, principal.OwnerID); err != nil {
		return Activity{}, err
	}
	item, err := l.store.FindCurrent(ctx, principal.OwnerID, id)
	if err != nil {
		return Activity{}, err
	}
	if err := l.policy.Authorize(principal, authorization.Read, item.OwnerID); err != nil {
		return Activity{}, err
	}
	return item, nil
}
