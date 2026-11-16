-- The coded value is interpreted per kind: a loan condition, cancellation
-- reason, or unfilled reason. code_description snapshots the owner's
-- Directory description when known; existing rows keep NULL.
ALTER TABLE notification
    RENAME COLUMN condition TO code;

ALTER TABLE notification
    ADD COLUMN code_description VARCHAR;
