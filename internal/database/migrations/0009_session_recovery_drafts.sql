CREATE TABLE curu.session_recovery_drafts (
    id uuid PRIMARY KEY,
    owner_id uuid NOT NULL,
    session_id uuid NOT NULL REFERENCES curu.sessions(id) ON DELETE CASCADE,
    base_revision bigint NOT NULL CHECK (base_revision > 0),
    payload jsonb NOT NULL CHECK (octet_length(payload::text) <= 1048576),
    created_at timestamptz NOT NULL DEFAULT current_timestamp,
    updated_at timestamptz NOT NULL DEFAULT current_timestamp,
    UNIQUE (owner_id, session_id)
);

CREATE INDEX session_recovery_drafts_owner_updated_idx
    ON curu.session_recovery_drafts (owner_id, updated_at DESC, id);
