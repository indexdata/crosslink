# illmock

The illmock program is a mocking ISO18626 client / server utility. It is mostly
controlled by environment variables.

if `AGENCY_SCENARIO` is given, illmock will send ILL requests. The
value is a list of comma separated of IDs that the requester will send
requests to (in parallel). Each identifier is stored in
BibliographicInfo.SupplierUniqueRecordId of the Request.

Example of starting two illmock instances. The first is acting as a supplier. The 2nd
is acting as supplier.

    HTTP_PORT=8082 PEER_URL=http://localhost:8081 ./illmock

    HTTP_PORT=8081 PEER_URL=http://localhost:8082 AGENCY_SCENARIO=WILLSUPPLY_LOANED ./illmock

## Environment varitables

### HTTP_PORT

Listen adress + port. If empty or omitted, the program will listen on any interface, port `8081`.

If the value includes a colon, it is assumed to be listening address and port, for example: `127.0.0.1:8090`.
Without colon, it translates to `:`value which will bind on any interface and port given.

### PEER_URL

The default value is `http://localhost:8081` .

### AGENCY_SCENARIO

If `AGENCY_SCENARIO` is omitted or empty, no requests will be send. If non-empty it is a comma-separated
list of scenario identifiers. Each identifier must be one of:

    WILLSUPPLY_LOANED
    WILLSUPPLY_UNFILLED
    UNFILLED
    LOANED

### AGENCY_TYPE

If omitted or empty, a value of `MOCK` is used.

### SUPPLYING_AGENCY_ID

If omitted or empty, a value of `SUP` is used.

### REQUESTING_AGENCY_ID

If omitted or empty, a value of `REQ` is used.
