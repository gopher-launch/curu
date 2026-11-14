CREATE TABLE curu.activity_recovery_drafts (
    id uuid PRIMARY KEY,
    owner_id uuid NOT NULL,
    activity_id uuid NOT NULL REFERENCES curu.activities(id) ON DELETE CASCADE,
    base_revision bigint NOT NULL CHECK (base_revision > 0),
    payload jsonb NOT NULL CHECK (
        jsonb_typeof(payload) = 'object'
        AND octet_length(payload::text) <= 1048576
    ),
    created_at timestamptz NOT NULL DEFAULT current_timestamp,
    updated_at timestamptz NOT NULL DEFAULT current_timestamp,
    UNIQUE (owner_id, activity_id)
);

CREATE INDEX activity_recovery_drafts_owner_updated_idx
    ON curu.activity_recovery_drafts (owner_id, updated_at DESC, id);
