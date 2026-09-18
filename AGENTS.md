# CrossLink agent guidelines

Applies throughout this repository.

## Scope and code quality

- Keep PRs to one behavior/fix plus tests/docs. Avoid unrelated refactors, formatting, dependency upgrades, and generated-file churn. Preserve user changes.
- Write clear, idiomatic, `gofmt`-formatted Go with small functions and clear package boundaries. Reuse existing helpers; extract real duplication, not speculative abstractions. Add interfaces only for useful abstraction or testability.
- Return errors (`value, err`), not panics outside `main`/startup. Check query, mutation, and commit errors; wrap with context while preserving causes.
- Document exported functions, structs, and interfaces with GoDoc. Explain non-obvious event, protocol/SRU, transaction, and logging behavior; omit redundant comments.
- Prefer standard-library and existing dependencies, including `sqlc`; avoid heavy ORMs such as GORM. Justify lightweight additions, maintain affected `go.mod`/`go.sum` and local replacements, and keep security updates focused.

## Architecture and source ownership

- Multi-module Go repository; no root Go module. `broker` orchestrates ILL; `directory` owns peer metadata (ISIL, endpoints, networks/tiers); `illmock` simulates external services. Keep shared protocol/client modules independent of broker business rules.
- Follow existing module boundaries: HTTP/protocol handlers validate and translate; services implement workflows; repositories own persistence; `app`/`cmd` wire dependencies. Do not introduce new layers or relocate unrelated code solely to enforce this guidance.
- Preserve ISO18626 requester/supplier exchanges and existing opaque/transparent modes. Use existing SRU holdings lookups and NCIP/Z39.50 integrations. Keep vendor differences in existing shims, profiles, and adapters.
- Resolve peers through existing Directory HTTP clients/adapters, never cross-service DB access. Use `encoding/xml` marshalling/unmarshalling for XML, not hand-built XML or manual streaming parsers.
- Edit source definitions and regenerate with module Make targets; never hand-edit generated code. API sources: `broker/oapi/open-api.yaml` and `directory/api.yaml`; SQL sources: `broker/sqlc/` and `directory/query.sql` plus migrations; protocol sources: the respective module's schemas/generators. Respect generated-file tracking/ignore conventions.
- Patron-request state models originate in `misc/state-models.yaml` (schema: `misc/state-model.json`). Regenerate embedded JSON with `make -C broker patron_request/service/statemodels/state-models.json`. Keep actions, transitions, conditions, and service types consistent with services; avoid duplicate state rules.

## Persistence, events, and scalability

- Use existing Postgres `LISTEN/NOTIFY` events/tasks for messaging and job queues; no Kafka, RabbitMQ, or other messaging frameworks.
- For multiple broker operations that must commit atomically, use the repository's `WithTxFunc` wrapper backed by `repo.PgBaseRepo` (`broker/repo/baserepo.go`); use the callback's transaction-backed repository throughout. Elsewhere follow module transaction conventions; avoid manual transaction management unless necessary.
- Parameterize SQL; never concatenate external values. Keep DB access simple, reuse pools, close resources, and keep transactions short. Avoid network calls under DB locks.
- Persist event/task state and notifications consistently. Respect consumer versus observer roles and existing database claims/locks. Design for duplicate delivery, retries, restarts, and multiple replicas; do not use process-local locks for cross-replica coordination or assume exactly-once external effects.
- Add versioned up/down migrations rather than rewriting deployed migrations; keep broker SQLC schema snapshots aligned. Consider existing data, indexes, lock duration, and compatibility during rolling deployments.
- Bound message sizes, batch sizes, concurrency, and memory use; paginate large results and avoid unnecessary per-record queries. Background work must honor cancellation and release resources on shutdown.

## Context, logging, and security

- Propagate context, cancellation, deadlines, and logging fields through handlers and background tasks. Use broker's `common.ExtendedContext` (`broker/common/extctx.go`); follow module context conventions elsewhere.
- Use contextual structured `slog` (`ctx.Logger().With(...)`) for developer/devops diagnostics. Reserve INFO for noteworthy situations and DEBUG for troubleshooting; avoid routine chatter, `fmt.Println`, plain `log.Printf`, and ad-hoc console output.
- Record all application-level ILL events in the transaction event log; do not duplicate them to stdout. Never log credentials or PII.
- Validate external data at trust boundaries using existing validators and protocol types, including protocol responses and Directory data. Preserve tenant scoping through existing authorization/resolver paths on reads, writes, events, and background work; a caller-supplied tenant, symbol, or resource ID is not proof of access.
- Use secure network defaults (HTTPS), bounded timeouts, and retries with backoff through existing clients. Retry only appropriate transient failures and account for non-idempotent operations; avoid multiplying retries across layers.

## Deployment and verification

- Use existing Docker/Helm and environment configuration conventions. Update affected README tables, Compose examples, charts, and FOLIO descriptors together. Keep secrets out of source; preserve health checks, graceful shutdown, and replica safety.
- Use same-package `_test.go` unit tests and existing module `test/` integration suites. Prefer standard `testing`, table-driven cases, and existing fixtures/helpers, including `testify`. Introduce another testing framework only when absolutely necessary.
- Test behavior, errors, edge cases, and regressions. Integration-test affected Postgres transactions/LISTEN-NOTIFY, Directory/catalog interactions, tenant isolation, and duplicate/concurrent processing.
- Coverage target: >=80%, aiming for ~100%; honor stricter module settings. Cover changed behavior without expanding PRs into unrelated coverage work; report pre-existing shortfalls. Never weaken assertions, coverage thresholds, or lint rules to pass checks.
- Run focused tests, then `make -C <module> check` for affected modules and shared-module consumers; root `make check` for repository-wide changes. Generate sources before direct `go test`; run Go commands inside modules.
- Run affected build/lint targets (see `.github/workflows/`), race tests for concurrency, API lint for contracts, Bruno scenarios (`bruno/README.md`) for workflow changes, and vulnerability checks for dependency changes. Prerequisites: Docker for integration tests; `xsltproc`, `pkg-config`, and libyaz where required by module READMEs.
- Validate changed Helm charts with lint/render checks and changed Compose files with `docker compose config`.
- Review the diff for unintended changes/whitespace errors. Report changes, checks actually run, failures, and unverified areas. Documentation-only changes need no Go tests.

## Maintaining these guidelines

- Capture recurring agent mistakes and review feedback as short, specific rules. Update stale paths/commands, merge duplicates, and remove guidance that no longer helps.
