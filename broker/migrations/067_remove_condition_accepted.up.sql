-- Acceptance is recorded in the event/notification history; the next work is
-- the same as for an unconditional supply. Preserve updated_at for aging.
UPDATE patron_request
SET state = 'WILL_SUPPLY'
WHERE side = 'lending'
  AND state_model IN ('default', 'returnables')
  AND state = 'CONDITION_ACCEPTED';
