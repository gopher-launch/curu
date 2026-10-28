package seed

import (
	"reflect"
	"strings"
	"testing"
)

func TestLoadReturnsBoundedDeterministicLibrary(t *testing.T) {
	t.Parallel()

	first, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	second, err := Load()
	if err != nil {
		t.Fatalf("second Load() error = %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("repeated Load() returned different content")
	}
	if len(first.Sources) != 3 {
		t.Fatalf("source count = %d, want 3", len(first.Sources))
	}
	for _, source := range first.Sources {
		if source.Name == "" || len(source.SHA256) != 64 {
			t.Errorf("invalid source provenance = %+v", source)
		}
	}
}

func TestLoadPreservesRepresentativeContentAndDurations(t *testing.T) {
	t.Parallel()

	library, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	evasion := findActivity(t, library, "evasion-gauntlet")
	if evasion.DurationSeconds != 300 || !strings.Contains(evasion.ContentMarkdown, "Recommended duration: 5–10 minutes") {
		t.Errorf("evasion duration/content = %d, %q", evasion.DurationSeconds, evasion.ContentMarkdown)
	}
	if len(evasion.Progressions) != 3 || evasion.Progressions[2].ContentMarkdown != "Touch → Wrap." {
		t.Errorf("evasion progressions = %+v", evasion.Progressions)
	}

	short := findActivity(t, library, "macarena-push-ups-circle")
	if short.DurationSeconds != 240 || !strings.Contains(short.ContentMarkdown, "Recommended duration: 4–6 minutes") {
		t.Errorf("short duration/content = %d, %q", short.DurationSeconds, short.ContentMarkdown)
	}

	venn := findActivity(t, library, "v2a-game")
	if venn.DurationSeconds != 600 || !strings.Contains(venn.ContentMarkdown, "### Scoring") {
		t.Errorf("single duration or extra section not retained: %d, %q", venn.DurationSeconds, venn.ContentMarkdown)
	}
	channel := findActivity(t, library, "channel-pressure-game-2-channel-constraint")
	if !strings.Contains(channel.ContentMarkdown, "### Constraint") {
		t.Errorf("constraint section not retained: %q", channel.ContentMarkdown)
	}
}

func findActivity(t *testing.T, library Library, sourceIdentity string) Activity {
	t.Helper()
	for _, activity := range library.Activities {
		if activity.SourceIdentity == sourceIdentity {
			return activity
		}
	}
	t.Fatalf("activity %q not found", sourceIdentity)
	return Activity{}
}
