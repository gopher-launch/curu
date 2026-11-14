package activity

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gopher-launch/curu/internal/authorization"
)

const testOwnerID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"

type fakeStore struct {
	listOwner, search        string
	findOwner, findID        string
	operation                string
	items                    []Summary
	item                     Activity
	createInput, updateInput Input
	updateRevision           int64
	archiveRevision          int64
	drafts                   []RecoveryDraftSummary
	draft                    RecoveryDraft
	draftInput               Input
}

func (s *fakeStore) ListRecoveryDrafts(_ context.Context, owner string) ([]RecoveryDraftSummary, error) {
	s.operation, s.findOwner = "list-drafts", owner
	return s.drafts, nil
}

func (s *fakeStore) FindRecoveryDraft(_ context.Context, owner, id string) (RecoveryDraft, error) {
	s.operation, s.findOwner, s.findID = "find-draft", owner, id
	return s.draft, nil
}

func (s *fakeStore) SaveRecoveryDraft(_ context.Context, owner, id string, revision int64, input Input) (RecoveryDraft, error) {
	s.operation, s.findOwner, s.findID, s.updateRevision, s.draftInput = "save-draft", owner, id, revision, input
	return s.draft, nil
}

func (s *fakeStore) DiscardRecoveryDraft(_ context.Context, owner, id string) error {
	s.operation, s.findOwner, s.findID = "discard-draft", owner, id
	return nil
}

func (s *fakeStore) Create(_ context.Context, _ string, input Input) (Activity, error) {
	s.createInput = input
	return s.item, nil
}

func (s *fakeStore) Update(_ context.Context, _ string, _ string, revision int64, input Input) (Activity, error) {
	s.updateRevision, s.updateInput = revision, input
	return s.item, nil
}

func (s *fakeStore) Archive(_ context.Context, owner, id string, revision int64) (Activity, error) {
	s.operation, s.findOwner, s.findID, s.archiveRevision = "archive", owner, id, revision
	return s.item, nil
}

func (s *fakeStore) Restore(_ context.Context, owner, id string, revision int64) (Activity, error) {
	s.operation, s.findOwner, s.findID, s.archiveRevision = "restore", owner, id, revision
	return s.item, nil
}

func TestLibraryCreateValidatesAndNormalizesEditableContent(t *testing.T) {
	store := &fakeStore{item: Activity{ID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", OwnerID: testOwnerID}}
	library, principal := newTestLibrary(t, store)
	duration := 90
	_, err := library.Create(context.Background(), principal, Input{Title: "  Passing  ", Tags: []string{" Handling ", "handling"}, Progressions: []ProgressionInput{{ContentMarkdown: "Harder"}}, DurationSeconds: &duration})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if store.createInput.Title != "Passing" || len(store.createInput.Tags) != 1 || store.createInput.Tags[0] != "Handling" {
		t.Fatalf("Create() normalized input = %+v", store.createInput)
	}
	_, err = library.Create(context.Background(), principal, Input{Title: ""})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Create() invalid error = %v", err)
	}
}

func TestLibraryUpdateCarriesExpectedRevisionAndStableID(t *testing.T) {
	store := &fakeStore{item: Activity{ID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", OwnerID: testOwnerID}}
	library, principal := newTestLibrary(t, store)
	_, err := library.Update(context.Background(), principal, "BBBBBBBB-BBBB-4BBB-8BBB-BBBBBBBBBBBB", 7, Input{Title: "Renamed"})
	if err != nil || store.updateRevision != 7 || store.updateInput.Title != "Renamed" {
		t.Fatalf("Update() revision/input/error = %d/%+v/%v", store.updateRevision, store.updateInput, err)
	}
}

func TestLibraryRecoveryDraftsUseRecoveryPolicyAndBoundedInput(t *testing.T) {
	activityID := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	draftID := "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	store := &fakeStore{draft: RecoveryDraft{ID: draftID, OwnerID: testOwnerID, ActivityID: activityID}}
	library, principal := newTestLibrary(t, store)

	if _, err := library.SaveRecoveryDraft(context.Background(), principal, strings.ToUpper(activityID), 4, Input{Title: " Submitted "}); err != nil {
		t.Fatalf("SaveRecoveryDraft() error = %v", err)
	}
	if store.operation != "save-draft" || store.findOwner != testOwnerID || store.findID != activityID || store.updateRevision != 4 || store.draftInput.Title != "Submitted" {
		t.Fatalf("SaveRecoveryDraft() store call = %q/%q/%q/%d/%+v", store.operation, store.findOwner, store.findID, store.updateRevision, store.draftInput)
	}
	if _, err := library.RecoveryDraft(context.Background(), principal, strings.ToUpper(draftID)); err != nil || store.operation != "find-draft" || store.findID != draftID {
		t.Fatalf("RecoveryDraft() operation/id/error = %q/%q/%v", store.operation, store.findID, err)
	}
	if err := library.DiscardRecoveryDraft(context.Background(), principal, draftID); err != nil || store.operation != "discard-draft" {
		t.Fatalf("DiscardRecoveryDraft() operation/error = %q/%v", store.operation, err)
	}
	if _, err := library.SaveRecoveryDraft(context.Background(), principal, activityID, 0, Input{Title: "Submitted"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("SaveRecoveryDraft() invalid revision error = %v", err)
	}
	if _, err := library.SaveRecoveryDraft(context.Background(), principal, activityID, 4, Input{Title: ""}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("SaveRecoveryDraft() invalid input error = %v", err)
	}
}

func (s *fakeStore) ListCurrent(_ context.Context, owner, search string) ([]Summary, error) {
	s.operation = "list-current"
	s.listOwner, s.search = owner, search
	return s.items, nil
}

func (s *fakeStore) ListArchived(_ context.Context, owner, search string) ([]Summary, error) {
	s.operation, s.listOwner, s.search = "list-archived", owner, search
	return s.items, nil
}

func (s *fakeStore) FindCurrent(_ context.Context, owner, id string) (Activity, error) {
	s.operation = "find-current"
	s.findOwner, s.findID = owner, id
	return s.item, nil
}

func (s *fakeStore) FindArchived(_ context.Context, owner, id string) (Activity, error) {
	s.operation, s.findOwner, s.findID = "find-archived", owner, id
	return s.item, nil
}

func TestLibraryArchivedViewsAndTransitionsUseOwnerRevision(t *testing.T) {
	id := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	store := &fakeStore{items: []Summary{{ID: id, OwnerID: testOwnerID}}, item: Activity{ID: id, OwnerID: testOwnerID, Archived: true}}
	library, principal := newTestLibrary(t, store)

	if _, err := library.ListArchived(context.Background(), principal, " retained "); err != nil || store.operation != "list-archived" || store.search != "retained" {
		t.Fatalf("ListArchived() operation/search/error = %q/%q/%v", store.operation, store.search, err)
	}
	if _, err := library.ArchivedDetail(context.Background(), principal, strings.ToUpper(id)); err != nil || store.operation != "find-archived" || store.findID != id {
		t.Fatalf("ArchivedDetail() operation/id/error = %q/%q/%v", store.operation, store.findID, err)
	}
	if _, err := library.Archive(context.Background(), principal, id, 4); err != nil || store.operation != "archive" || store.archiveRevision != 4 || store.findOwner != testOwnerID {
		t.Fatalf("Archive() operation/revision/owner/error = %q/%d/%q/%v", store.operation, store.archiveRevision, store.findOwner, err)
	}
	if _, err := library.Restore(context.Background(), principal, id, 5); err != nil || store.operation != "restore" || store.archiveRevision != 5 {
		t.Fatalf("Restore() operation/revision/error = %q/%d/%v", store.operation, store.archiveRevision, err)
	}
	if _, err := library.Archive(context.Background(), principal, id, 0); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Archive() invalid revision error = %v", err)
	}
}

func TestLibraryUsesOwnerScopeAndNormalizesSearch(t *testing.T) {
	store := &fakeStore{items: []Summary{{ID: "one", OwnerID: testOwnerID, Title: "Known"}}}
	library, principal := newTestLibrary(t, store)
	items, err := library.List(context.Background(), principal, "  passing drill  ")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(items) != 1 || store.listOwner != testOwnerID || store.search != "passing drill" {
		t.Fatalf("List() = %+v, owner/search = %q/%q", items, store.listOwner, store.search)
	}
}

func TestLibraryRejectsInvalidSearchBeforeStore(t *testing.T) {
	store := &fakeStore{}
	library, principal := newTestLibrary(t, store)
	_, err := library.List(context.Background(), principal, strings.Repeat("x", MaxSearchBytes+1))
	if !errors.Is(err, ErrInvalidQuery) || store.listOwner != "" {
		t.Fatalf("List() error = %v, store owner = %q", err, store.listOwner)
	}
}

func TestLibraryDetailUsesStableIDAndRejectsOtherOwnerResult(t *testing.T) {
	const id = "BBBBBBBB-BBBB-4BBB-8BBB-BBBBBBBBBBBB"
	store := &fakeStore{item: Activity{ID: strings.ToLower(id), OwnerID: "cccccccc-cccc-4ccc-8ccc-cccccccccccc"}}
	library, principal := newTestLibrary(t, store)
	_, err := library.Detail(context.Background(), principal, id)
	if !errors.Is(err, authorization.ErrDenied) {
		t.Fatalf("Detail() error = %v, want authorization denial", err)
	}
	if store.findOwner != testOwnerID || store.findID != strings.ToLower(id) {
		t.Fatalf("FindCurrent owner/id = %q/%q", store.findOwner, store.findID)
	}

	store.findID = ""
	_, err = library.Detail(context.Background(), principal, "not-an-id")
	if !errors.Is(err, ErrNotFound) || store.findID != "" {
		t.Fatalf("invalid Detail() error = %v, store id = %q", err, store.findID)
	}
}

func newTestLibrary(t *testing.T, store Store) (*Library, authorization.Principal) {
	t.Helper()
	policy, err := authorization.NewPolicy(authorization.ExternalIdentity{Issuer: "issuer", Subject: "owner"}, testOwnerID)
	if err != nil {
		t.Fatal(err)
	}
	library, err := NewLibrary(store, policy)
	if err != nil {
		t.Fatal(err)
	}
	return library, authorization.Principal{OwnerID: testOwnerID}
}
