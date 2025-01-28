# illmock

The illmock program is a mocking ISO18626 client / server utility. It is mostly
controlled by environment variables. For either role, a listening address
can be set with `HTTP_PORT. For example:
 
    HTTP_PORT=1.2.3.4:9001 ./illmock

or just with port to listen for on any interface with

    HTTP_PORT=9001 ./illmock

For either role, the illmock will send messages to PEER_URL (todo: must be
configurable per peer later).

if `REQUESTER_SUPPLY_IDS` is given, illmock is acting as a requester. The
value is a list of comma separated agency IDs that the requester will send
requests to (in parallel).

The illmock program is always acting as a supplier.

Example of starting illmock instances. The first is acting as a supplier. The 2nd
is acting as both, but only the requester part is used.

    HTTP_PORT=8082 PEER_URL=http://localhost:8081 ./illmock

    HTTP_PORT=8081 PEER_URL=http://localhost:8082 REQUESTER_SUPPLY_IDS=WILLSUPPLY_LOANED ./illmock




   
   
