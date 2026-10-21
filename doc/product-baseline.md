# Curu product baseline

This document records the accepted product boundaries for Curu. The detailed backlog and provenance remain in [`roadmap.md`](../roadmap.md); the architecture contract is in [`implementation-design.md`](implementation-design.md). The statements below describe intended post-integration behavior, not current application capability. Curu is a fresh implementation. The old prototype is an excluded thought experiment and is not an implementation or compatibility source.

## Purpose and users

Curu helps a coach or individual maintain reusable activities, assemble and adapt a practical session, consult it on the field, and reuse the result. Rugby is the first concrete use case, while neutral concepts keep the core useful beyond one sport. The MVP serves one owner; collaboration, public signup, organization administration, and multi-tenancy are outside this baseline.

## Core information model

- A session has a title, optional date, intent, and group/context text.
- A session contains ordered, named, freeform blocks. A block can have intent, notes, an observation lens, an optional time allocation, and zero or more ordered activity placements. There is no required stage sequence, stage enum, or recursive grouping.
- Adding an activity to a session copies its usable content, progressions, recommendations, and source provenance into the placement. The saved plan does not depend on a later library lookup. Library edits or archive actions cannot rewrite or break a placement.
- A placement owns local notes and an optional duration. Replacing its source-derived content retains those fields and explicitly warns that the old notes may need review. Moving a placement keeps its properties; block properties stay with the block.
- Cloning a session creates independent content and clears its date. Dates remain optional and no date prompt is required.

Activities are maintained live through the application. They use metadata, optional classification, labeled Markdown coaching fields, and editable progressions. Activities, sessions, and templates can be archived and restored; permanent deletion and revision-history browsers are not MVP features.

Templates reuse independent session content. Date is occurrence-specific and is never part of a template. The copy/clear/default policy for group/context, notes, and other implemented fields is intentionally unresolved until the review in CURU-026; template implementation in CURU-010 must follow that accepted policy.

## Planning and editing behavior

Edits use explicit saves with visible saved, unsaved, validation, and error feedback. Structural actions must not silently discard entered form data. Atomic revision checks reject stale changes rather than overwriting newer work. Submitted stale content remains available, can be saved as a separate recoverable owner-scoped draft, and is never merged or applied automatically.

Durations are integer seconds. A block is counted once: use its explicit allocation when present, otherwise show the complete or partial sum of placement durations. Untimed content is unknown rather than zero. Missing or inconsistent allocations produce non-blocking warnings; a coach may save a valid plan without timeboxes or repeated confirmation.

## On-field experience

Phone reading is a primary experience, separate from editing. It presents a compact block outline, expands copied instructions while retaining position and context, and keeps direct access to any part of the plan. The growing plan remains visible during editing, and direct editing is reachable from anywhere in it. The supplied mockup informs compactness, context, and touch use but does not prescribe a theme, stage wizard, or exact visual design.

A representative plan may contain an arrival runway with two or three activities, a dynamic warmup with roughly ten short activities, three to five main blocks, a game or constraint game, reflection and cheer, and cooldown/stretch. This is an acceptance example, not a mandatory sequence; arrival activities need not all run sequentially.

## Product and delivery boundaries

Curu is a Go modular monolith using GORM with an external PostgreSQL database, explicit schema migrations, server-rendered HTML with HTMX, and minimal JavaScript. It is expected to run behind a gateway on the public internet, probably on AWS initially, without embedding a cloud provider into the application.

The gateway and delivery platform own TLS, network exposure, third-party login, browser session/logout behavior, infrastructure provisioning, database service operation, backup scheduling/retention, and restore execution. Curu owns its schema migrations, initialization, data meaning, compatibility requirements, and post-restore verification.

Curu accepts a signed JWT intended for it, validates that token, maps a stable external identity to an internal principal, and applies application authorization. It does not trust arbitrary identity headers or treat any cryptographically valid JWT as permission to access the owner dataset. The exact token transport, key rotation, issuer/audience/algorithm/time validation, owner binding, authorization, CSRF/origin, proxy-trust, expiration, and logout contracts are review work in CURU-014 and are not settled or implemented here.

Dataset initialization is explicit, atomic, versioned, and separate from startup, ordinary saves, recovery drafts, backup/restore, and disposable reset. The supplied `seedlib_main.go`, `seed_library.sql`, and `library-v2.md` are reference inputs, not prototype implementation. Their known 44 activities and 110 progressions must be accounted for when CURU-003 creates the bounded seed asset. Seed references may be adapted with bounded compatibility changes, but initialization never synchronizes later seed changes over a working library or performs blanket deletion.

Hosting, CI/CD promotion, operational scheduling, identity-provider provisioning, cloud database provisioning, rich CMS features, analytics, AI recommendations, media/calendar integrations, native apps, offline synchronization, automatic conflict merging, and the other non-goals listed in `roadmap.md` remain outside this baseline.
