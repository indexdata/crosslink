CREATE TABLE batch_action_email_delivery (
    run_id VARCHAR PRIMARY KEY REFERENCES batch_action_run (id) ON DELETE CASCADE,
    selected_request_ids VARCHAR[] NOT NULL,
    queue_printed_actions BOOLEAN NOT NULL,
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    accepted_at TIMESTAMPTZ
);
