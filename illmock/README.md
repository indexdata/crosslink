# Intro

The `illmock` is a service offering:

 * Mock ILL ISO18626 supplier and requester roles
 * UI form for testing ILL
 * ILL flows service
 * Mock SRU/OASIS searchRetrieve service
 * Mock Directory entries service

# ILL service

The ISO18626 protocol endpoint is available at the `/iso18626` URI path.

`illmock` can operate as both an ILL requester and an ILL supplier, depending
on the type of ISO18626 message it processes:

  * standard `Request` message --> supplier role
  * `Request` with `SubType` = `PatronRequest` --> requester role, standard `Request` is sent to configured peer
  * `RequstingAgencyMessage` --> supplier role
  * `SupplyingAgencyMessage` --> requester role

`illmock` instance requires a peer URL to be configured for sending follow-up messages.
A static value can be provided via the `PEER_URL` env var or the client can configure it
dynamically by setting the following fields in the incoming `Request` message:

  * `<RequestingAgencyInfo>/<Address>/<ElectronicAddress>/<ElectronicAddressData>` sets the requester peer URL
  * `<SupplierInfo>/<Description>` sets the the supplier peer URL

Example of launching two `illmock` instances that will send messages to each
other:

    HTTP_PORT=8082 PEER_URL=http://localhost:8081/iso18626 ./illmock
    HTTP_PORT=8081 PEER_URL=http://localhost:8082/iso18626 ./illmock

We will use the former as a requester and the latter as a supplier, by sending
a Patron Request with one of the sample message in directory `examples`:

    curl -XPOST -HContent-Type:text/xml -d@examples/req.xml http://localhost:8081/iso18626

The `requestingAgencyRequestId` is  auto-generated, if it's not provided in the `Request` header, and it is
reported in the `confirmationHeader` and the HTTP `X-Request-ID` header.

The requester and supplier `agencyId` are set to default values unless they are provided in the `Request` header.

## Submit form

The mock comes with a simple submit form at the `/form` path that can be used as an alternative to curl for posting ISO18626 requests.

## Supplier behavior

The `<bibliographicInfo>/<supplierUniqueRecordId>` value of incoming request is used to
invoke a particular scenario when acting as the supplier.

The scenario is used by the supplier to perform a particular workflow. The
following values are recognized:

| Scenario                  | Workflow                                                                            |
|---------------------------|-------------------------------------------------------------------------------------|
|`LOANED`                   | Respond with a `Loaned` message, finish with `LoanComplete`                         |
|`LOANED_OVERDUE`           | Respond with `Loaned`, then with a an `Overdue` and expect a `Renew`                |
|`UNFILLED`                 | Respond with `Unfilled` message                                                     |
|`WILLSUPPLY`               | Respond with `WillSupply` only                                                      |
|`WILLSUPPLY_LOANED`        | Respond with `WillSupply` then send `Loaned`                                        |
|`WILLSUPPLY_UNFILLED`      | Respond with `WillSupply` then send `Unfilled`                                      |
|`WILLSUPPLY_LOANED_OVERDUE`| Respond with `WillSupply` then send `Loaned` followed by `Overdue`                  |
|`COMPLETED`                | Respond with `CopyCompleted` if ServiceType=`Copy`; otherwise `LoanCompleted`       |
|`ERROR`                    | Respond with a `BadlyFormedMessage` message confirmation error                      |
|`HTTP-ERROR-400`           | Respond with HTTP `400` status                                                      |
|`HTTP-ERROR-500`           | Respond with HTTP `500` status                                                      |
|`RETRY:COND_` ...          | Response with `RetryPossible` and ReasonRetry `LoanCondition`                       |
|`RETRY:COST_` ...          | Response with `RetryPossible` and ReasonRetry+ReasonUnfilled `CostExceedsMaxCost`   |
|`RETRY:ONLOAN_` ...        | Response with `RetryPossible` and ReasonRetry `OnLoan`                              |

## Requester behavior

The PatronRequest's `<serviceInfo>/<note>` field is used to control the requester behavior.

The following values are recognized:

  * `#CANCEL#` the requester will send a `Cancel` action to the supplier upon receiving the first SupplyingAgencyMessage.
  For a sample, refer to `examples/cancel-req.xml`.

  * `#RENEW#` the requester will send a `Renew` request to the supplier upon receiving an `Overdue` message.
  For a sample, refer to `examples/renew-req.xml`.

# ILL flows

History of ILL messages can be retrieved at the `/api/flows` endpoint.
The endpoint takes optional query parameters:

  * `id` show flows for a particular `requestingAgencyRequestId`
  * `role` either `requester` or `supplier`
  * `requester` agency ID of the requesting agency
  * `supplier` agency ID of the supplying agency

For example:

    curl http://localhost:8081/api/flows?supplier=myid

# SRU service

The program offers an SRU service at URI path `/sru`. Only version 2.0
is supported. It is substantially different from version 1.1, 1.2 -
for example different namespace and different semantics for recordPacking.

The service produces a MARCXML record if a query of format "id = value" is
used. If the index (`id`) is omitted a SRU diagnostic is returned.

The identifier value is split by semicolon and each substring generates a holdings record entry
in the `999#11` field with subfield `$l` set to the local ID and subfield `$s` set to library ISIL.

By default each substring is taken verbatim, except for some special cases:

  * `error`: produces an SRU error (non-surrogate diagnostic).
  * `return-` prefix: produces a holdings entry with both `$l`, `$s` of the suffix.
  * `record-error`: produces SRU response with a diagnostic record.
  * `not-found` or empty: omits generating a holdings `$l`, `$s` entry.

For example to get a MARCXML with identifier 123, use:

    curl 'http://localhost:8081/sru?query=id%3D123'

With yaz-client:

    yaz-client http://localhost:8081/sru
    Z> sru get 2.0
    Z> f id=123
    Z> s

With zoomsh:

    zoomsh "set sru_version 2.0" "set sru get" \
        "connect http://localhost:8081/sru" \
        "search cql:id=123" "show 0 1" "quit"

# Directory service

The directory service is accessible from the `/directory/entries` endpoint. For example:

    curl http://localhost:8081/directory/entries

See [the OpenAPI spec](directory/directory_api.yaml) . The `cql` query parameter is a CQL string.
The only supported index is `symbol`. Supported relations are: `any`, `all`, `=`.

# Environment variables

| Name                  | Description                                                          | Default value                  |
|-----------------------|----------------------------------------------------------------------|--------------------------------|
|`HTTP_PORT`            | Listening `address:port` or just port, for example: `127.0.0.1:8090` |`8081`                          |
|`PEER_URL`             | Fallback URL of the peer                                             |`http://localhost:8081/iso18626`|
|`AGENCY_TYPE`          | Fallback message header agency type value                            |`MOCK`                          |
|`SUPPLYING_AGENCY_ID`  | Fallback supplier agency ID (symbol)                                 |`SUP`                           |
|`REQUESTING_AGENCY_ID` | Fallback requester agency ID (symbol)                                |`REQ`                           |
|`CLEAN_TIMEOUT`        | Specifies how long a flow is kept in memory before being removed     |`10m`                           |
|`MESSAGE_DELAY`        | Supplier: delay between each SupplyingAgencyMessage.                 |`100ms`                         |
|                       | Requester: delay before sending ShippedReturn.                       |                                |
|`HTTP_HEADERS`         | `;` separated extra HTTP client headers, e.g. `X-Okapi-Tenant:T1`    | none                           |
