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
	listOwner, search string
	findOwner, findID string
	items             []Summary
	item              Activity
}

func (s *fakeStore) ListCurrent(_ context.Context, owner, search string) ([]Summary, error) {
	s.listOwner, s.search = owner, search
	return s.items, nil
}

func (s *fakeStore) FindCurrent(_ context.Context, owner, id string) (Activity, error) {
	s.findOwner, s.findID = owner, id
	return s.item, nil
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
