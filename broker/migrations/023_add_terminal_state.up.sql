ALTER TABLE patron_request ADD COLUMN terminal_state BOOLEAN NOT NULL DEFAULT false;

CREATE INDEX idx_pr_terminal_state ON patron_request (terminal_state);