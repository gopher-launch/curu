package database

import "testing"

func TestEscapeLikeTreatsSearchMetacharactersLiterally(t *testing.T) {
	t.Parallel()
	if got, want := escapeLike(`50%_wide\field`), `50\%\_wide\\field`; got != want {
		t.Fatalf("escapeLike() = %q, want %q", got, want)
	}
}
