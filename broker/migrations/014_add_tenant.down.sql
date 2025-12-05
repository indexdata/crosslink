ALTER TABLE patron_request DROP COLUMN tenant;
ALTER TABLE patron_request RENAME COLUMN patron TO requester;