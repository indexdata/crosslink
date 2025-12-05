ALTER TABLE patron_request RENAME COLUMN requester TO patron;
ALTER TABLE patron_request RENAME COLUMN borrowing_peer_id TO requester_symbol;
ALTER TABLE patron_request RENAME COLUMN lending_peer_id TO supplier_symbol;
ALTER TABLE patron_request ADD COLUMN tenant VARCHAR;
