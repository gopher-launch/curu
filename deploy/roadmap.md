---
version: 1
project: curu-deploy
updated: 2026-11-09
---

# Curu deployment roadmap

## Scope and source

This roadmap records the deployment design accepted by Chris on November 1, 2026. It is separate from the root [application roadmap](../roadmap.md). `CDP-` IDs identify deployment Tasks; existing `CURU-` IDs retain their application meanings. The initial source inspection used application commit `cc5b6941404129b69776ff785084f50ba449656b`; inspect the selected application revision and contracts again when implementing a Task.

The deployment baseline and implemented operator interfaces are documented in [README.md](README.md). Milestone status below is evidence-gated; detailed target-host results and Chris's verdict are kept in the external operator record. Root [AGENTS.md](../AGENTS.md) governs both workstreams.

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
- CDP-001 through CDP-008 delivered the owned chart, reproducible kind/trust inputs, persistent separated data authority, explicit data sequencing, repeatable identity, the HTTPS browser-authentication path, the repeatable operator lifecycle/integration validation, and target-laptop browser acceptance. No core deployment Task remains ready in D1. Optional/follow-up Tasks require deliberate selection; they never become a gate on the core milestone merely because their prerequisites are delivered.
- `Depends on` references are confined to this roadmap. Existing application capabilities are source/contract prerequisites, not duplicated Tasks or unresolved cross-roadmap dependency IDs. Application changes need explicitly bounded scope in their owning workstream.
- Preserve IDs. Reconcile affected templates, README interfaces, guidance, and roadmap readiness in the same candidate. Split oversized Tasks before widening execution scope. No task is delivered because it appears here or because manifests render.
- Use disposable resources for destructive tests. Routine validation needs no paid provider or real retained-data reset. Keep operational progress, review verdicts, and pending integration notes in external records.

## Application prerequisites

The selected application image supplies the HTTP server plus `/curu-migrate`, `/curu-initialize`, and `/curu-recovery-check`. Its configuration and data contracts are in [configuration](../doc/configuration.md), [gateway](../doc/gateway.md), [integration](../doc/integration-contract.md), and [database operations](../doc/database-operations.md). Currently delivered application scope includes the protected read-only library, schema version 2, and the 44-activity/110-progression seed. Later application authoring/session work does not block this deployment.

## Delivery milestone

| Milestone | Tasks | Acceptance |
| --- | --- | --- |
| D1: authenticated local library — accepted | CDP-001 through CDP-008 | Reproduce the laptop installation, explicitly migrate/seed, sign in as owner, browse/search/details, deny another identity, exercise expiry, and preserve content across restart/upgrade |
| Optional manual recovery | CDP-009 | Export and verify an isolated application restore |
| Deferred network isolation | CDP-010 | Exercise actual enforcement and document its boundary |

D1 includes actual target-browser acceptance, distinct from automated disposable-cluster verification. Phone acceptance is recorded only if performed. Neither optional row is a prerequisite for D1, and D1 does not claim a hosted or fully isolated production deployment.

## CDP-009 — Add optional manual export and isolated restore

- Kind: task
- Status: planned
- Priority: low
- Parent: none
- Depends on: none

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

D1 has no remaining core deployment Task. CDP-001 delivered chart/configuration/render interfaces, CDP-002 delivered the local cluster boundary, CDP-003 delivered the persistent data service and role boundary, CDP-004 delivered explicit migration and initialization sequencing, CDP-005 delivered repeatable identity plus accepted/rejected real-token evidence, CDP-006 delivered the browser-authenticated HTTPS path, CDP-007 delivered the scoped operator lifecycle plus aggregate disposable validation, and CDP-008 delivered the target-laptop acceptance procedure and evidence-gated milestone reconciliation. CDP-009 and CDP-010 are optional/follow-up work and stay planned. Application readiness continues to be managed independently in the root roadmap.
