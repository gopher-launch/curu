CREATE TABLE curu.activities (
    id uuid PRIMARY KEY,
    owner_id uuid NOT NULL,
    source_identity text,
    title text NOT NULL CHECK (length(btrim(title)) BETWEEN 1 AND 200),
    summary text NOT NULL DEFAULT '',
    content_markdown text NOT NULL DEFAULT '',
    duration_seconds integer CHECK (duration_seconds IS NULL OR duration_seconds >= 0),
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    archived_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT current_timestamp,
    updated_at timestamptz NOT NULL DEFAULT current_timestamp,
    UNIQUE (owner_id, source_identity)
);

CREATE INDEX activities_owner_current_idx
    ON curu.activities (owner_id, title, id)
    WHERE archived_at IS NULL;

CREATE TABLE curu.dataset_initializations (
    singleton_id smallint PRIMARY KEY DEFAULT 1 CHECK (singleton_id = 1),
    kind text NOT NULL CHECK (kind IN ('empty', 'seed')),
    seed_identity text,
    seed_version text,
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    initialized_at timestamptz NOT NULL DEFAULT current_timestamp,
    CHECK (
        (kind = 'empty' AND seed_identity IS NULL AND seed_version IS NULL)
        OR
        (kind = 'seed' AND seed_identity IS NOT NULL AND seed_version IS NOT NULL)
    )
);
