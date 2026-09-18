# CrossLink agent guidelines

Applies throughout this repository.

## Scope and code quality

- Keep PRs focused on one behavior or fix, with its tests and documentation. Avoid unrelated refactors, formatting, dependency upgrades, and generated-file churn; preserve existing user changes.
- Write idiomatic, `gofmt`-formatted Go with clear package boundaries and small, composable functions. Prefer clarity and maintainability over cleverness. Reuse existing helpers and domain logic; extract shared code when it removes real duplication, not for speculative reuse.
- Introduce interfaces only for useful abstraction or testability. Avoid unnecessary layers, indirection, and generic utility packages.
- Return errors (`value, err`) rather than panicking outside `main`/startup; check query, mutation, and commit errors. Wrap errors with useful context while preserving their cause.
- Give exported functions, structs, and interfaces GoDoc comments. Explain non-obvious event, protocol, SRU, transaction, and contextual logging behavior; omit comments that restate code.
- Prefer the standard library and existing dependencies. Keep new dependencies lightweight (e.g. the existing `sqlc` approach); avoid heavy ORMs such as GORM. Explain additions, maintain affected `go.mod`/`go.sum` files and local replacements, and address security updates in focused changes.

## Architecture and source ownership

- This is a multi-module Go repository, not a root Go module. `broker` orchestrates ILL; `directory` owns peer metadata (ISIL, endpoints, networks/tiers); `illmock` simulates external services. Protocol/shared modules include `iso18626`, `sru`, `ncip`, `marcxml`, `zoom`, `httpclient`, and `testutil`.
- Follow nearby package boundaries: HTTP/protocol handlers validate and translate; services implement workflows; repositories own persistence; `app`/`cmd` wire dependencies. Keep reusable protocol modules independent of broker business rules.
- Preserve ISO18626 requester/supplier exchanges and existing opaque/transparent modes. Use SRU for union-catalog holdings lookups and existing NCIP/Z39.50 integrations where applicable. Keep vendor differences in existing shims, profiles, and adapters.
- Resolve peer metadata through the Directory HTTP API and existing clients/adapters, not cross-service database access. Use `encoding/xml` marshalling/unmarshalling for XML, not hand-built XML or manual streaming parsers.
- Edit source definitions, then regenerate with the relevant module's Make targets; never hand-edit generated code. API sources: `broker/oapi/open-api.yaml` and `directory/api.yaml`; SQL sources: `broker/sqlc/` and `directory/query.sql` plus migrations; protocol sources: the respective module's schemas/generators. Follow tracked/ignored-file conventions for generated outputs.
- Patron-request state models originate in `misc/state-models.yaml` (schema: `misc/state-model.json`). Regenerate embedded JSON with `make -C broker patron_request/service/statemodels/state-models.json`. Keep allowed actions, transitions, conditions, and service types consistent with service behavior; avoid duplicating state rules.

## Persistence, events, and scalability

- Use Postgres `LISTEN/NOTIFY` and the existing event/task infrastructure for internal messaging and job queues; do not introduce Kafka, RabbitMQ, or another messaging framework.
- Use the provided `WithTxFunc` helper for operations requiring consistency, implemented by `repo.PgBaseRepo` in `broker/repo/baserepo.go`. Use the callback's transaction-backed repository throughout. Avoid manual transaction management unless necessary.
- Parameterize SQL; never concatenate external values. Keep database access simple, reuse connection pools, and close rows/resources. Keep transactions short and avoid slow network calls while holding database locks.
- Persist event/task state and notifications consistently. Respect consumer versus observer roles and existing database claims/locks. Design for duplicate delivery, retries, restarts, and multiple replicas; do not rely on process-local locks or assume exactly-once external effects.
- Add versioned up/down migrations rather than rewriting deployed migrations; keep broker SQLC schema snapshots aligned. Consider existing data, indexes, lock duration, and compatibility during rolling deployments.
- Bound message sizes, batch sizes, concurrency, and memory use; paginate large results and avoid unnecessary per-record queries. Background work must honor cancellation and release resources on shutdown.

## Context, logging, and security

- Propagate request context, cancellation, deadlines, and logging fields through handlers and background tasks. Use `common.ExtendedContext` in `broker/common/extctx.go` for broker code and the module's existing context conventions elsewhere.
- Use structured `slog` via the contextual logger (`ctx.Logger().With(...)`). Logs serve developers/devops: keep them minimal, reserve INFO for unusual/noteworthy situations, and use DEBUG for troubleshooting. No routine-operation chatter, `fmt.Println`, plain `log.Printf`, or ad-hoc console output.
- Record all application-level ILL events in the transaction event log; do not duplicate them to stdout. Never log credentials or PII.
- Validate and sanitize external inputs, including protocol responses and Directory data. Preserve tenant scoping and authorization on reads, writes, events, and background work; never trust a caller-supplied identifier as authorization.
- Use secure network defaults (HTTPS), bounded timeouts, and retries with backoff through existing clients. Retry only appropriate transient failures and account for non-idempotent operations; avoid multiplying retries across layers.

## Deployment and verification

- Services ship as Docker images and Helm charts. Use existing environment/configuration conventions; keep affected README configuration tables, Compose examples, Helm values/templates, and FOLIO descriptors aligned. Keep secrets out of source and preserve health checks, graceful shutdown, and replica safety.
- Use `_test.go` tests in the same package for unit behavior and existing module `test/` suites for integration coverage. Prefer standard `testing`, table-driven cases where useful, and existing fixtures/helpers; add external test frameworks only when absolutely necessary.
- Test observable behavior, failures, and edge cases. Add regression coverage for fixes; exercise Postgres transactions/LISTEN-NOTIFY and Directory/catalog interactions with integration tests. Cover tenant isolation and duplicate/concurrent processing when affected.
- Maintain coverage >=80%, aiming for ~100%; honor stricter module coverage settings. Do not weaken assertions, coverage thresholds, or lint rules to make checks pass.
- Run focused tests first, then `make -C <module> check` for affected modules and consumers of changed shared modules. Use root `make check` for repository-wide changes. Generate required sources before direct `go test` runs; execute Go commands from the relevant module.
- Run affected build/lint checks using module Makefiles and `.github/workflows/`; use race tests for concurrency changes, API lint for contract changes, and the Bruno scenarios in `bruno/README.md` for affected end-to-end workflows. Run vulnerability checks for dependency changes. Docker is required for integration tests; generators/native builds may need `xsltproc`, `pkg-config`, and libyaz (see module READMEs).
- Review the final diff for unintended changes and whitespace errors. Report what changed, checks actually run, and any failures or unverified areas; never claim checks passed without running them. Documentation-only changes need no Go test run.
