CREATE TABLE template
(
    id           VARCHAR PRIMARY KEY,
    owner        VARCHAR   NOT NULL,
    title        VARCHAR   NOT NULL,
    purpose      VARCHAR   NOT NULL,
    subject      TEXT,
    body         TEXT      NOT NULL,
    content_type VARCHAR   NOT NULL,
    labels       TEXT[]    NOT NULL DEFAULT '{}',
    audience     VARCHAR,
    created_at   TIMESTAMP NOT NULL DEFAULT now(),
    updated_at   TIMESTAMP
);

CREATE INDEX idx_template_owner_created_at ON template (owner, created_at);
CREATE INDEX idx_template_owner_purpose_created_at ON template (owner, purpose, created_at);
CREATE INDEX idx_template_labels_gin ON template USING GIN (labels);