# Introduction

CrossLink broker is a system to manage inter-library loans (ILL), specifically it:

* accepts and handles ILL requests from external requesters (e.g Alma or ReShare) via the ISO18626 protocol
* locates suppliers with available holdings using the _Search/Retrieval via URL_ (SRU) protocol
* resolves supplier information via the [Directory API](./../directory/directory_api.yaml)
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
   The lifecycle of a _Patron Request_ is governed by a state model—a specification of allowed states, actions, and transitions, see the [State Model Schema](./../misc/state-model.json) and a specific implementation for handling [returnables](./../misc/returnables.yaml).
   This API supports building multi-tenant management/staff UIs on top of the broker or tightly integrating the broker into existing solutions.
   Internally, the broker creates an ILL transaction to back the execution of a _Patron Request_ so that the detailed monitoring is available through the `ILL Transactions API`.
   See the [Broker API Specification](./oapi/open-api.yaml) for details, where relevant endpoints are tagged with `patron-requests-api`.

The broker’s APIs use hyperlinks to connect JSON resources.
If you use Chrome or another browser to explore the API,
consider installing an extension like [JSON Formatter](https://chromewebstore.google.com/detail/json-formatter/bcjindcccaagfpapjjmafapmmgkkhgoa), which makes hyperlinked JSON easier to navigate.

Note on FOLIO integration: selected API endpoints are available under the base path `/broker` when the `TENANT_TO_SYMBOL` environment variable is set.
This enables the broker to operate as a FOLIO/Okapi module with authentication/authorization and multi-tenancy support;
see the [ModuleDescriptor](./descriptors/ModuleDescriptor-template.json) for details.

# Compatibility with external peers

ISO18626 is designed as a peer-to-peer protocol and, as such, does not include any specific provisions (e.g., message types or statuses) for broker-based operations.

CrossLink Broker relies on regular ISO18626 exchanges to provide broker-specific functionality in a protocol-compliant manner:

1. Automatically sends ISO18626 `ExpectToSupply` message to the requester each time a new supplier is selected
2. Forwards the ISO18626 `Unfilled` message from the supplier as a `Notification` rather than a `StatusChange`, to avoid terminating the lending request too early.
   Regular `Unfilled` status is communicated at the end of the transaction when the broker exhausts all suppliers ("end of rota").
3. _local supply_ feature: detects when the supplier is part of or the same institution as the requester and acts accordingly to the selected mode

To remain compatible with exiting external ISO18626 peers, CrossLink Broker can operate in two modes:

1. `opaque` -- in this mode, Broker's own symbol (set via the `BROKER_SYMBOL` env var) is used in the headers of outgoing messages, behaving as a regular ISO18626 peer.
   Actual supplier is identified by prepending `Supplier: {symbol}` to the message note field, see `SUPPLIER_SYMBOL_NOTE` env var.
   `ExpectToSupply` messages after a supplier change are sent as a `Notification` rather than a `StatusChange`.
   Upon detecting _local supply_, the supplier is skipped.

2. `transparent` -- the requester and supplier symbols are used in the forwarded message headers, thus fully revealing both parties.
   When detecting _local supply_, messages are not forwarded but instead handled between the requester and the broker.

The broker mode can be configured for each peer individually by setting the `BrokerMode` field on the `peer` entity (via the `/peers/:id` endpoint). Unless explicitly set, the broker will configure the `BrokerMode` automatically based on the peer `Vendor` field as follows:

* vendor `Alma` -> external peer in `opaque` mode
* vendor `ReShare` -> external peer in `transparent` mode
* vendor `CrossLink` -> internal peer, ILl requests are managed via the Patron Requests API, no outgoing ISO18626
* vendor `Unknown` -> mode set via the fallback `BROKER_MODE` env var, `opaque` by default

Additionally, the broker includes a _shim_ layer to modify the ISO18626 messages using vendor-specific logic.
This is often needed as ISO18626 implementations tend to diverge from the standard and may include custom extensions.

Note that for all modes, the broker attaches Directory information about the supplier and the requester by

* appending `requestingAgencyInfo` and `supplierInfo` fields to the outgoing lending `request` message
* appending `returnInfo` field to the outgoing `Loaned` supplying agency message

# Configuration

Configuration is provided via environment variables:

| Name                      | Description                                                                           | Default value                             |
|---------------------------|---------------------------------------------------------------------------------------|-------------------------------------------|
| `HTTP_PORT`               | Server port                                                                           | `8081`                                    |
| `DB_TYPE`                 | Database type                                                                         | `postgres`                                |
| `DB_USER`                 | Database user                                                                         | `crosslink`                               |
| `DB_PASSWORD`             | Database password                                                                     | `crosslink`                               |
| `DB_HOST`                 | Database host                                                                         | `localhost`                               |
| `DB_DATABASE`             | Database name                                                                         | `crosslink`                               |
| `DB_PORT`                 | Database port                                                                         | `25432`                                   |
| `DB_SCHEMA`               | Database schema to use                                                                | `crosslink_broker`                        |
| `LOG_LEVEL`               | Log level: `ERROR`, `WARN`, `INFO`, `DEBUG`                                           | `INFO`                                    |
| `ENABLE_JSON_LOG`         | Should JSON log format be enabled                                                     | `false`                                   |
| `BROKER_MODE`             | Default broker mode if not configured for a peer: `opaque` or `transparent`           | `opaque`                                  |
| `BROKER_SYMBOL`           | Symbol for the broker when in the `opaque` mode                                       | `ISIL:BROKER`                             |
| `REQ_AGENCY_INFO`         | Should `request/requestingAgencyInfo` be populated from Directory                     | `true`                                    |
| `SUPPLIER_INFO`           | Should `request/supplierInfo` be populated from Directory                             | `true`                                    |
| `RETURN_INFO`             | Should `returnInfo` be populated from Directory for supplier `Loaned` message         | `true`                                    |
| `VENDOR_NOTE`             | Should `note` field be prepended with `Vendor: {vendor}` text                         | `true`                                    |
| `SUPPLIER_SYMBOL_NOTE`    | Should `note` field be prepended with a `Supplier: {symbol}` text, `opaque` mode only | `true`                                    |
| `OFFERED_COSTS`           | Should `deliveryCosts` be transferred to `offeredCosts` for ReShare vendor requesters | `false`                                   |
| `NOTE_FIELD_SEP`          | Separator for fields (e.g. Vendor) prepended to the note                              | `, `                                      |
| `CLIENT_DELAY`            | Delay duration for outgoing ISO18626 messages                                         | `0ms`                                     |
| `SHUTDOWN_DELAY`          | Delay duration for graceful shutdown (in-flight connections)                          | `15s`                                     |
| `MAX_MESSAGE_SIZE`        | Max accepted ISO18626 message size                                                    | `100KB`                                   |
| `HOLDINGS_ADAPTER`        | Holdings lookup method: `mock` or `sru`                                               | `mock`                                    |
| `SRU_URL`                 | Comma separated list of URLs when `HOLDINGS_ADAPTER` is `sru`                         | `http://localhost:8081/sru`               |
| `DIRECTORY_ADAPTER`       | Directory lookup method: `mock` or `api`                                              | `mock`                                    |
| `DIRECTORY_API_URL`       | Comma separated list of URLs when `DIRECTORY_ADAPTER` is `api`                        | `http://localhost:8081/directory/entries` |
| `PEER_REFRESH_INTERVAL`   | Peer refresh interval (via Directory lookup)                                          | `5m`                                      |
| `MOCK_CLIENT_URL`         | Mocked peer URLs value when `DIRECTORY_ADAPTER` is `mock`                             | `http://localhost:19083/iso18626`         |
| `API_PAGE_SIZE`           | Default value for the `limit` query parameter when paging the API                     | `10`                                      |
| `TENANT_TO_SYMBOL`        | Pattern to map tenant to `requesterSymbol` when accessing the API via Okapi,          | (empty value)                             |
|                           | the `{tenant}` token is replaced by the `X-Okapi-Tenant` header value                 |                                           |
| `SUPPLIER_PATRON_PATTERN` | Pattern used to create patron ID when receiving Request on supplier side              | `%v_user`                                 |

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

# Run locally

You can run the `broker` program locally with:

```
make run
```

The application needs a Postgres DB.
There is a `docker-compose.yml` file prepared to start the DB with default user credentials and a default port:

```
docker compose up
```
