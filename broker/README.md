# Introduction

CrossLink broker manages inter-library loan (ILL) transactions, specifically:

* accepts and handles ISO18626 requests
* locates suppliers via _Search/Retrieval via URL_ (SRU) protocol
* resolves suppliers address via the Directory API
* negotiates loans with suppliers via ISO18626
* forwards the settled loan notification to requesters

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

CrossLink broker can operate in three modes:

1. `opaque` -- in this mode, the broker behaves like a regular ISO18626 peer and does not reveal any information about the located supplier. Broker's own symbol (set via the `BROKER_SYMBOL` env var) is used in the header of outgoing messages.

2. `translucent` -- in this mode the broker sends an ISO `ExpectToSupply` message to the requester to notify it each time a new or changed supplier is selected. Also in this mode, the broker supports the _local supply_ feature where it detects that the selected supplier is the same institution as the requester or one of its branches. With _local supply_, messages are handled directly in the broker and are not proxied to the supplier.

3. `transparent` -- this mode is identical to the `translucent` mode but, additionally, the broker transmits the requester and supplier symbols in the proxied message header, thus fully revealing both parties to each other.

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
| `BROKER_MODE`          | Default broker mode if not configured for a peer: `opaque`, `transparent` or `translucent`| `opaque`                                  |
| `BROKER_SYMBOL`        | Symbol for the broker when in the `opaque` mode                                           | `ISIL:BROKER`                             |
| `REQ_AGENCY_INFO`      | Should `request/requestingAgencyInfo` be populated from Directory                         | `true`                                    |
| `SUPPLIER_INFO`        | Should `request/supplierInfo` be populated from Directory                                 | `true`                                    |
| `RETURN_INFO`          | Should `returnInfo` be populated from Directory for supplier `Loaned` message             | `true`                                    |
| `VENDOR_INFO`          | Should `note` be prepended with `#Vendor: xxx` note                                       | `true`                                    |
| `CLIENT_DELAY`         | Delay duration for outgoing ISO18626 messages                                             | `0ms`                                     |
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

Generate sources and compile the main program with:

```
make
```

You can also run included tests with:

```
make check
```

or run test for selected `_test` package

```
go test -v -coverpkg=./.. -cover ./cmd/broker
```

# Run locally

You can run the program locally with:

```
make run
```

The application needs a Postgres DB.
There is a `docker-compose.yml` file prepared to start the DB with default user credentials and a default port:

```
docker compose up
```
