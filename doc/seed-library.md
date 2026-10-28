# Bounded coaching-library seed

CURU-003 adapted the three supplied reference inputs into the committed [`internal/seed/library.json`](../internal/seed/library.json) asset. The asset identifies itself as `curu-coaching-library` version `1`, records the SHA-256 digest of each reference, and contains exactly 44 activities and 110 progressions. CURU-004's explicit initialization command applies it atomically; loading the package does not write a database, and server startup and migrations do not apply it.

## Schema mapping

- Each reference `activity_id` is retained unchanged as `activities.source_identity`. Activity and progression primary keys are deterministic UUIDs derived from their reference IDs and the seed version.
- The lower recommended duration becomes `activities.duration_seconds` after conversion from the reference nanosecond value to integer seconds. The complete single or ranged recommendation remains visible in the activity's `## Metadata` Markdown.
- Themes, inferred game phases, and tags remain in the labeled metadata section. Objective, setup, game play, performance factors, and extra sections such as scoring and constraints remain in labeled Markdown sections.
- Progression order is converted from the reference's one-based sort order to the schema's zero-based `position`. Progression text is retained as Markdown. The references specify no progression duration or title, so those values remain unset and empty.
- The target schema has no separate columns for the reference's maximum duration, themes, phases, tags, or labeled coaching fields. Keeping those values in editable Markdown preserves the supplied content without adding schema outside CURU-003 or pretending the inferred classifications are authoritative.

The adapted counts match the 44 activity and 110 progression inputs, so there are no deliberate omissions. Future correction is normal live-library editing after those product routes exist; changing this committed asset never synchronizes over an initialized working library.

## Verification

`go test ./internal/seed` validates identity/version, counts, deterministic decoding, UUID and source-identity uniqueness, progression order, seconds-based durations, and representative ranged, short, scoring, and constraint content. The disposable PostgreSQL checks documented in [`database-operations.md`](database-operations.md) verify complete atomic initialization and its 44/110 database counts. They never initialize a retained dataset.
