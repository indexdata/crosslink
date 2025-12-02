# Introduction

CrossLink broker manages inter-library loan (ILL) transactions, specifically:

* accepts and handles ISO18626 lending requests
* locates suppliers with available holdings with the _Search/Retrieval via URL_ (SRU) protocol
* resolves supplier information via the Directory API
* negotiates loans with suppliers via ISO18626
* forwards ISO18626 messages between requester and selected supplier

# API

See the [Broker API Specification](./oapi/open-api.yaml) for details.

The broker's API uses hyperlinks to connect JSON resources.
If you're using Chrome or another browser to explore the API,
consider using an extension like [JSON Formatter](https://chromewebstore.google.com/detail/json-formatter/bcjindcccaagfpapjjmafapmmgkkhgoa) which allows easy navigation hyperlinked JSON.

Note that selected read-only API endpoints are accessible with the base path `/broker`
if env `TENANT_TO_SYMBOL` is defined.
This allows the broker to be used as a FOLIO/Okapi module,
see the [ModuleDescriptor](./descriptors/ModuleDescriptor-template.json) for details.

# Mode of operation

ISO18626 is designed as a peer-to-peer protocol and, as such, does not include any specific provisions (e.g., message types or statuses) for broker-based operations.

CrossLink Broker relies on regular ISO18626 messages to provide broker-specific functionality:

1. Automatically sends ISO18626 `ExpectToSupply` message to the requester each time a new supplier is selected
2. Forwards the ISO18626 `Unfilled` message from the supplier as a `Notification` rather than a `StatusChange`, to avoid terminating the request.
   Regular `Unfilled` status is communicated at the end of the transaction when the broker exhausts all suppliers ("end of rota").
3. _local supply_ feature: detects when the supplier is part of or the same institution as the requester

To remain compatible with standard ISO18626 peers, CrossLink Broker can operate in two modes:

1. `opaque` -- in this mode, Broker's own symbol (set via the `BROKER_SYMBOL` env var) is used in the headers of outgoing messages, behaving as a regular ISO18626 peer.
   Actual supplier can be identified by prepending `Supplier: {symbol}` to the message note field, see `SUPPLIER_SYMBOL_NOTE` env var.
   `ExpectToSupply` messages after a supplier change are sent as a `Notification` rather than `StatusChange`.
   Upon detecting _local supply_, the supplier is skipped.

2. `transparent` -- the requester and supplier symbols are used in the forwarded message headers, thus fully revealing both parties.
   When detecting _local supply_, messages are not forwarded but instead handled between the requester and the broker.

The broker mode can be configured for each peer individually by setting the `BrokerMode` field on the `peer` entity (via the `/peers/:id` endpoint). Unless explicitly set, the broker will configure the `BrokerMode` based on the peer `Vendor` field as follows:

* vendor `Alma` -> mode `opaque`
* vendor `ReShare` -> mode `transparent`
* vendor `Unknown` -> fallback mode set via the `BROKER_MODE` env var, `opaque` by default

Note that for all modes, the broker attaches Directory information about the supplier and the requester by

* appending `requestingAgencyInfo` and `supplierInfo` fields to the outgoing lending `request` message
* appending `returnInfo` field to the outgoing `Loaned` supplying agency message

# Configuration

Configuration is provided via environment variables:

| Name                   | Description                                                                               | Default value                             |
|------------------------|-------------------------------------------------------------------------------------------|-------------------------------------------|
| `HTTP_PORT`            | Server port                                                                               | `8081`                                    |
| `DB_TYPE`              | Database type                                                                             | `postgres`                                |
| `DB_USER`              | Database user                                                                             | `crosslink`                               |
| `DB_PASSWORD`          | Database password                                                                         | `crosslink`                               |
| `DB_HOST`              | Database host                                                                             | `localhost`                               |
| `DB_DATABASE`          | Database name                                                                             | `crosslink`                               |
| `DB_PORT`              | Database port                                                                             | `25432`                                   |
| `LOG_LEVEL`            | Log level: `ERROR`, `WARN`, `INFO`, `DEBUG`                                               | `INFO`                                    |
| `ENABLE_JSON_LOG`      | Should JSON log format be enabled                                                         | `false`                                   |
| `BROKER_MODE`          | Default broker mode if not configured for a peer: `opaque` or `transparent`               | `opaque`                                  |
| `BROKER_SYMBOL`        | Symbol for the broker when in the `opaque` mode                                           | `ISIL:BROKER`                             |
| `REQ_AGENCY_INFO`      | Should `request/requestingAgencyInfo` be populated from Directory                         | `true`                                    |
| `SUPPLIER_INFO`        | Should `request/supplierInfo` be populated from Directory                                 | `true`                                    |
| `RETURN_INFO`          | Should `returnInfo` be populated from Directory for supplier `Loaned` message             | `true`                                    |
| `VENDOR_NOTE`          | Should `note` field be prepended with `Vendor: {vendor}` text                             | `true`                                    |
| `SUPPLIER_SYMBOL_NOTE` | Should `note` field be prepended with a `Supplier: {symbol}` text, `opaque` mode only     | `true`                                    |
| `OFFERED_COSTS`        | Should `deliveryCosts` be transferred to `offeredCosts` for ReShare vendor requesters     | `false`                                   |
| `NOTE_FIELD_SEP`       | Separator for fields (e.g. Vendor) prepended to the note                                  | `, `                                      |
| `CLIENT_DELAY`         | Delay duration for outgoing ISO18626 messages                                             | `0ms`                                     |
| `SHUTDOWN_DELAY`       | Delay duration for graceful shutdown (in-flight connections)                              | `15s`                                     |
| `MAX_MESSAGE_SIZE`     | Max accepted ISO18626 message size                                                        | `100KB`                                   |
| `HOLDINGS_ADAPTER`     | Holdings lookup method: `mock` or `sru`                                                   | `mock`                                    |
| `SRU_URL`              | Comma separated list of URLs when `HOLDINGS_ADAPTER` is `sru`                             | `http://localhost:8081/sru`               |
| `DIRECTORY_ADAPTER`    | Directory lookup method: `mock` or `api`                                                  | `mock`                                    |
| `DIRECTORY_API_URL`    | Comma separated list of URLs when `DIRECTORY_ADAPTER` is `api`                            | `http://localhost:8081/directory/entries` |
| `PEER_REFRESH_INTERVAL`| Peer refresh interval (via Directory lookup)                                              | `5m`                                      |
| `MOCK_CLIENT_URL`      | Mocked peer URLs value when `DIRECTORY_ADAPTER` is `mock`                                 | `http://localhost:19083/iso18626`         |
| `API_PAGE_SIZE`        | Default value for the `limit` query parameter when paging the API                         | `10`                                      |
| `TENANT_TO_SYMBOL`     | Pattern to map tenant to `requesterSymbol` when accessing the API via Okapi,              | (empty value)                             |
|                        | the `{tenant}` token is replaced by the `X-Okapi-Tenant` header value                     |                                           |

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
go test -v -coverpkg=./.. -cover ./cmd/broker
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
