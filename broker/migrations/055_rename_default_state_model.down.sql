ALTER TABLE patron_request
    ALTER COLUMN state_model SET DEFAULT 'returnables';

UPDATE patron_request
SET state_model = 'returnables'
WHERE state_model = 'default';
