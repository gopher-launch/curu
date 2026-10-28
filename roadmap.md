---
version: 1
project: curu
updated: 2026-10-28
---

# Curu product roadmap

## Roadmap status and source

This is the approved initial product baseline and task backlog for the new Curu repository. It was prepared from `curu-product-roadmap-draft.md` (SHA-256 `e8d6131c6917840ae93e54c303057ce99bd9884b54ff2e8dda9a0ff31e2dc901`). CURU-001 and CURU-014 delivered the documentation and integration-contract baseline. CURU-002 delivered the minimal runnable application shell. CURU-019 delivered explicit PostgreSQL migrations, restricted runtime grants, and read-only startup compatibility. CURU-003 delivered the bounded coaching-library seed asset. CURU-004 delivered safe explicit seeded or intentionally empty dataset initialization; no product UI or authentication capability has been delivered yet.

Chris and Chappie own product direction. The accepted baseline below incorporates the October 19 decisions and the CURU-014 authentication contract. Recovery-draft mechanics and template field mapping remain reviewable design work, not silently accepted implementation choices.

Start from scratch. The old Curu prototype is an excluded thought experiment, not a compatibility target or source to mine. Reuse only the explicitly supplied library/seed reference inputs. No existing application repository needs an architectural salvage assessment.

## Product purpose and accepted baseline

Curu helps a coach or individual maintain reusable activities, assemble/adapt a practical session, consult it on the field, and reuse the plan. Rugby is the first concrete use case. Broader applicability shapes terminology without adding sports plugins, recursive development hierarchies or speculative audiences.

- One Go modular monolith, one external Postgres database, GORM with explicit migrations, server-rendered HTML/HTMX and minimal JavaScript.
- The application runs behind a gateway over public internet access, probably on AWS initially. Hosting, TLS, network exposure, identity-provider/gateway provisioning, database service and operational scheduling belong to the delivery/platform effort. Curu owns its interface contracts and data meaning/compatibility.
- MVP serves one owner. Curu accepts a trusted JWT and maps it to an internal authN/authZ model. Login/provider details stay outside application/domain logic.
- Session → ordered named blocks → zero or more ordered activity placements. No stage enum, mandatory sequence or recursive groups.
- Sessions support title, optional date, intent and group/context text. Blocks hold intent, notes, observation lens and optional allocation. Placements own copied activity content, local notes, optional duration and source provenance.
- Adding an activity copies its usable content; saved plans do not depend on future library lookups or historical library revisions. Library edits/archive cannot rewrite a plan. Plan edit history is outside MVP.
- Replacing an activity retains placement duration and local notes. Make source-content replacement explicit and flag that old notes may need review.
- Placements move between blocks with their own properties retained; block-specific properties remain on the block.
- Cloning a session copies independent content and clears its date. Date remains optional; a date prompt is not required.
- Live activity creation/editing is primary. Use metadata plus labeled Markdown fields and editable coaching content/progressions. Archive/restore activities, sessions and templates; permanent deletion is deferred.
- Use explicit saves with visible saved/unsaved/error feedback. Structural actions must not silently discard unsaved inputs.
- Detect stale edits atomically. Preserve submitted work, offer a separate recoverable copy, then reload current truth deliberately. No automatic merge, silent overwrite or full edit-history requirement.
- Store durations in integer seconds. Missing or inconsistent planning allocations warn without blocking valid saves or demanding repeated confirmation. A coach may plan without timeboxes.
- Dedicated phone reading is the target. Expand instructions within a compact outline and preserve context/place. Keep the growing plan visible during editing; direct editing is available anywhere. The supplied mockup is a reference, not a mandatory theme/stage wizard.
- Date is occurrence-specific and excluded from templates. Other session-context treatment remains for CURU-026; do not guess that boundary now.

Reference session: arrival runway with two or three activities; dynamic warmup with roughly ten short activities; three to five main activity blocks; a game/constraint game; reflection and cheer; cooldown/stretch. Not every arrival activity must run sequentially.

Timing semantics: count a block once, using its explicit allocation if supplied, otherwise a complete/partial sum of placement durations. Untimed content remains visibly unknown, not zero. Warn on uncertainty or budget mismatch without turning it into an obstacle to planning.

## JWT and authorization boundary

Accepted direction: the gateway handles third-party authentication and presents a signed token intended for Curu. Curu verifies it, maps a principal, then applies its own permissions. It does not accept arbitrary identity headers or treat any valid JWT as authority over the dataset.

The accepted CURU-014 contract in `doc/integration-contract.md` specifies:

- A deployment-selected access-token validation profile and dedicated Curu resource audience, plus configured trust for signing keys/rotation, issuer, algorithms and required lifetime checks. Prefer the standard OAuth JWT access-token profile; a suitable existing token may be forwarded without a custom Curu type or reissuance. Never select trust or validation profiles from an unverified token, and never fall back to accepting generic signed JWTs.
- Stable external identity based on issuer/subject, mapped to an internal principal. A configured single-owner binding is enough for MVP; changing provider bindings should not require changing application content identity.
- A small application policy authorizes read/mutate/archive/recovery actions. Keep raw JWT claims and third-party role names outside domain behavior.
- Gateway owns login, refresh and browser session/logout mechanics. Curu enforces the presented token's validity and handles expiry safely. The browser CSRF/origin/proxy contract must be explicit even if the gateway injects Authorization.
- Values such as deployment issuer URL, audience and keys are configuration, not reasons to bake AWS or one identity provider into the model. No new token issuer, password database or policy engine is required in Curu.

Technical references: the standard [OAuth JWT access-token profile, RFC 9068](https://www.rfc-editor.org/rfc/rfc9068.html), JWT explicit typing and mutually exclusive profiles in RFC 8725 [§3.11](https://www.rfc-editor.org/rfc/rfc8725.html#section-3.11) and [§3.12](https://www.rfc-editor.org/rfc/rfc8725.html#section-3.12), [HTTP authentication challenges, RFC 9110 §15.5.2](https://www.rfc-editor.org/rfc/rfc9110.html#section-15.5.2), [Bearer challenges, RFC 6750 §3](https://www.rfc-editor.org/rfc/rfc6750.html#section-3), and [conditional updates, RFC 9110 §13.1.1](https://www.rfc-editor.org/rfc/rfc9110.html#section-13.1.1). HTTP conditional requests are one transport option; database compare-and-update is still required to prevent a lost update.

## Data-service contract

The external service supplies storage, backup retention/scheduling, restore execution and infrastructure security. Curu owns schema migrations, dataset initialization, semantic validity, compatibility expectations and application verification after restore. No cloud SDK or provisioner is necessary merely to consume Postgres.

Separate migrations, initial seed/bootstrap, ordinary UI saves, recovery drafts, backup/restore and disposable reset. Runtime credentials do not own the schema. Startup never resets data or runs ORM-inferred migrations.

Reuse useful content from seedlib_main.go/seed_library.sql/library-v2.md with bounded compatibility changes: seconds, explicit schema mapping and no blanket deletes. Prior inspection found 44 activity and 110 progression inputs; account for any deliberate changes. High-fidelity import/inference is not a product milestone.

Initialization is explicit, atomic and recorded with a seed identity/version. Completed repeats are no-ops, partial failures roll back and concurrent invocations do not duplicate content. Refuse an unrecognized non-empty dataset instead of merging over live edits. Support a recorded intentionally empty installation. Later seed changes do not synchronize over the working library.

Recovery drafts preserve attempted user content separately from authoritative records. Their location, bounded payload/ownership and explicit retention/discard behavior are specified before implementation. Recommended initial behavior is an owner-scoped durable draft, not an automatic conflict merge or an offline working-copy system.

## Delivery rules

- Execute/review one bounded Task at a time. Epics and Features organize work; only Kind: task entries can be selected. A milestone captures delivery scope across parents, not permission to implement a whole range.
- CURU-001 made CURU-014 ready. Their documentation and reviewed integration-contract baselines made CURU-002 ready. The delivered application shell made CURU-019 ready; its delivered migration boundary made CURU-003 ready. The delivered bounded seed asset made CURU-004 ready; its delivered explicit initializer makes CURU-015 the next ready Task. Other Tasks remain planned. Promote the next eligible Task through reviewed candidate reconciliation when its concrete prerequisites and acceptance are satisfied; do not promote unresolved product choices automatically.
- Every candidate includes relevant documentation, focused verification, and concise roadmap reconciliation. Source is the truth of implemented behavior. Preserve existing IDs; no Task is delivered solely because it appears in this document.
- CURU-001 through CURU-018 retain their draft identities. CURU-019 through CURU-027 split broad scopes/add explicit authorization, recovery and template-boundary work; detailed outcomes below are authoritative.
- Product choices remain with Chris/Chappie. Design Tasks may produce explicit proposals for operator review. Do not invent approvals, fabricate human field results or silently turn a design question into code.
- Keep later Tasks at useful planning depth. Before readiness, bound acceptance against the actual source. If a Task proves too large, revise/split it with traceable IDs rather than widening the selected scope.
- Go/build/router/migration/library choices are routine implementation decisions within the accepted architecture. Use maintained standard tooling and document the choice; no broad platform abstraction.
- Verify meaningful behavior with disposable fixtures/databases and local token fixtures. Validate pure domain rules without external services; database/auth/browser boundaries get focused integration checks. No paid provider calls, real-data reset or cloud deployment is required to validate routine changes.
- Initial Tasks must establish exact contributor/build/test instructions and mandatory checks. Follow those subsequently; do not invent command names from this planning document. Real phone/on-field and external-service checks remain honest operator evidence.
- Provisioning, CI/CD promotion and publication remain separate work. No automatic push/deploy is implied by a Task or milestone.
- Non-goals: microservices, event buses, GraphQL, SPA framework, athlete tracking, attendance/analytics, AI recommendations, public signup, collaboration/multi-tenancy, rich CMS, template inheritance, revision-history browsers, PDF service, calendar/media/native app, drag-and-drop, offline sync and automatic conflict merge.

## Delivery checkpoints

| Checkpoint | Tasks | Reviewable result |
| --- | --- | --- |
| M0: product and integration contracts | CURU-001, CURU-014 | Repository baseline and explicit JWT/authZ/data-service contract; no runtime delivery claimed |
| M1: secure editable activity library | CURU-002, CURU-019, CURU-003, CURU-004, CURU-015, CURU-020, CURU-016, CURU-013, CURU-005, CURU-017, CURU-021, CURU-022 | Protected live library authoring/archive, safe initialization, isolated recovery evidence and stale-edit recovery |
| M2: first complete session for field use | CURU-006, CURU-007, CURU-023, CURU-024, CURU-008, CURU-025, CURU-018 | Flexible multi-activity plans, optional timing, recoverable conflicts and phone reading; Chris verifies real field usability |
| M3: repeat planning | CURU-009, CURU-026, CURU-010, CURU-027 | Independent session copies and templates under an explicitly accepted reusable/occurrence field policy |
| M4: MVP evidence and coaching acceptance | CURU-011, CURU-012 | Useful discovery and integrated evidence; Chris's actual-session acceptance remains a separate observed decision |

Milestones list every Task once. Concrete Task dependencies below determine eligibility; milestone labels are not dependency references. M1/M2 include foundation safety and their own verification. M4 is not deferred security or testing. Actual coaching acceptance is an operator outcome and is not inferred from delivery of CURU-012.

## Backlog organization

Epic/Feature IDs below are grouping entries, never selectable execution units. Standalone Tasks have Parent: none. Parent-group status is reconciled conservatively from actual scope; this roadmap does not assume the runner automatically calculates it.

## CURU-E001 — Establish the application and data foundation

- Kind: epic
- Status: planned
- Priority: high
- Parent: none
- Depends on: none

Deliver the runnable application, explicit data lifecycle, JWT boundary and application authorization without coupling Curu to a hosting provider.

## CURU-E002 — Maintain a living activity library

- Kind: epic
- Status: planned
- Priority: high
- Parent: none
- Depends on: none

Make useful activities easy to find, create, adapt and archive through the protected UI.

## CURU-E003 — Plan, consult and reuse coaching sessions

- Kind: epic
- Status: planned
- Priority: high
- Parent: none
- Depends on: none

Deliver flexible multi-activity plans, reliable saves, phone reading, duplication and reusable templates.

## CURU-F001 — Establish the runnable application and persistence

- Kind: feature
- Status: delivered
- Priority: high
- Parent: CURU-E001
- Depends on: none

CURU-002 and CURU-019 establish the small greenfield runtime and explicit Postgres ownership.

## CURU-F002 — Initialize and recover application data safely

- Kind: feature
- Status: planned
- Priority: high
- Parent: CURU-E001
- Depends on: none

CURU-003, CURU-004 and CURU-013 adapt seed content and demonstrate initialization/recovery contracts.

## CURU-F003 — Enforce trusted identity and application permissions

- Kind: feature
- Status: planned
- Priority: high
- Parent: CURU-E001
- Depends on: none

CURU-015, CURU-020 and CURU-016 implement JWT verification, local policy and safe browser requests.

## CURU-F004 — Find relevant activities

- Kind: feature
- Status: planned
- Priority: high
- Parent: CURU-E002
- Depends on: none

CURU-005 and CURU-011 deliver browsing, search and useful predictable filters.

## CURU-F005 — Edit and retain the live library

- Kind: feature
- Status: planned
- Priority: high
- Parent: CURU-E002
- Depends on: none

CURU-017, CURU-021 and CURU-022 provide explicit authoring, archive/restore and conflict recovery.

## CURU-F006 — Assemble and adapt a session plan

- Kind: feature
- Status: planned
- Priority: high
- Parent: CURU-E003
- Depends on: none

CURU-006, CURU-007, CURU-023, CURU-024, CURU-008 and CURU-025 deliver blocks, copied placements, flexible timing and safe edits.

## CURU-F007 — Read the plan on the field

- Kind: feature
- Status: planned
- Priority: high
- Parent: CURU-E003
- Depends on: none

CURU-018 delivers the compact phone view with expandable instructions and retained context.

## CURU-F008 — Reuse sessions and templates

- Kind: feature
- Status: planned
- Priority: high
- Parent: CURU-E003
- Depends on: none

CURU-009, CURU-026, CURU-010 and CURU-027 deliver independent duplication and templates after the field policy is accepted.

## CURU-001 — Record the greenfield product baseline and application boundaries

- Kind: task
- Status: delivered
- Priority: high
- Parent: none
- Depends on: none

**Objective:** Create the repository's concise product/architecture baseline from the accepted Curu direction and the supplied roadmap; do not reopen settled choices or reuse the prototype.

**Acceptance criteria:**

- Record Go, GORM/Postgres, explicit migrations, server-rendered HTML/HTMX, single-owner MVP, optional dates, named multi-activity blocks, copied activity content, live authoring, explicit saves, archive/restore, and phone reading.
- Record the public-access/data-service responsibility split and the agreed mockup principles. Identify detailed JWT integration defaults and template context conversion as design work below, not implementation-agent product authority.
- Define build/test/start/migrate/bootstrap interfaces and documentation locations at a useful contract level. Record that this is a fresh implementation; the prototype is not an implementation source.
- Preserve all roadmap IDs and input provenance. No application behavior, hosting deployment, credentials, or completed capability is claimed.

**Verification:** Review documentation against the accepted decisions and dependency graph. No model/provider or deployment trial is required.

## CURU-014 — Specify the JWT, authorization and data-service contracts

- Kind: task
- Status: delivered
- Priority: high
- Parent: none
- Depends on: CURU-001

**Objective:** Turn the accepted gateway boundary into a small reviewable integration contract, including explicit technical defaults and the data-service split.

**Acceptance criteria:**

- Gateway/ingress owns third-party login and browser session lifecycle; Curu validates its signed, intended JWT and maps identity to an internal principal. Specify token delivery, configured trust/key rotation, issuer/audience, accepted algorithms, time checks, stable identity, and failure behavior.
- Specify the minimal single-owner authorization policy at application use cases: a valid token alone does not authorize access. Proposed initial binding is a configured issuer/subject pair mapped to one internal owner. No public signup, arbitrary token-controlled roles, tenant platform, or external policy engine.
- Define responsibility for CSRF, origin/proxy trust, expiration responses and deployment-specific logout. Do not assume internal JWT transport removes browser CSRF exposure.
- Specify external Postgres/backup/restore responsibilities and Curu's migrations, initialization, compatibility, credentials, runtime privileges, and recovery obligations. No cloud provider is selected by application code.
- Make any material unresolved choice explicit for Chris; documentation may propose details, but must not report them as accepted or implemented. Accepted integration of this design candidate is the record of the chosen contract.

**Verification:** Review standard-profile access-token acceptance, wrong-kind/profile rejection, trust/rotation, challenge and access examples, data lifecycle scenarios, and clear responsibilities. Provider-specific profiles require explicit distinguishing rules and negative fixtures before use. No live identity provider or AWS resources are required.

## CURU-002 — Create the minimal runnable Go application

- Kind: task
- Status: delivered
- Priority: high
- Parent: CURU-F001
- Depends on: CURU-014

**Objective:** Establish a small buildable greenfield application shell.

**Acceptance criteria:**

- Add the Go module, cmd/curu, configuration validation, consistent layout/navigation and minimal health/logging/shutdown behavior.
- Use net/http with a small router and server-rendered templates. Leave activity/session/template behavior for their Tasks.
- Provide reproducible build/test/start commands and a minimal non-root application container interface without infrastructure provisioning.
- Do not serve placeholder private data through an unauthenticated bypass. Authentication is implemented below; missing required runtime security configuration cannot silently produce a remotely usable private app.

**Verification:** Focused startup/configuration/health checks, Go tests/build, and the documented local launch. Do not require a cloud deployment.

## CURU-019 — Establish Postgres migrations and restricted runtime access

- Kind: task
- Status: delivered
- Priority: high
- Parent: CURU-F001
- Depends on: CURU-002

**Objective:** Deliver explicit schema ownership and the initial library/data-lifecycle persistence boundary.

**Acceptance criteria:**

- Add explicit initial migrations for current activities, content/progressions as needed, bootstrap metadata and revision tokens. Do not invent the full future schema.
- Use GORM for persistence without runtime AutoMigrate. Keep migration credentials/ownership separate from runtime data permissions.
- Demonstrate fresh migration and forward application on an existing fixture with retained content. Startup verifies compatibility instead of resetting data.
- Document connection, credential and external database assumptions independent of AWS.

**Verification:** Disposable Postgres integration checks for schema, retained data, and runtime-role restrictions; verify migrations do not run as a side effect of ordinary startup.

## CURU-003 — Adapt the existing seed without rebuilding the importer

- Kind: task
- Status: delivered
- Priority: high
- Parent: CURU-F002
- Depends on: CURU-019

**Objective:** Salvage the useful supplied activity seed for the new schema.

**Acceptance criteria:**

- Use supplied seedlib_main.go/seed_library.sql/library-v2.md as reference inputs only. Retain useful content and source identities; keep UI correction the normal future maintenance path.
- Convert nanosecond duration values to integer seconds and remove blanket deletion/replacement from normal seed application.
- Produce a deterministic bounded seed asset compatible with migrations. Account for the known 44 activity and 110 progression inputs, explaining any deliberate differences.
- Do not rebuild a high-fidelity Markdown pipeline or spend scope on perfect inferred classification.

**Verification:** Load the adapted asset against the migrated disposable database; inspect representative extra sections, short/ranged durations, and repeatable generated or committed content.

## CURU-004 — Implement safe explicit dataset initialization

- Kind: task
- Status: delivered
- Priority: high
- Parent: CURU-F002
- Depends on: CURU-003

**Objective:** Initialize a new application dataset safely without synchronizing over live content.

**Acceptance criteria:**

- Apply seed content and its seed identifier/version atomically; completed retries are no-ops and partial failures roll back.
- Handle concurrent initializers without double application. Refuse an unrecognized non-empty dataset; support a recorded intentionally empty installation.
- Keep ordinary startup and UI editing separate from initialization. Changed seed assets never silently update existing activities.
- Document disposable reset as a separate explicit operator operation against a named isolated target, absent from the web app. Do not implement infrastructure lifecycle orchestration.

**Verification:** Disposable Postgres checks for fresh, empty, repeated, concurrent, failed and non-empty-target cases; prove edited content survives retries/startup.

## CURU-015 — Validate trusted JWTs and map an internal principal

- Kind: task
- Status: ready
- Priority: high
- Parent: CURU-F003
- Depends on: CURU-002, CURU-014

**Objective:** Implement the accepted authentication adapter with standard maintained libraries.

**Acceptance criteria:**

- Implement the standard OAuth JWT access-token profile and validate its type/required claims, cryptographic signature, configured issuer/resource audience, allowed algorithms and required temporal claims before mapping a principal. No bespoke Curu token type or gateway reissuance is required for a suitable existing access token.
- Make the supported validation profile an explicit deployment setting, never selected by an unverified token or weakened by fallback. Do not build a generic profile framework; provider-specific support is added only for a concrete reviewed integration need, with wrong-kind rejection fixtures.
- Resolve signing keys only through configured trusted material/endpoints with bounded rotation/failure behavior. Reject unsigned, substituted, wrong-purpose, wrong-profile, malformed, expired and untrusted tokens.
- Keep raw JWTs/provider claim names outside application/domain behavior. Use stable external identity rather than editable email/display names.
- Do not implement login, JWT issuance, token exchange, a production signer, or an identity-provider integration inside Curu.

**Verification:** Local signing fixtures prove standard-profile success for both access-token media-type forms and their case-insensitive equivalents; missing/wrong type or required claims; rejection of same-issuer ID tokens, wrong-purpose tokens, unsupported profiles and fallback attempts; wrong issuer/audience/key/algorithm; expiry; and rotation-overlap behavior. No real login, gateway token issuer or cloud resource is needed.

## CURU-020 — Enforce Curu's single-owner authorization policy

- Kind: task
- Status: planned
- Priority: high
- Parent: CURU-F003
- Depends on: CURU-015, CURU-014

**Objective:** Authorize use of the application's private data independently of authentication success.

**Acceptance criteria:**

- Map the accepted owner binding to a small internal policy and apply it consistently at application use-case boundaries.
- Unknown authenticated subjects cannot read or mutate Curu data, including guessed object URLs, HTMX routes, archives and recovery drafts.
- Treat token role/group claims as inputs only where the agreed contract explicitly maps them; deny by default.
- Provide explicit unauthenticated/unauthorized behavior and the minimal safe health exception defined by the contract. Do not add RBAC administration or multi-tenancy.

**Verification:** Principal/policy and handler tests distinguish missing/invalid credentials, valid but unauthorized identity, and the authorized owner; prove no alternate route bypass.

## CURU-016 — Establish protected browser and HTMX request handling

- Kind: task
- Status: planned
- Priority: high
- Parent: CURU-F003
- Depends on: CURU-020

**Objective:** Deliver the shared request protections used by every data-backed page and mutation.

**Acceptance criteria:**

- Implement the accepted CSRF/origin/proxy-trust contract for browser traffic, including when the gateway injects Authorization. GET is read-only.
- Use escaped templates and safely rendered Markdown, bounded validated inputs, parameterized queries, and secret-safe logging/errors.
- Handle missing, invalid and expired authentication with the contract status and Bearer challenge, without disguising an expired-token save as successful or silently discarding an open editor.
- Document gateway responsibilities for TLS, cookies, login/logout, identity-token forwarding and stripping conflicting client headers; no provider-specific gateway deployment.

**Verification:** Normal and HTMX request tests cover rejected cross-origin/invalid mutations, safe content rendering, input errors, missing/invalid/expired authentication with the required challenge, preserved editor input, and secret-free output.

## CURU-013 — Demonstrate the application data recovery contract

- Kind: task
- Status: planned
- Priority: high
- Parent: CURU-F002
- Depends on: CURU-004, CURU-015

**Objective:** Verify that Curu can recover using the external database service's backup/restore capability.

**Acceptance criteria:**

- Document supported schema/application compatibility, included datasets, credential handling, trust reconfiguration, and post-restore validation.
- Exercise a backup/restore into an isolated disposable target, retaining edited content and bootstrap metadata. Extend fixtures when later Tasks introduce plans/templates/recovery drafts.
- Keep backup scheduling, retention/storage provisioning and managed-service operation with the delivery/data service; Curu supplies its recovery requirements and checks.
- No unattended operation on real retained data, cloud infrastructure creation, or restore over an operator's existing dataset.

**Verification:** A reproducible isolated Postgres restore exercise and assertions against original application content; report what was actually tested.

## CURU-005 — Browse and search the current activity library

- Kind: task
- Status: planned
- Priority: high
- Parent: CURU-F004
- Depends on: CURU-004, CURU-016

**Objective:** Expose protected library list/detail pages and basic discovery.

**Acceptance criteria:**

- Show current seed or user content clearly, including instructions/progressions and meaningful extra content.
- Provide basic title/text search, useful empty states and stable links independent of title renaming.
- Do not require rugby classification for an activity or treat source-file grouping as a session stage.
- Use the shared authentication, authorization, rendering and request protections.

**Verification:** Repository/query and browser/handler checks for known items, empty library, search, complete detail content and denied access.

## CURU-017 — Create and edit activities with explicit saves

- Kind: task
- Status: planned
- Priority: high
- Parent: CURU-F005
- Depends on: CURU-005

**Objective:** Make the live database maintainable from the UI.

**Acceptance criteria:**

- Provide metadata and labeled Markdown coaching-content fields, editable progressions, optional classification and integer-second duration recommendations.
- Stable identity survives renaming; explicit save actions show saved/unsaved/error state and preserve submitted values on validation failure.
- Perform atomic revision-checked updates from the outset. A stale submission is rejected without losing its input; durable recovery is completed in CURU-022.
- Do not build a rich-text platform, activity version-history browser or automatic import reconciliation.

**Verification:** Create/edit/reopen tests and a stale-editor fixture prove persistence, safe rendering, validation recovery, no lost update and content independent of title.

## CURU-021 — Archive and restore activities

- Kind: task
- Status: planned
- Priority: high
- Parent: CURU-F005
- Depends on: CURU-017

**Objective:** Remove activities from the working library without destructive deletion.

**Acceptance criteria:**

- Archive hides activities from normal browsing/selection while a protected archived view permits restoration.
- Use the current revision and authorization policy for archive/restore. A stale action cannot overwrite a newer edit.
- Archived activities remain identifiable and do not disappear from existing plan-owned copies introduced later.
- Permanent deletion is outside this release.

**Verification:** Archive/search/restore scenarios, stale actions and access checks; later snapshot tests prove plan isolation.

## CURU-022 — Preserve conflicting activity edits as recovery drafts

- Kind: task
- Status: planned
- Priority: high
- Parent: CURU-F005
- Depends on: CURU-017, CURU-020

**Objective:** Provide the agreed non-destructive recovery path when an activity changes underneath an editor.

**Acceptance criteria:**

- On stale save, leave authoritative content unchanged and retain the submitted editable data. Explain that a newer revision exists.
- Offer an explicit save of a separate owner-scoped recovery draft before reloading the latest record; confirm draft persistence before replacing the editor.
- Let the owner reopen or explicitly discard the recovered content. A recovery draft is never silently applied over the newer source.
- Keep payload format and size/ownership checks bounded. Document storage/retention with the data contract; no automatic merge, offline sync or unlimited edit history.

**Verification:** Two-editor tests prove compare-and-update atomicity, exact submitted-content recovery, reload, owner isolation and draft-save failure without loss of the open form.

## CURU-006 — Create sessions with named freeform blocks

- Kind: task
- Status: planned
- Priority: high
- Parent: CURU-F006
- Depends on: CURU-013, CURU-021, CURU-022

**Objective:** Persist concrete session plans independently of the activity library.

**Acceptance criteria:**

- Create/list/open/edit sessions with title, optional date, group/context and intent, plus named freeform blocks with notes and observation lens.
- Dates and duration allocations are optional; draft future plans can be saved without either.
- Use explicit saves, atomic revision checks and input preservation. Do not allow structural actions to silently discard other unsaved editor content.
- Archive/restore sessions with the same user-facing lifecycle principles as activities; no SessionStage enum or recursive groups.

**Verification:** Save/reopen, undated/freeform draft, validation, archive/restore and stale-save checks; prove no activity is required for a block.

## CURU-007 — Persist plan-owned activity snapshots

- Kind: task
- Status: planned
- Priority: high
- Parent: CURU-F006
- Depends on: CURU-006, CURU-017

**Objective:** Store independent activity occurrences inside session blocks.

**Acceptance criteria:**

- A block owns zero or more ordered placements. Atomically copy coherent usable source content, progressions, recommendations and source provenance into each placement.
- Rendering an existing placement requires no current library lookup. Library edits, archive, and removal of a source reference cannot mutate or break the plan.
- Plan-local adaptations remain separate from current library content. Source activity identity is provenance, not shared mutable content.
- Use ordinary owned records or a bounded content column; historical library revisions or one opaque canonical session object are not prerequisites.

**Verification:** Database tests for snapshot consistency, multiple occurrences, independent adaptation, source changes/removal and transaction rollback.

## CURU-023 — Select, replace and move activities within the session editor

- Kind: task
- Status: planned
- Priority: high
- Parent: CURU-F006
- Depends on: CURU-007

**Objective:** Deliver usable multi-activity block editing.

**Acceptance criteria:**

- Find/add activities without losing session-editor context. Support the arrival and roughly ten-item warmup examples.
- Replacing an activity preserves placement duration and local notes. Replace source-derived activity content explicitly and warn that retained notes may need review.
- Move a placement between blocks while retaining its own properties. Block-specific data remains with its block.
- Removal from the plan never removes the source library activity. Preserve unsaved entered data and use revision-checked atomic actions.

**Verification:** Editor walkthrough and repository checks for selection, replacement, remove and cross-block move with retained notes/duration and no library mutation.

## CURU-024 — Reorder blocks and placements safely

- Kind: task
- Status: planned
- Priority: high
- Parent: CURU-F006
- Depends on: CURU-023

**Objective:** Make session structure easy to change with obvious controls.

**Acceptance criteria:**

- Provide move-up/down controls for blocks and placements, with deterministic order and sensible boundary behavior.
- Reorder the correct session aggregate transactionally, checking its expected revision. Concurrent move/edit cannot silently overwrite the plan.
- Keep editor focus/context and unrelated unsaved fields intact; avoid duplicate submissions producing accidental repeated moves.
- No drag-and-drop, parallel-group scheduler or recursive nesting.

**Verification:** Ordering, boundary, concurrent action and rollback checks plus a phone-width interaction walkthrough.

## CURU-008 — Add optional timing with non-blocking feedback

- Kind: task
- Status: planned
- Priority: high
- Parent: CURU-F006
- Depends on: CURU-023

**Objective:** Support useful time estimates without turning planning into time accounting.

**Acceptance criteria:**

- Persist seconds; accept optional short placement durations and optional block/session allocations. Keep library recommendations separate.
- Use a block allocation when supplied, otherwise derive a partial/complete total from its placements. Count each block once and expose uncertainty.
- Missing allocations or inconsistent totals produce accessible inline warnings and a save summary, not blocked saves or repeated confirmation requirements.
- Reject malformed data normally, while allowing valid incomplete/inconsistent coaching plans. Omit all timing if the coach does not need it.

**Verification:** Timing examples cover untimed sessions, ten short activities, block budgets, overruns, partial totals and successful saves with warnings.

## CURU-025 — Recover conflicting session edits without overwriting newer work

- Kind: task
- Status: planned
- Priority: high
- Parent: CURU-F006
- Depends on: CURU-022, CURU-024, CURU-008

**Objective:** Extend revision protection and recovery drafts to complete plan editing.

**Acceptance criteria:**

- All plan-affecting mutations participate in the aggregate revision; a child operation also makes an older editor stale.
- On conflict retain the submitted plan, including ordering, snapshots, notes and timing, while leaving authoritative data untouched.
- Offer a separate owner-scoped recovery copy, verify it saved, then reload current truth on the user's action. A recovery failure leaves entered work available.
- No automatic merge, forced overwrite, routine polling requirement, or full plan revision-history feature. Reuse the bounded recovery pattern instead of building an editor framework.

**Verification:** Two-editor save/reorder/move cases prove atomic stale rejection, complete recovery, reload and failure behavior.

## CURU-018 — Read the complete session comfortably on a phone

- Kind: task
- Status: planned
- Priority: high
- Parent: CURU-F007
- Depends on: CURU-025

**Objective:** Deliver the primary on-field consumption view.

**Acceptance criteria:**

- Separate reading from editing; show compact block sequence, intent, durations where useful and expandable copied instructions.
- Preserve reading position/context across expansion and navigation. Show the growing plan during editing and permit direct access to any part.
- Exercise arrival, ten short warmup activities, three to five main blocks, a game, reflection/cheer and cooldown. Theme/stage wizards are optional future interactions.
- Provide touch-friendly, legible presentation. PDF/print polish, live timers and offline sync are not required.
- Supply a concise real-phone/on-field checklist for Chris. Automated viewport checks are not evidence that an actual field trial occurred.

**Verification:** Phone-width browser checks and a full reference-session walkthrough; record actual devices tested and leave operator field acceptance explicit.

## CURU-009 — Duplicate sessions as independent undated plans

- Kind: task
- Status: planned
- Priority: high
- Parent: CURU-F008
- Depends on: CURU-018

**Objective:** Reuse an existing session without mutating it.

**Acceptance criteria:**

- Copy blocks, order, placements, snapshots, local notes and allocations into independent records.
- Clear the copied session date; date remains optional and no date prompt is required.
- Define a clear duplicate title and preserve useful editable planning content. Archived sources remain accessible only through the owner's deliberate action.
- Changing or archiving either session cannot mutate the other; no fresh library content is substituted silently.

**Verification:** Copy isolation, cleared optional date, snapshot fidelity, archived-source access and rollback checks.

## CURU-026 — Define the reusable versus occurrence-specific template boundary

- Kind: task
- Status: planned
- Priority: high
- Parent: CURU-F008
- Depends on: CURU-009

**Objective:** Produce the concrete session-to-template conversion policy for Chris's review before implementing templates.

**Acceptance criteria:**

- Classify actual implemented session/block/placement fields using a reusable-definition versus particular-occurrence distinction. Date is excluded; group/context and note treatment must be made explicit.
- Use representative examples to propose which fields copy, clear, or become editable defaults. Do not invent a generic compile/runtime framework.
- Present the policy as a reviewable design candidate. Operator acceptance of that specific policy records the product decision; unanswered material choices remain explicit and template implementation stays planned.
- No template implementation, runtime user prompting system, or silent guessing of the unresolved context boundary.

**Verification:** Field-mapping examples for session-to-template and template-to-session, including date, context, notes, snapshots and intent; no runtime behavior is claimed.

## CURU-010 — Create and edit reusable session templates

- Kind: task
- Status: planned
- Priority: high
- Parent: CURU-F008
- Depends on: CURU-026

**Objective:** Implement template authoring using the accepted conversion policy.

**Acceptance criteria:**

- Save a session as a template and edit its named blocks, placements and reusable content under the accepted field map.
- Templates own their content, archive/restore independently, and never share mutable placement records with their source sessions.
- Reuse explicit saves, atomic revision checks and owner-scoped recovery on conflict; do not regress library/session behavior.
- No inheritance, template version-history browser or organizational sharing.

**Verification:** Field-policy, save/edit/archive/restore, conflict recovery and source/template isolation cases.

## CURU-027 — Instantiate independent sessions from templates

- Kind: task
- Status: planned
- Priority: high
- Parent: CURU-F008
- Depends on: CURU-010

**Objective:** Create a usable session from reusable structure without hidden source coupling.

**Acceptance criteria:**

- Copy the accepted reusable fields into an independent session; the date is absent unless deliberately supplied.
- Use the template's copied activity content, not an implicit refresh from today's library.
- Changes to the session, template or original source cannot mutate another. Preserve the defined ordering and allocations.
- Archived templates require a deliberate owner action instead of appearing in ordinary selection.

**Verification:** Instantiation and copy-isolation checks across source/template/new session, cleared occurrence fields and failure rollback.

## CURU-011 — Refine activity discovery using actual planning friction

- Kind: task
- Status: planned
- Priority: high
- Parent: CURU-F004
- Depends on: CURU-018

**Objective:** Deliver useful predictable filters without building taxonomy administration.

**Acceptance criteria:**

- Add the phase/theme/tag/duration filters justified by the reference workflow, with documented matching and combination semantics.
- Unclassified activities remain discoverable. Tags and rugby subject matter do not become session stages.
- Preserve search/editor context and provide meaningful zero-result behavior.
- Record operator feedback when available; otherwise test concrete reference scenarios and identify unvalidated assumptions.

**Verification:** Focused query and UI scenarios covering filter combinations, duration ranges, unclassified items and empty results.

## CURU-012 — Prepare MVP acceptance evidence and the operator field walkthrough

- Kind: task
- Status: planned
- Priority: high
- Parent: none
- Depends on: CURU-027, CURU-011

**Objective:** Assemble the practical release evidence without fabricating operator acceptance or bundling unknown fixes.

**Acceptance criteria:**

- Run the documented integrated checks for protected CRUD, bootstrap repeatability, snapshot/copy isolation, conflict recovery, migrations/restore compatibility and phone reading.
- Provide a concise walkthrough for several actual sessions and record which results are automated, actually observed by an operator, or still pending.
- Gather any real feedback already supplied. Material fixes become bounded follow-up Tasks with new IDs; do not silently expand this Task into open-ended product work.
- Do not mark a milestone or the MVP accepted solely because this evidence Task was delivered. Actual coaching acceptance remains Chris's decision.

**Verification:** Normal application checks plus source-bound evidence/report and an honest operator checklist. No live deployment, real-data reset or manufactured adverse event is required.

## Next task boundary

CURU-001 delivered the first product and architecture baseline documents without claiming application behavior. CURU-014 delivered the reviewed gateway/JWT, authorization, browser-security, and data-service contract without claiming application behavior. CURU-002 delivered the minimal runnable application shell. CURU-019 delivered the explicit PostgreSQL migration and restricted runtime boundary. CURU-003 delivered the bounded 44-activity/110-progression seed asset. CURU-004 delivered atomic explicit seeded or intentionally empty dataset initialization without startup seeding or synchronization over edited content. CURU-015 is the only ready Task and is the next bounded implementation task.

CURU-001 records accepted product boundaries; CURU-014's accepted design contract is recorded in `doc/integration-contract.md`. Template context conversion is deliberately resolved later by CURU-026 before CURU-010. No unfinished template decision blocks the library/session work.
