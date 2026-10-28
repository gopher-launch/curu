package initialization

import "testing"

func TestMatchesOnlyExactCompletedRequest(t *testing.T) {
	t.Parallel()
	identity, version := "library", "1"
	cases := []struct {
		name     string
		record   record
		request  request
		expected bool
	}{
		{"empty", record{Kind: KindEmpty}, request{kind: KindEmpty}, true},
		{"different kind", record{Kind: KindEmpty}, request{kind: KindSeed, seedIdentity: identity, seedVersion: version}, false},
		{"exact seed", record{Kind: KindSeed, SeedIdentity: &identity, SeedVersion: &version}, request{kind: KindSeed, seedIdentity: identity, seedVersion: version}, true},
		{"different version", record{Kind: KindSeed, SeedIdentity: &identity, SeedVersion: &version}, request{kind: KindSeed, seedIdentity: identity, seedVersion: "2"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if actual := matches(tc.record, tc.request); actual != tc.expected {
				t.Fatalf("matches() = %v, want %v", actual, tc.expected)
			}
		})
	}
}
