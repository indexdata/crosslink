ALTER TABLE patron_request DROP COLUMN tenant;
ALTER TABLE patron_request RENAME COLUMN requester_symbol TO borrowing_peer_id;
ALTER TABLE patron_request RENAME COLUMN supplier_symbol TO lending_peer_id;
ALTER TABLE patron_request RENAME COLUMN patron TO requester;
