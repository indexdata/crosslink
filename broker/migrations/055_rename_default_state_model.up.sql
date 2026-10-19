ALTER TABLE patron_request
    ALTER COLUMN state_model SET DEFAULT 'default';

UPDATE patron_request
SET state_model = 'default'
WHERE state_model = 'returnables';
