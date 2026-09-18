# Introduction

CrossLink broker is a system to manage inter-library loans (ILL), specifically it:

* accepts and handles ILL requests from external requesters (e.g Alma or ReShare) via the ISO18626 protocol
* locates suppliers and their holdings from a Union Catalog using the _Search/Retrieval via URL_ (SRU) protocol
* resolves supplier information via the [Directory API](./../directory/api.yaml)
* checks item availability using the Z3.50 or the SRU protocol
* negotiates loans with external suppliers (e.g Alma or ReShare) via ISO18626
* allows internal requesters and suppliers to manage ILL requests using a convenient JSON API
* provides ILS integration for internal requesters and suppliers via NCIP

# API

The broker exposes a JSON API that addresses two use cases:

1. The `ILL Transactions API` endpoints allow monitoring ILL transactions and events and managing transaction-related entities
   such as peers and located suppliers.
   ILL transactions handled through this API are usually created by external ILL clients (e.g Alma or ReShare) via the ISO18626 protocol.
   See the [Broker API Specification](./oapi/open-api.yaml) for details.

2. The `Patron Request API` is used to create and manage ILL borrowing and lending requests directly in the broker.
   The lifecycle of a _Patron Request_ is governed by a state model—a specification of allowed states, actions, and transitions. See the [State Model Schema](./../misc/state-model.json) and the embedded [state model for returnable loans and non-returnable copies](./../misc/state-models.yaml), whose conditional elements use `appliesTo.serviceTypes`.
   This API supports building multi-tenant management/staff UIs on top of the broker or tightly integrating the broker into existing solutions.
   Internally, the broker creates an ILL transaction to back the execution of a _Patron Request_ so that the detailed monitoring is available through the `ILL Transactions API`.
   See the [Broker API Specification](./oapi/open-api.yaml) for details, where relevant endpoints are tagged with `patron-requests-api`.

The broker’s APIs use hyperlinks to connect JSON resources.
If you use Chrome or another browser to explore the API,
consider installing an extension like [JSON Formatter Classic](https://chromewebstore.google.com/detail/json-formatter-classic/caacnjeoikecoeepknkbjdcaediamaej), which makes hyperlinked JSON easier to navigate.

Note on FOLIO integration: selected API endpoints are available under the base path `/broker` when the `TENANT_TO_SYMBOL` environment variable is set.
This enables the broker to operate as a FOLIO/Okapi module with authentication/authorization and multi-tenancy support;
see the [ModuleDescriptor](./descriptors/ModuleDescriptor-template.json) for details.

# Compatibility with external peers

ISO18626 is designed as a peer-to-peer protocol and, as such, does not include any specific provisions (e.g., message types or statuses) for broker-based operations.

CrossLink Broker relies on regular ISO18626 exchanges to provide broker-specific functionality in a protocol-compliant manner:

1. Automatically sends a requester-facing ISO18626 `ExpectToSupply` message each time a new supplier is selected.
2. Forwards supplier `Unfilled` messages as `Notification` messages rather than `StatusChange` messages, to avoid terminating the borrowing request too early.
   Regular `Unfilled` status is communicated at the end of the transaction when the broker exhausts all suppliers ("end of rota").
3. _local supply_ feature: detects when the supplier is part of or the same institution as the requester and acts accordingly to the selected mode

To remain compatible with existing external ISO18626 peers, CrossLink Broker can operate in two modes:

1. `opaque` -- in this mode, Broker's own symbol (set via the `BROKER_SYMBOL` env var) is used in the headers of outgoing messages, behaving as a regular ISO18626 peer.
   For external peer messages, the actual supplier is identified by prepending `Supplier: {symbol}` to the message note field.
   `ExpectToSupply` messages after a supplier change are sent as a `Notification` rather than a `StatusChange`.
   Supplier notifications from skipped suppliers are still forwarded, with the original supplier identified in the note.
   Upon detecting _local supply_, the supplier is skipped.

2. `transparent` -- the requester and supplier symbols are used in the forwarded message headers, thus fully revealing both parties.
   When detecting _local supply_, messages are not forwarded but instead handled between the requester and the broker.

The broker mode can be configured for each peer individually by setting the `BrokerMode` field on the `peer` entity (via the `/peers/:id` endpoint). Unless explicitly set, the broker will configure the `BrokerMode` automatically based on the peer `Vendor` field as follows:

* vendor `Alma` -> external peer in `opaque` mode
* vendor `ReShare` -> external peer in `transparent` mode
* vendor `ILLiad` -> external peer in `opaque` mode
* vendor `CrossLink` -> internal peer, ILL requests are managed via the Patron Requests API, no outgoing ISO18626
* vendor `Unknown` -> mode set via the fallback `BROKER_MODE` env var, `opaque` by default

Additionally, the broker includes a _shim_ layer to modify the ISO18626 messages using vendor-specific logic.
This is often needed as ISO18626 implementations tend to diverge from the standard and may include custom extensions.

Note that for all modes, the broker attaches Directory information about the supplier and the requester by

* appending `requestingAgencyInfo` and `supplierInfo` fields to the outgoing lending `request` message
* appending `returnInfo` field to the outgoing `Loaned` supplying agency message

# Configuration

Configuration is provided via environment variables:

| Name                             | Description                                        | Default value                             |
|----------------------------------|----------------------------------------------------|-------------------------------------------|
| `HTTP_PORT`                      | Server port                                        | `8081`                                    |
| `DB_TYPE`                        | Database type                                      | `postgres`                                |
| `DB_USER`                        | Database user                                      | `crosslink`                               |
| `DB_PASSWORD`                    | Database password                                  | `crosslink`                               |
| `DB_HOST`                        | Database host                                      | `localhost`                               |
| `DB_DATABASE`                    | Database name                                      | `crosslink`                               |
| `DB_PORT`                        | Database port                                      | `25432`                                   |
| `DB_SCHEMA`                      | Database schema to use                             | `crosslink_broker`                        |
| `DB_PROVISION`                   | Should app create DB role/schema (`true`/`false`)  | `false`                                   |
| `DB_MIGRATE`                     | Should app run DB migrations (`true`/`false`)      | `true`                                    |
| `DB_EXPLAIN_ANALYZE`             | Whether to run `EXPLAIN ANALYZE` on patron         | `false`                                   |
|                                  | requests limited by CQL                            |                                           |
| `LOG_LEVEL`                      | Log level: `ERROR` , `WARN` , `INFO` , `DEBUG`     | `INFO`                                    |
| `ENABLE_JSON_LOG`                | Should JSON log format be enabled                  | `false`                                   |
| `BROKER_MODE`                    | Default broker mode if not configured for a peer:  | `opaque`                                  |
|                                  | `opaque` or `transparent`                          |                                           |
| `BROKER_SYMBOL`                  | Symbol for the broker when in the `opaque` mode    | `ISIL:BROKER`                             |
| `REQ_AGENCY_INFO`                | Should `request/requestingAgencyInfo` be populated | `true`                                    |
|                                  | from Directory Deprecated: use requester           |                                           |
|                                  | `illConfig.includeRequestingAgencyInfo` .          |                                           |
| `SUPPLIER_INFO`                  | Should `request/supplierInfo` be populated from    | `true`                                    |
|                                  | Directory Deprecated: use supplier                 |                                           |
|                                  | `illConfig.includeSupplierInfo` .                  |                                           |
| `RETURN_INFO`                    | Should `returnInfo` be populated from Directory    | `true`                                    |
|                                  | for supplier `Loaned` message Deprecated: use      |                                           |
|                                  | supplier `illConfig.includeReturnInfo` .           |                                           |
| `VENDOR_NOTE`                    | Should `note` field be prepended with              | `true`                                    |
|                                  | `Vendor: {vendor}` text Deprecated: use requester  |                                           |
|                                  | `illConfig.includeVendorNote` .                    |                                           |
| `OFFERED_COSTS`                  | Should `deliveryCosts` be transferred to           | `false`                                   |
|                                  | `offeredCosts` for ReShare vendor requesters       |                                           |
|                                  | Deprecated: use requester                          |                                           |
|                                  | `illConfig.useOfferedCosts` .                      |                                           |
| `NOTE_FIELD_SEP`                 | Separator for fields (e.g. Vendor) prepended to    | `, `                                      |
|                                  | the note Deprecated: use recipient                 |                                           |
|                                  | `illConfig.noteFieldSeparator` .                   |                                           |
| `CLIENT_DELAY`                   | Delay duration for outgoing ISO18626 messages      | `0ms`                                     |
| `SHUTDOWN_DELAY`                 | Delay duration for graceful shutdown (in-flight    | `15s`                                     |
|                                  | connections)                                       |                                           |
| `MAX_MESSAGE_SIZE`               | Max accepted ISO18626 message size                 | `100KB`                                   |
| `HOLDINGS_ADAPTER`               | Holdings lookup method: `mock` , `sru` or          | `mock`                                    |
|                                  | `consortium`                                       |                                           |
| `HOLDINGS_SRU_URL`               | Comma separated list of URLs when                  | `http://localhost:8081/sru`               |
|                                  | `HOLDINGS_ADAPTER` is `sru`                        |                                           |
| `HOLDINGS_ISXN_LOOKUP`           | Whether to use ISBN/ISSN lookup for `sru` method   | `false`                                   |
| `HOLDINGS_FORMAT`                | Parser for SRU holdings: `reservoir` , `marc` ,    | `reservoir`                               |
|                                  | `opac` or `MARC-21plus-1`                          |                                           |
| `CONSORTIUM_SYMBOL`              | Designates peer for which configuration is used    | (empty value)                             |
|                                  | for consortium. At this time, it is used when      |                                           |
|                                  | `HOLDINGS_ADAPTER` = `consortium` .                |                                           |
| `DIRECTORY_ADAPTER`              | Directory lookup method: `mock` or `api`           | `mock`                                    |
| `DIRECTORY_API_URL`              | Comma separated list of URLs when                  | `http://localhost:8086/directory/entries` |
|                                  | `DIRECTORY_ADAPTER` is `api`                       |                                           |
| `AVAILABILITY_ADAPTER`           | Availability adapter: `mock` , `zoom` ,            | `zoom`                                    |
|                                  | `metaproxy` . see                                  |                                           |
|                                  | [Building with native extensions (CGO)][cgo]       |                                           |
| `METAPROXY_URL`                  | Metaproxy URL when `AVAILABILITY_ADAPTER` =        | (empty value)                             |
|                                  | `metaproxy`                                        |                                           |
| `PEER_REFRESH_INTERVAL`          | Peer refresh interval (via Directory lookup)       | `5m`                                      |
| `MOCK_PEER_URL`                  | Mocked peer URLs value when `DIRECTORY_ADAPTER` is | `http://localhost:19083/iso18626`         |
|                                  | `mock`                                             |                                           |
| `MOCK_PICKUP_INSTITUTION_SYMBOL` | Institution symbol owning synthetic pickup         | `ISIL:MOCK`                               |
|                                  | locations in mock directory mode; use this as the  |                                           |
|                                  | requester symbol for selected pickup locations     |                                           |
| `API_PAGE_SIZE`                  | Default value for the `limit` query parameter when | `10`                                      |
|                                  | paging the API                                     |                                           |
| `TENANT_TO_SYMBOL`               | Pattern to map tenant to `requesterSymbol` when    | (empty value)                             |
|                                  | accessing the API via Okapi, the `{tenant}` token  |                                           |
|                                  | is replaced by the `X-Okapi-Tenant` header value.  |                                           |
|                                  | If pattern is exactly `directory` the symbol will  |                                           |
|                                  | be obtained by directory lookup.                   |                                           |
| `SUPPLIER_PATRON_PATTERN`        | Pattern used to create patron ID when receiving    | `%v_user`                                 |
|                                  | Request on supplier side Deprecated: use supplier  |                                           |
|                                  | `illConfig.supplierPatronPattern` .                |                                           |
| `LANGUAGE`                       | Language parameter used for ts_vector search in DB | `english`                                 |
| `SCHEDULER_RETRY_DELAY`          | Delay for rescheduling failed scheduled tasks and  | `5m`                                      |
|                                  | fallback poll interval in `waitUntil`              |                                           |
| `SMTP_HOST`                      | SMTP server host for sending emails, if not        | (empty value)                             |
|                                  | configured all email tasks will fail               |                                           |
| `SMTP_PORT`                      | SMTP server port                                   | `2525`                                    |
| `SMTP_USERNAME`                  | Username for SMTP authentication                   | (empty value)                             |
| `SMTP_PASSWORD`                  | Password for SMTP authentication                   | (empty value)                             |
| `BATCH_PULLSLIP_MAX_COUNT`       | Max count of Patron request to include in pullslip | `100`                                     |
|                                  | batch                                              |                                           |
| `BATCH_ACTION_RUN_RETENTION`     | Number of batch action events to retain. Set to 0  | `5`                                       |
|                                  | to disable retention cleanup.                      |                                           |

[cgo]: #building-with-native-extensions-cgo

Availability checks are enabled per supplier by the presence of `catalogConfig.sru` or `catalogConfig.zoom` in its Directory entry.
Other catalog settings, such as `metadataUpdateMode`, do not enable availability checks. A successful lookup with no holdings skips
the supplier and advances the rota.
Adapter, lookup, and result-processing failures are recorded as errors but fail open: the selected supplier still receives the request.
This prevents a transient catalog failure from being treated as confirmed unavailability.

# Build

Generate sources and compile the main programs with:

```
make
```

This will build the following binaries:

* `broker` — the main program for the ILL service
* `archive` — a utility for archiving old ILL transactions

You can also run included tests with:

```
make check
```

or run test for selected `_test` package

```
go test -v -coverpkg=./... -cover ./cmd/broker
```

## Building with native extensions (CGO)

The `zoom` availability adapter requires the native `libyaz` library and CGO to be enabled during the build (the default).

Install `libyaz` using your OS package manager:

- **Debian/Ubuntu:** `sudo apt-get install libyaz-dev`
- **RHEL/CentOS** (requires EPEL)**:** `sudo yum install libyaz-devel`
- **Fedora:** `sudo dnf install libyaz-devel`
- **macOS:** `brew install yaz`

To build without native extensions, disable CGO:

```
CGO_ENABLED=0 make
```

This will make `zoom` adapter unavailable and the `metaproxy` adapter should be used instead.

# Run locally

You can run the `broker` program locally with:

```
make run
```

The application requires a Postgres DB and will use hard-coded default DB connection params unless configured, see `DB_*` env vars above.

If `DB_PROVISION=true`, default `false`, the configured database user must have privileges to create roles and schemas in the database (the `CREATE` privilege on the database and the ability to run `CREATE SCHEMA`). The `DB_SCHEMA` env must be non-empty when provisioning (default).
If `DB_PROVISION=false`, schema and role provisioning must be done before startup.

If `DB_MIGRATE=true`, default, the app runs migrations on startup. Migrations will create and update all required tables and other objects in the schema. Empty `DB_SCHEMA` means default user
schema is used (usually `public`).

You can execute provisioning- or migration-only via `/broker db-up`, after which the app will terminate.

NOTE: For production use it's recommended to disable `DB_PROVISION` and separately provision a runtime user (e.g. `crosslink`)
with `CONNECT` to the target database and public privileges locked down and an owner role (e.g `crosslink_broker`)
with full privileges on the dedicated schema granted to the user.
See the example [DB provisioning script](../misc/db-provision.sql).
Optionally, with `DB_MIGRATE` off, migrations can be performed separately and the runtime user won't require any `CREATE` privileges.

To run locally in a container, there is a `docker-compose.yml` file prepared with both the app and the DB.

To start just the DB container with default connection params:

```
docker compose up -d postgres
```

To run db-up only (and exit):

```
docker compose --profile db-up run --rm db-up
```

Start the default stack (DB + broker; broker runs provision and migrations on startup):

```
docker compose up
```

### Supplier pull slips and shipment

For Loan and CopyOrLoan requests, generating a pull-slip PDF queues the `pullslip-printed` action for the included eligible supplier requests. Accepted conditions return the supplier to `WILL_SUPPLY`. Printing moves `WILL_SUPPLY` to `SEARCHING` (picking and awaiting shipment). Reprinting in `SEARCHING` leaves the state unchanged. The `ship` action is available only in `SEARCHING`. Copy delivery remains available without this loan workflow.

The `email-pullslips` batch queues the same action after SMTP successfully accepts an email containing a PDF. Emails without PDFs and failed generation or sending do not advance requests. Actions run asynchronously and recheck the current state. If queuing fails after output, the operation reports an error; retrying may reproduce the PDF or email, while repeated `pullslip-printed` actions in `SEARCHING` are harmless. Existing saved batch queries are not rewritten; use `WILL_SUPPLY` for pull-slip queries and include `SEARCHING` in aging queries as needed.

## Loan recall

Lenders can invoke `recall` from `RECEIVED`, `RENEWED`, `OVERDUE`, or
`RENEWAL_PENDING` for `Loan` and `CopyOrLoan`. Optional action parameters are
`note` and `dueDate` (a date or RFC3339 timestamp). An omitted or null date
preserves the current deadline, including an open-ended loan; a date without
a time means the end of the supplier's calendar day. Blank or invalid dates
are rejected. A recall date may be in the past, allowing an immediate return
request for an already overdue loan.

The action sends ISO18626 `StatusChange` / `Recalled` and moves the lender to
`RECALLED` only after successful sending. The borrower enters `RECALLED` with
staff attention required. Its primary action is `ship-return`; `check-in`
remains available, while checkout and renewal are unavailable. Recall
supersedes pending renewal. Duplicate recall and late renewal/overdue messages
preserve the recall status and deadline; recall after return shipment does
not reopen the loan. The existing return and completion steps still apply.
Recall during outbound shipment is not supported.
