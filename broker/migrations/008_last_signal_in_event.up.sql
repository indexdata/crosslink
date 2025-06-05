ALTER TABLE event ADD COLUMN last_signal VARCHAR DEFAULT '';

UPDATE event SET last_signal = '';

ALTER TABLE event ALTER COLUMN last_signal SET NOT NULL;
