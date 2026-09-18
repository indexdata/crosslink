# CrossLink agent guidelines

Applies throughout this repository.

## Working approach

- Treat questions, investigations, and requests for advice as discussion, not permission to edit files.
- Before non-trivial code changes, inspect the relevant code and outline the proposed approach, affected areas, and validation. Ask follow-up questions when requirements, scope, or tradeoffs are unclear.
- Wait for agreement on the approach before editing. An explicit request to implement an already-discussed plan counts as agreement.
- For small, clearly specified edits, proceed directly. Keep planning proportional to the task.
- Treat follow-up requests as revisions to the whole change. When behavior, terminology, or design changes, update all affected code, tests, names, comments, and documentation—including earlier work in the session. Remove superseded code and expectations; keep cleanup within the task's scope.

## Scope and code quality

- Keep PRs to one behavior/fix plus tests/docs. Avoid unrelated refactors, formatting, dependency upgrades, and generated-file churn. Preserve user changes.
- Format Go with `gofmt`. Reuse existing helpers; extract real duplication, not speculative abstractions. Add interfaces only for useful abstraction or testability.
- Prefer returned errors over panics or panic-based helpers in new code outside startup; leave unrelated existing panic-based code alone. Check query, mutation, and commit errors; wrap with context while preserving causes.
- Document exported APIs and non-obvious invariants.
- Prefer standard-library and existing dependencies; avoid heavy ORMs such as GORM. Justify lightweight additions, maintain affected `go.mod`/`go.sum` and local replacements, and keep security updates focused.

## Architecture and source ownership

- Multi-module Go repository; no root Go module. `broker` orchestrates ILL; `directory` owns peer metadata (ISIL, endpoints, networks/tiers); `illmock` simulates external services. Keep shared protocol/client modules independent of broker business rules.
- Follow existing module boundaries: HTTP/protocol handlers validate and translate; services implement workflows; repositories own persistence; `app`/`cmd` wire dependencies. Do not introduce new layers or relocate unrelated code solely to enforce this guidance.
- Preserve ISO18626 requester/supplier exchanges and existing opaque/transparent modes. Use existing SRU holdings lookups and NCIP/Z39.50 integrations. Keep vendor differences in existing shims, profiles, and adapters.
- Resolve peers through existing Directory HTTP clients/adapters, never cross-service DB access. Use existing protocol types and `encoding/xml`; avoid constructing XML with strings.
- Edit source definitions and regenerate with module Make targets; never hand-edit generated code. API sources: `broker/oapi/open-api.yaml` and `directory/api.yaml`; SQL sources: `broker/sqlc/` and `directory/query.sql` plus migrations; protocol sources: the respective module's schemas/generators. Respect generated-file tracking/ignore conventions.
- Patron-request state models originate in `misc/state-models.yaml` (schema: `misc/state-model.json`). Regenerate embedded JSON with `make -C broker patron_request/service/statemodels/state-models.json`. Keep actions, transitions, conditions, and service types consistent with services; avoid duplicate state rules.

## Persistence, events, and scalability

- Use existing Postgres `LISTEN/NOTIFY` events/tasks for messaging and job queues; no Kafka, RabbitMQ, or other messaging frameworks.
- For multiple broker operations that must commit atomically, use the repository's `WithTxFunc` wrapper backed by `repo.PgBaseRepo` (`broker/repo/baserepo.go`); use the callback's transaction-backed repository throughout. Elsewhere follow module transaction conventions; avoid manual transaction management unless necessary.
- Use existing SQLC repositories and connection pools. Parameterize SQL; never concatenate external values. Keep transactions short and avoid network calls under DB locks.
- Persist event/task state and notifications consistently. Respect consumer versus observer roles and existing database claims/locks. Design for duplicate delivery, retries, restarts, and multiple replicas; do not use process-local locks for cross-replica coordination or assume exactly-once external effects.
- Add versioned up/down migrations rather than rewriting deployed migrations; keep broker SQLC schema snapshots aligned. Consider existing data, indexes, lock duration, and compatibility during rolling deployments.
- Bound message sizes, batch sizes, concurrency, and memory use; paginate large results and avoid unnecessary per-record queries.

## Context, logging, and security

- Propagate context, deadlines, and logging fields through handlers and background tasks; honor cancellation. Use broker's `common.ExtendedContext` (`broker/common/extctx.go`); follow module context conventions elsewhere.
- Use contextual structured `slog` (`ctx.Logger().With(...)`) for developer/devops diagnostics. Reserve INFO for noteworthy situations and DEBUG for troubleshooting; avoid routine chatter, `fmt.Println`, plain `log.Printf`, and ad-hoc console output.
- Record all application-level ILL events in the transaction event log; do not duplicate them to stdout. Never log credentials or PII.
- Validate external data at trust boundaries using existing validators and protocol types, including protocol responses and Directory data. Preserve tenant scoping through existing authorization/resolver paths on reads, writes, events, and background work; a caller-supplied tenant, symbol, or resource ID is not proof of access.
- Use existing network clients, secure defaults (HTTPS), and bounded timeouts. Add bounded retries/backoff only for appropriate transient failures when repeating the operation is safe; avoid multiplying retries across layers.

## Deployment and verification

- Use existing Docker/Helm and environment configuration conventions. Update affected README tables, Compose examples, charts, and FOLIO descriptors together. Keep secrets out of source; preserve health checks and graceful shutdown with resource cleanup.
- Follow nearby test organization and reuse existing fixtures/helpers, including `testify`. Prefer standard `testing`; introduce another testing framework only when absolutely necessary.
- Add regression tests for changed behavior. Integration-test affected Postgres transactions/LISTEN-NOTIFY, Directory/catalog interactions, tenant isolation, and duplicate/concurrent processing.
- Follow module coverage configuration; cover changed behavior without expanding PRs into unrelated coverage work. Report pre-existing shortfalls; never weaken assertions, coverage thresholds, or lint rules to pass checks.
- Run focused tests, then `make -C <module> check` for affected modules and shared-module consumers; root `make check` for repository-wide changes. Generate sources before direct `go test`; run Go commands inside modules.
- Run affected build/lint targets (see `.github/workflows/`), race tests for concurrency, API lint for contracts, Bruno scenarios (`bruno/README.md`) for workflow changes, and vulnerability checks for dependency changes. Prerequisites: Docker for integration tests; `xsltproc`, `pkg-config`, and libyaz where required by module READMEs.
- Validate changed Helm charts with lint/render checks and changed Compose files with `docker compose config`.
- Review the diff for unintended changes/whitespace errors. Report changes, checks actually run, failures, and unverified areas. Documentation-only changes need no Go tests.

## Maintaining these guidelines

- Capture recurring agent mistakes and review feedback as short, specific rules. Update stale paths/commands, merge duplicates, and remove guidance that no longer helps.
