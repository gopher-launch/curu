// Package seed exposes the bounded coaching-library asset prepared for
// explicit dataset initialization. Applying the asset belongs to CURU-004.
package seed

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
)

const (
	Identity             = "curu-coaching-library"
	Version              = "1"
	ExpectedActivities   = 44
	ExpectedProgressions = 110
)

//go:embed library.json
var files embed.FS

type Library struct {
	SeedIdentity string     `json:"seed_identity"`
	SeedVersion  string     `json:"seed_version"`
	Sources      []Source   `json:"sources"`
	Activities   []Activity `json:"activities"`
}

type Source struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

type Activity struct {
	ID              string        `json:"id"`
	SourceIdentity  string        `json:"source_identity"`
	Title           string        `json:"title"`
	Summary         string        `json:"summary"`
	ContentMarkdown string        `json:"content_markdown"`
	DurationSeconds int           `json:"duration_seconds"`
	Progressions    []Progression `json:"progressions"`
}

type Progression struct {
	ID              string `json:"id"`
	Position        int    `json:"position"`
	Title           string `json:"title"`
	ContentMarkdown string `json:"content_markdown"`
	DurationSeconds *int   `json:"duration_seconds"`
}

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-8[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func Load() (Library, error) {
	content, err := files.ReadFile("library.json")
	if err != nil {
		return Library{}, fmt.Errorf("read embedded seed library: %w", err)
	}
	var library Library
	if err := json.Unmarshal(content, &library); err != nil {
		return Library{}, fmt.Errorf("decode embedded seed library: %w", err)
	}
	if err := validate(library); err != nil {
		return Library{}, fmt.Errorf("validate embedded seed library: %w", err)
	}
	return library, nil
}

func validate(library Library) error {
	if library.SeedIdentity != Identity || library.SeedVersion != Version {
		return errors.New("unexpected seed identity or version")
	}
	if len(library.Activities) != ExpectedActivities {
		return fmt.Errorf("activity count is %d, want %d", len(library.Activities), ExpectedActivities)
	}
	seenIDs := make(map[string]bool)
	seenSources := make(map[string]bool)
	progressionCount := 0
	for _, activity := range library.Activities {
		if !uuidPattern.MatchString(activity.ID) || seenIDs[activity.ID] {
			return fmt.Errorf("invalid or duplicate activity ID %q", activity.ID)
		}
		seenIDs[activity.ID] = true
		if activity.SourceIdentity == "" || seenSources[activity.SourceIdentity] {
			return fmt.Errorf("empty or duplicate source identity %q", activity.SourceIdentity)
		}
		seenSources[activity.SourceIdentity] = true
		if activity.Title == "" || activity.ContentMarkdown == "" || activity.DurationSeconds < 0 {
			return fmt.Errorf("incomplete activity %q", activity.SourceIdentity)
		}
		for index, progression := range activity.Progressions {
			progressionCount++
			if !uuidPattern.MatchString(progression.ID) || seenIDs[progression.ID] {
				return fmt.Errorf("invalid or duplicate progression ID %q", progression.ID)
			}
			seenIDs[progression.ID] = true
			if progression.Position != index || progression.ContentMarkdown == "" {
				return fmt.Errorf("invalid progression %d for %q", index, activity.SourceIdentity)
			}
		}
	}
	if progressionCount != ExpectedProgressions {
		return fmt.Errorf("progression count is %d, want %d", progressionCount, ExpectedProgressions)
	}
	return nil
}
