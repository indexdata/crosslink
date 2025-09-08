# GitHub Copilot Instructions for Crosslink

These instructions guide GitHub Copilot when generating code for this repository.
Crosslink is an Interlibrary Loan (ILL) brokering service written in Go. It implements the **ISO18626** protocol and integrates with external union catalogs via **SRU**, using **Postgres LISTEN/NOTIFY** internally as an event bus for both message passing and job queuing.
Requester/supplier metadata (ISIL codes, endpoints, network/tier membership) is managed by the **Directory service**.

---

## General Guidelines
- Write **idiomatic Go** code that follows standard Go conventions for style and organization.
- Prefer **clarity and maintainability** over clever or overly compact code.
- Keep **external dependencies to a minimum**:
  - Prefer the Go **standard library**.
  - If needed, use **lightweight libraries** such as `sqlc`.
  - Avoid heavy ORMs or thick abstraction layers (e.g. GORM).
- Ensure **robust error handling** with clear return values (`value, err`) following Go idioms.
- Keep **code coverage ≥ 80%**, with a goal of approaching **100%**.

---

## Project-Specific Architecture
- **Event bus**: implemented with Postgres `LISTEN/NOTIFY`.
  - Used for **internal message passing** and **job queuing**.
  - Copilot should generate event-handling code that respects this pattern (don’t introduce external messaging frameworks like Kafka or RabbitMQ).
- **Protocol support**:
  - Implements **ISO18626** request/response handling.
  - Facilitate message passing between requester and supplier.
- **Union catalog integration**:
  - Uses **SRU (Search/Retrieve via URL)** for holdings lookups.
  - Rely on Go’s XML **marshalling/unmarshalling** (`encoding/xml`) for request/response handling.
- **Directory service**:
  - Queried for requester/supplier peer info (ISIL code, endpoint, metadata).
  - Copilot should generate client code that handles lookups via simple HTTP requests.

---

## Database Access & Consistency
- Use the provided `baserepo.WithTxFunc` helper when performing DB operations that require consistency.
  - This ensures that code is executed within a transaction and commits/rolls back correctly.
  - Avoid writing transaction management code manually unless necessary.
- Always check errors from queries and mutations.
- Use parameterized queries (no string concatenation).
- Keep DB interactions simple and predictable; don’t introduce unnecessary abstractions.

---

## Logging & Context
- Use the provided **`extctx` package** for context handling and logging.
  - It extends `context.Context` with structured logging (`slog`) that supports contextual fields.
  - Always propagate the `extctx.Context` through request handling and background tasks.

### Logging Principles
- Logs written via `slog` are primarily for **developers and devops troubleshooting**, not for end users or library staff.
- Keep logs **clean and minimal**:
  - **Do not log every routine operation**.
  - **INFO-level logs** should only appear for unusual or noteworthy situations (not normal flow).
  - Prefer **DEBUG** for deep troubleshooting detail, if used at all.
- Each ILL transaction maintains its own **event log**:
  - All **application-level events** must be logged through the transaction event log.
  - Do **not** duplicate this information to stdout logs.
- Use structured logging with contextual fields (`ctx.Logger().With(...)`).
- Avoid `fmt.Println`, plain `log.Printf`, or ad-hoc console output.

---

## Code Style & Conventions
- Use **Go module layout** with clear package boundaries.
- Functions should return `(value, error)` rather than panicking, unless in `main()` or startup.
- Use **context.Context** (preferably from `extctx`) for cancelation, deadlines, and request scoping.
- Prefer **interfaces** only where they add testability or abstraction value.
- Keep code simple, direct, and idiomatic:
  - Favor small, composable functions.
  - Avoid unnecessary abstraction or indirection.
- Write **table-driven tests** where appropriate.

---

## Documentation & Comments
- Exported functions, structs, and interfaces must have **GoDoc-compatible comments**.
- Comment non-obvious implementation details, especially in:
  - Event bus handling
  - ISO18626 protocol processing
  - SRU query handling
  - Database transactions with `WithTxFunc`
  - Logging with `extctx` and slog
- Avoid redundant comments that repeat what the code already states.

---

## Testing
- Maintain **≥80% code coverage** (goal: ~100%).
- Place tests in the same package with `_test.go` suffix.
- Use the **standard `testing` package** (no external test frameworks unless absolutely necessary).
- Test strategies:
  - **Unit tests** for functions and components.
  - **Integration tests** for event bus (LISTEN/NOTIFY) and Directory/union catalog interactions.
  - Include **error cases and edge conditions**.

---

## Performance & Reliability
- Use **XML marshalling/unmarshalling** rather than manual streaming parsing.
- Avoid holding large datasets in memory unless necessary.
- Use **connection pooling** for Postgres clients.
- Ensure retries and backoff are implemented for network calls.

---
## Security

- Sanitize and validate all external inputs (e.g., SRU responses, Directory data).
- Use secure defaults for network calls (e.g., HTTPS).
- Avoid logging sensitive information (e.g., credentials, PII).
- Bound size of incoming messages to prevent DoS attacks.
- Regularly update dependencies to patch security vulnerabilities.
