package database

import "testing"

func TestEscapeLikeTreatsSearchMetacharactersLiterally(t *testing.T) {
	t.Parallel()
	if got, want := escapeLike(`50%_wide\field`), `50\%\_wide\\field`; got != want {
		t.Fatalf("escapeLike() = %q, want %q", got, want)
	}
}

func TestNewUUIDProducesCanonicalVersionFourIdentity(t *testing.T) {
	t.Parallel()
	value, err := newUUID()
	if err != nil {
		t.Fatal(err)
	}
	if len(value) != 36 || value[14] != '4' || (value[19] != '8' && value[19] != '9' && value[19] != 'a' && value[19] != 'b') {
		t.Fatalf("newUUID() = %q", value)
	}
}
