# illmock

The illmock program is a mocking ISO18626 client / server utility. It is mostly
controlled by environment variables.

illmock is acting as both request and supplier as the XML ISO18626 message can determine its role.

Example of launching two illmock instances that will send messages to each other.

    HTTP_PORT=8082 PEER_URL=http://localhost:8081 ./illmock

    HTTP_PORT=8081 PEER_URL=http://localhost:8082 ./illmock

We will use the former as a requester and the latter as a supplier, by sending a Patron Request
to the former with:

    curl -XPOST -HContent-Type:text/xml \ -d'<ISO18626Message><request><bibliographicInfo><supplierUniqueRecordId>WILLSUPPLY_LOANED</supplierUniqueRecordId>
     </bibliographicInfo><serviceInfo><requestType>New</requestType><requestSubType>PatronRequest</requestSubType><serviceType>
     </serviceType></serviceInfo></request></ISO18626Message>' http://localhost:8081/iso18626

The supplierUniqueRecordId value is used by supplier to perform a particular scenario.

## Scenario

The scenario is used by the supplier to perform a particular work-flow. The following values are recognized:

    WILLSUPPLY_LOANED
    WILLSUPPLY_UNFILLED
    UNFILLED
    LOANED

The scenario is inspected as part of `<bibliographicInfo><supplierUniqueRecordId>` or it may be given
as environment variable `AGENCY_SCENARIO`.

## Environment variables

### HTTP_PORT

Listen adress + port. If empty or omitted, the program will listen on any interface, port `8081`.

If the value includes a colon, it is assumed to be listening address and port, for example: `127.0.0.1:8090`.
Without colon, it translates to `:`value which will bind on any interface and port given.

### PEER_URL

The default value is `http://localhost:8081` .

### AGENCY_SCENARIO

If `AGENCY_SCENARIO` is defined and non-empty the illmock program will initiate requests during start (100 ms after launch).
See [Scenario].

### AGENCY_TYPE

If omitted or empty, a value of `MOCK` is used.

### SUPPLYING_AGENCY_ID

If omitted or empty, a value of `SUP` is used.

### REQUESTING_AGENCY_ID

If omitted or empty, a value of `REQ` is used.
