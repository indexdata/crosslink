ALTER TABLE patron_request RENAME COLUMN requester TO patron;
ALTER TABLE patron_request ADD COLUMN tenant VARCHAR;
