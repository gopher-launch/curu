---
version: 1
project: curu-deploy
updated: 2026-11-07
---

# Curu deployment roadmap

## Scope and source

This roadmap records the deployment design accepted by Chris on November 1, 2026. It is separate from the root [application roadmap](../roadmap.md). `CDP-` IDs identify deployment Tasks; existing `CURU-` IDs retain their application meanings. The initial source inspection used application commit `cc5b6941404129b69776ff785084f50ba449656b`; inspect the selected application revision and contracts again when implementing a Task.

The deployment baseline is documented in [README.md](README.md). These documents establish direction and planning, not a delivered chart, running cluster, completed browser trial, or isolation result. Root [AGENTS.md](../AGENTS.md) governs both workstreams.

## Accepted baseline

- First host: HP Ubuntu laptop; runtime: single-node kind. No cloud account, hosted service, subscription, or public hosting is required.
- One owned Helm chart deploys Curu, plain NGINX, OAuth2 Proxy, Keycloak, Postgres, and operator Jobs using directly maintained templates. No vendor/third-party subcharts or transitive chart dependencies. Reviewed, pinned upstream images are allowed.
- Bundled Postgres and Keycloak are optional. External connections, issuer configuration, trust material, and existing Secrets are explicit values. The application remains provider-neutral.
- One Postgres instance may host separate Curu and Keycloak databases and credentials. Application runtime, migration, initialization, database administration, and identity administration remain separate authorities.
- HTTPS and trusted issuer/JWKS certificates, exact owner authorization, RFC 9068 access tokens, CSRF/browser behavior, private backend exposure, and secret handling remain required. Normal startup does not migrate, initialize, reset, or reseed.
- NetworkPolicy enforcement/verification may be deferred when unavailable in kind. A replacement CNI is not a prerequisite. Record the local trusted-cluster/private-hop boundary and unverified isolation; keep actual public/host exposure restricted to the intended edge. This local qualification does not establish production isolation or relax application security.
- PVC storage has an explicit laptop persistence path and lifecycle. Scheduled backups, cloud storage, high availability, hosted clusters, replacement gateways/providers, general platform frameworks, and application feature work are outside the first milestone. Manual recovery and network isolation have separate follow-up Tasks.

## Selection and reconciliation

- Select one bounded `Kind: task` entry from this roadmap. Do not infer deployment task selection from the root roadmap or assume a runner selects this file automatically.
- CDP-001 delivered the owned chart and configuration boundary, CDP-002 delivered the reproducible local kind/storage/image/name/trust inputs, CDP-003 delivered persistent PostgreSQL plus separated database authority, and CDP-004 delivered explicit migration/initialization sequencing. CDP-005 is the next ready deployment Task; later core Tasks remain planned. After reviewing a delivered prerequisite, promote the next core Task only when its concrete inputs and acceptance are satisfied. Optional/follow-up Tasks require deliberate selection; they never become a gate on the core milestone merely because their prerequisites are delivered.
- `Depends on` references are confined to this roadmap. Existing application capabilities are source/contract prerequisites, not duplicated Tasks or unresolved cross-roadmap dependency IDs. Application changes need explicitly bounded scope in their owning workstream.
- Preserve IDs. Reconcile affected templates, README interfaces, guidance, and roadmap readiness in the same candidate. Split oversized Tasks before widening execution scope. No task is delivered because it appears here or because manifests render.
- Use disposable resources for destructive tests. Routine validation needs no paid provider or real retained-data reset. Keep operational progress, review verdicts, and pending integration notes in external records.

## Application prerequisites

The selected application image supplies the HTTP server plus `/curu-migrate`, `/curu-initialize`, and `/curu-recovery-check`. Its configuration and data contracts are in [configuration](../doc/configuration.md), [gateway](../doc/gateway.md), [integration](../doc/integration-contract.md), and [database operations](../doc/database-operations.md). Currently delivered application scope includes the protected read-only library, schema version 2, and the 44-activity/110-progression seed. Later application authoring/session work does not block this deployment.

## Delivery milestone

| Milestone | Tasks | Acceptance |
| --- | --- | --- |
| D1: authenticated local library | CDP-001 through CDP-008 | Reproduce the laptop installation, explicitly migrate/seed, sign in as owner, browse/search/details, deny another identity, exercise expiry, and preserve content across restart/upgrade |
| Optional manual recovery | CDP-009 | Export and verify an isolated application restore |
| Deferred network isolation | CDP-010 | Exercise actual enforcement and document its boundary |

D1 includes actual target-browser acceptance, distinct from automated disposable-cluster verification. Phone acceptance is recorded only if performed. Neither optional row is a prerequisite for D1, and D1 does not claim a hosted or fully isolated production deployment.

## CDP-005 — Bootstrap Keycloak and prove access-token compatibility

- Kind: task
- Status: ready
- Priority: high
- Parent: none
- Depends on: none

**Objective:** Supply repeatable local identity configuration that satisfies Curu's delivered token and owner contract.

**Acceptance criteria:**

- Add owned single-replica Keycloak templates using its separate Postgres database, explicit hostname/proxy/trust configuration, scoped Secrets, probes, and constrained administrative access. Use an explicitly configured server start mode with HTTPS at the edge.
- Deliver repeatable realm/client and controlled development-user setup without manual console configuration being the only supported path. Initial provisioning and later reconciliation preserve existing users, credentials, signing state, and client configuration outside the declared managed fields.
- Configure RS256, `at+jwt`, a dedicated Curu audience, required RFC 9068 claims including `client_id`/`jti`, and a lifetime at or below 15 minutes. Add client-specific claim mapping where necessary; do not relax Curu's validator or forward ID tokens.
- Establish the exact stable owner subject/issuer and internal owner UUID; keep passwords and administration credentials out of committed realm data and logs. Repeated upgrades do not reset identities or silently rebind dataset ownership.
- Verify local HTTPS discovery/JWKS trust and configure the browser client's PKCE/callback/session inputs for CDP-006. Support bundled-disabled issuer/JWKS/client configuration without deleting externally owned resources. Any temporary identity test exposure is explicit and disposable.

**Verification:** Programmatic provisioning/reconciliation, Secret and user/signing-state persistence, trusted discovery/JWKS, and actual token acceptance by the selected Curu build with rejection of wrong-purpose/audience/expired tokens. A controlled flow may support token fixtures, but cannot substitute for CDP-006's real authorization-code browser flow.

## CDP-006 — Integrate NGINX and OAuth2 Proxy browser authentication

- Kind: task
- Status: planned
- Priority: high
- Parent: none
- Depends on: CDP-005

**Objective:** Deliver the complete HTTPS browser-to-Curu path using the bundled identity provider.

**Acceptance criteria:**

- Add owned NGINX and OAuth2 Proxy templates. NGINX terminates HTTPS, routes identity and login/callback/logout requests, consults browser authentication, and is the immediate peer forwarding product requests to Curu. Keep backend Services and readiness off the host-facing route.
- Use OIDC Authorization Code flow with PKCE and secure host-only browser cookies with explicit session/refresh policy. Configure redirects and upstream TLS trust from the selected local names, without open redirects or verification skips.
- Strip spoofed browser Authorization, identity, and forwarding headers. Inject one Bearer access token intended for Curu and one trusted client address; never substitute an ID token or expose the access-token authentication response header to the browser.
- Propagate refreshed cookies correctly, including split-cookie cases if applicable. Preserve Curu's CSRF/security response headers and enforce safe expiry behavior when either the application or gateway rejects a request: keep HTMX DOM/content, signal authentication required, and require deliberate sign-in without redirect/replay of mutations.
- Prove owner login and another user's denial; document logout as gateway-session termination and explicitly describe provider-session behavior. Do not expose identity administration as an unrestricted application route.

**Verification:** Disposable HTTPS browser checks for actual login/callback, library list/search/detail access, unknown-user `403`, spoofed-header resistance, refresh beyond access-token lifetime, gateway/application expiry handling, logout, and inaccessible backend/readiness host routes. Use existing boundary fixtures for editor-preservation semantics without implementing application authoring as part of deployment.

## CDP-007 — Deliver repeatable operator lifecycle and integration checks

- Kind: task
- Status: planned
- Priority: high
- Parent: none
- Depends on: CDP-006

**Objective:** Assemble component interfaces into a small, repeatable operator workflow with useful diagnostics.

**Acceptance criteria:**

- Publish exact implemented commands for local setup/install, explicit data operations, status/logs, stop/start, upgrade, and removal. Scope every operation to an explicit intended cluster/context/release; destructive data removal names its target and remains separate from routine teardown.
- Bind image/source/version/configuration inputs to a deployment record without storing credentials. Establish bounded waits, failure/exit reporting, and actionable diagnostics; do not require per-pod shell rituals or live debuggers for normal troubleshooting.
- Exercise clean installation and an upgrade with retained content, owner identity, credentials, and signing state. Back out only when the prior application image remains compatible with the resulting database schema; never imply Helm rollback reverses migrations.
- Supply a repeatable deployment validation command combining owned chart checks and focused disposable kind/browser checks. Cover bundled modes and exercise disabled bundled services against compatible disposable external endpoints with explicitly supplied trust/credentials.
- Verify JWKS outage behavior with fresh versus expired cache, coarse readiness, and failure-safe exposure. Keep NetworkPolicy enforcement, optional recovery, cloud credentials, and phone testing out of the mandatory local workflow.

**Verification:** Run the documented lifecycle from clean disposable inputs, exercise a controlled failed operation and bounded diagnostics, validate retained-data upgrade/restart and external configurations, and record actual checks separately from assumptions. Go checks apply only if application/build changes are also made.

## CDP-008 — Record the HP laptop browser acceptance

- Kind: task
- Status: planned
- Priority: high
- Parent: none
- Depends on: CDP-007

**Objective:** Demonstrate D1 on the intended host without confusing automated evidence with operator acceptance.

**Acceptance criteria:**

- Prepare a concise walkthrough of exact commands, prerequisites, resource/storage inputs, selected versions, and observed deployment behavior. Keep detailed run evidence outside the product dataset and keep README instructions current.
- Chris signs in on the HP laptop through local HTTPS, sees the freshly seeded 44 activities, searches, and opens complete activity details/progressions. Record the actual observed result and any acceptance gaps; do not fabricate access to that host or device.
- Record demonstrated denial, expiry, restart/persistence, and documented upgrade outcomes, linking automated evidence where appropriate. A failure is an explicit gap rather than an assumed pass.
- State whether NetworkPolicy enforcement was exercised; an unverified result does not block D1. State whether manual recovery and any phone/LAN trial were performed; neither is a prerequisite.
- Reconcile milestone status only from evidence and Chris's actual acceptance. Preparing the checklist alone does not establish target-browser acceptance; missing operator evidence remains visibly pending.

**Verification:** Source-bound checklist plus genuine target-host/operator evidence. No public internet, cloud provisioning, subscription, scheduled backup, or manufactured phone trial is required.

## CDP-009 — Add optional manual export and isolated restore

- Kind: task
- Status: planned
- Priority: low
- Parent: none
- Depends on: CDP-007

**Objective:** Offer a small manual recovery path when local content becomes worth retaining, without creating a backup service.

**Acceptance criteria:**

- Publish explicit manual export and isolated-restore operations using PostgreSQL tooling and `/curu-recovery-check`. Quiesce writes between source manifest and dump or use an equivalent fixed snapshot; bind evidence to the exact backup/image/schema identity.
- Supply target-specific roles/credentials and run compatibility/grant reconciliation and manifest comparison before any promotion. Never initialize/reseed a restored dataset or overwrite a retained database as a routine test.
- Distinguish application-data export from Keycloak identity/signing state, gateway sessions, certificates, and deployment-secret recovery; an application manifest does not prove whole-stack recovery.
- Keep scheduling, retention automation, cloud destinations, and installation dependencies outside this Task. Failure to select or deliver this optional Task does not block D1.

**Verification:** Disposable edited-content export/restore and manifest comparison, mismatch rejection, and explicit retained-target protection. Do not imply a backup exists merely because the command is documented.

## CDP-010 — Verify network isolation on a capable cluster

- Kind: task
- Status: planned
- Priority: low
- Parent: none
- Depends on: none

**Objective:** Exercise actual policy enforcement separately from the accepted local-development milestone.

**Acceptance criteria:**

- Select and document a capable test environment and its policy implementation. If kind needs an additional CNI, make that an explicitly selected follow-up configuration, not a retroactive local bootstrap requirement.
- Define/verify the intended edge, Curu, identity, data, DNS, and operator traffic relationships. Check allowed and denied connections from representative pods; rendered policies alone do not establish enforcement.
- Document private-hop trust/protection and the scope of administrator/node access that policy does not isolate. Preserve existing application authentication, authorization, and database privilege controls.
- Report actual results, limitations, and the hosted-environment prerequisites still outstanding. Do not mark the local policy exception resolved until enforcement has been exercised.

**Verification:** Real permitted/denied connection tests and exposure/private-hop evidence on the selected environment. This Task is outside D1 and is not required to deliver or accept the initial kind installation.

## Next task boundary

CDP-005 is the only ready deployment Task. CDP-001 delivered chart/configuration/render interfaces, CDP-002 delivered the local cluster boundary, CDP-003 delivered the persistent data service and role boundary, and CDP-004 delivered explicit migration and initialization sequencing; these do not constitute an authenticated installation. CDP-006 through CDP-008 remain the core planned implementation sequence; CDP-009 and CDP-010 are optional/follow-up work and stay planned even though CDP-010's dependency is satisfied. Application readiness continues to be managed independently in the root roadmap.
