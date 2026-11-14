package session

import (
	"context"
	"errors"
	"testing"

	"github.com/gopher-launch/curu/internal/authorization"
)

const ownerID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"

var owner = authorization.Principal{OwnerID: ownerID}

type fakeStore struct {
	item     Session
	input    Input
	revision int64
	archived bool
	err      error
}

func (s *fakeStore) List(context.Context, string, bool) ([]Summary, error) { return nil, s.err }
func (s *fakeStore) Find(context.Context, string, string, bool) (Session, error) {
	return s.item, s.err
}
func (s *fakeStore) Create(_ context.Context, _ string, input Input) (Session, error) {
	s.input = input
	return s.item, s.err
}
func (s *fakeStore) Update(_ context.Context, _, _ string, revision int64, input Input) (Session, error) {
	s.input, s.revision = input, revision
	return s.item, s.err
}
func (s *fakeStore) Archive(_ context.Context, _, _ string, revision int64, archived bool) (Session, error) {
	s.revision, s.archived = revision, archived
	return s.item, s.err
}

func testPlanner(t *testing.T, store Store) *Planner {
	t.Helper()
	policy, err := authorization.NewPolicy(authorization.ExternalIdentity{Issuer: "https://issuer.example", Subject: "coach"}, ownerID)
	if err != nil {
		t.Fatal(err)
	}
	planner, err := NewPlanner(store, policy)
	if err != nil {
		t.Fatal(err)
	}
	return planner
}

func TestCreateAllowsUndatedSessionWithEmptyBlock(t *testing.T) {
	store := &fakeStore{item: Session{ID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"}}
	planner := testPlanner(t, store)
	_, err := planner.Create(context.Background(), owner, Input{Title: " Future plan ", Blocks: []BlockInput{{Name: " Arrival ", Notes: "Players choose an activity"}}})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if store.input.Title != "Future plan" || store.input.Date != nil || len(store.input.Blocks) != 1 || store.input.Blocks[0].Name != "Arrival" {
		t.Fatalf("stored input = %+v", store.input)
	}
}

func TestUpdateRejectsInvalidAndPreservesAtomicStoreBoundary(t *testing.T) {
	store := &fakeStore{}
	planner := testPlanner(t, store)
	_, err := planner.Update(context.Background(), owner, "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", 3, Input{Title: "Draft", Blocks: []BlockInput{{Name: ""}}})
	if !errors.Is(err, ErrInvalidInput) || store.revision != 0 {
		t.Fatalf("Update() = %v, store revision = %d", err, store.revision)
	}
	store.err = ErrConflict
	input := Input{Title: "Submitted", Blocks: []BlockInput{{Name: "Warmup", ObservationLens: "Spacing"}}}
	_, err = planner.Update(context.Background(), owner, "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", 3, input)
	if !errors.Is(err, ErrConflict) || store.revision != 3 || store.input.Blocks[0].ObservationLens != "Spacing" {
		t.Fatalf("Update conflict = %v; store = %+v", err, store)
	}
}

func TestArchiveRequiresPositiveRevision(t *testing.T) {
	planner := testPlanner(t, &fakeStore{})
	if _, err := planner.Archive(context.Background(), owner, "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", 0, true); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Archive() error = %v", err)
	}
}
